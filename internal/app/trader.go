package app

import (
	"context"
	"fmt"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/jayce/btc-trader/internal/config"
	"github.com/jayce/btc-trader/internal/eventbus"
	"github.com/jayce/btc-trader/internal/exchange"
	"github.com/jayce/btc-trader/internal/exchange/binance"
	"github.com/jayce/btc-trader/internal/market"
	"github.com/jayce/btc-trader/internal/notify"
	"github.com/jayce/btc-trader/internal/order"
	"github.com/jayce/btc-trader/internal/position"
	"github.com/jayce/btc-trader/internal/risk"
	"github.com/jayce/btc-trader/internal/storage"
	"github.com/jayce/btc-trader/internal/storage/timescale"
	"github.com/jayce/btc-trader/internal/strategy"
	"github.com/jayce/btc-trader/internal/strategy/trend"
	"github.com/jayce/btc-trader/internal/web"
	"github.com/jayce/btc-trader/internal/web/handler"
	"github.com/jayce/btc-trader/internal/web/ws"
	"go.uber.org/zap"
)

// Trader is the main application orchestrator for live/paper trading.
type Trader struct {
	cfg      *config.Config
	logger   *zap.Logger
	bus      *eventbus.Bus
	exchange exchange.Exchange
	store    *timescale.Store
	position *position.Manager
	risk     *risk.Manager
	order    *order.Manager
	strat    strategy.Strategy
	indComp  *market.IndicatorComputer
}

// NewTrader creates and initializes all components.
func NewTrader(ctx context.Context, cfg *config.Config, logger *zap.Logger) (*Trader, error) {
	// Event bus
	bus := eventbus.New(logger)

	// Exchange client
	ex := binance.NewClient(
		cfg.Exchange.APIKey,
		cfg.Exchange.SecretKey,
		cfg.App.Testnet,
		logger.Named("binance"),
	)

	// Database
	store, err := timescale.New(ctx, cfg.Database, logger.Named("db"))
	if err != nil {
		return nil, fmt.Errorf("init db: %w", err)
	}

	// Run migrations
	if err := store.Migrate(ctx); err != nil {
		store.Close()
		return nil, fmt.Errorf("migrate db: %w", err)
	}

	// Position manager
	posMgr := position.NewManager(logger.Named("position"))

	// Risk manager
	riskMgr := risk.NewManager(cfg.Risk, bus, logger.Named("risk"))

	// Order manager
	orderMgr := order.NewManager(ex, riskMgr, posMgr, store, bus, logger.Named("order"))
	orderMgr.SetRiskConfig(cfg.Risk)
	riskMgr.SetOrderCanceler(orderMgr, cfg.Exchange.Symbols)

	// Strategy
	strat, err := createStrategy(cfg.Strategy)
	if err != nil {
		store.Close()
		return nil, fmt.Errorf("create strategy: %w", err)
	}

	// Indicator computer
	indComp := market.NewIndicatorComputer()

	return &Trader{
		cfg:      cfg,
		logger:   logger,
		bus:      bus,
		exchange: ex,
		store:    store,
		position: posMgr,
		risk:     riskMgr,
		order:    orderMgr,
		strat:    strat,
		indComp:  indComp,
	}, nil
}

// Run starts all components and blocks until context is canceled.
func (t *Trader) Run(ctx context.Context) error {
	t.logger.Info("starting trader",
		zap.String("mode", t.cfg.App.Mode),
		zap.Strings("symbols", t.cfg.Exchange.Symbols),
		zap.String("strategy", t.strat.Name()),
	)

	// Initialize equity and sync positions from account
	if t.cfg.Exchange.APIKey != "" {
		acc, err := t.exchange.GetAccount(ctx)
		if err != nil {
			t.logger.Warn("failed to get initial account, continuing", zap.Error(err))
		} else {
			var usdtEquity float64
			for _, b := range acc.Balances {
				if b.Asset == "USDT" {
					usdtEquity = b.Free + b.Locked
					break
				}
			}
			// Paper 模式下账户余额可能很小，用 10000 作为最低初始权益
			if t.cfg.App.Mode == "paper" && usdtEquity < 100 {
				t.logger.Info("paper mode: using simulated initial equity", zap.Float64("actual_usdt", usdtEquity))
				usdtEquity = 10000
			}
			t.risk.SetEquity(usdtEquity)
			// Sync existing positions from Binance account
			t.position.SyncFromAccount(acc.Balances, t.cfg.Exchange.Symbols, func(symbol string) float64 {
				ticker, err := t.exchange.GetTicker(ctx, symbol)
				if err != nil {
					t.logger.Warn("get ticker for position sync", zap.String("symbol", symbol), zap.Error(err))
					return 0
				}
				return ticker.LastPrice
			})
		}
	} else {
		t.logger.Warn("no API key configured, skipping account initialization")
		t.risk.SetEquity(10000) // 模拟初始资金
	}

	// Load exchange symbol info (stepSize, minQty, etc.) for order precision
	if err := t.order.LoadSymbolInfo(ctx); err != nil {
		t.logger.Warn("failed to load symbol info, orders may fail precision checks", zap.Error(err))
	}

	// Backfill historical klines from REST API before starting WS streams.
	// This fills any gaps caused by previous restarts and ensures charts have
	// enough data to render immediately.
	t.backfillKlines(ctx)

	g, gCtx := errgroup.WithContext(ctx)

	// Start WebSocket streams for each symbol
	for _, symbol := range t.cfg.Exchange.Symbols {
		sym := symbol
		for _, interval := range t.cfg.Exchange.KlineIntervals {
			intv := interval
			g.Go(func() error {
				return t.runKlineIngestion(gCtx, sym, intv)
			})
		}
	}

	// Strategy evaluation loop
	g.Go(func() error {
		return t.runStrategyLoop(gCtx)
	})

	// Risk continuous monitor
	g.Go(func() error {
		return t.risk.ContinuousMonitor(gCtx)
	})

	// Order manager: tracks SL/TP fills, trailing stops, polls order status
	if t.cfg.App.Mode == "live" {
		g.Go(func() error {
			return t.order.Run(gCtx)
		})
	}

	// User data stream for order tracking (requires API key)
	if t.cfg.Exchange.APIKey != "" {
		g.Go(func() error {
			return t.runUserDataStream(gCtx)
		})
	} else {
		t.logger.Warn("no API key, skipping user data stream")
	}

	// Account snapshot loop
	g.Go(func() error {
		return t.runSnapshotLoop(gCtx)
	})

	// Dashboard server
	if t.cfg.Dashboard.Enabled {
		deps := &handler.Deps{
			Config:   t.cfg,
			Bus:      t.bus,
			Store:    t.store,
			Exchange: t.exchange,
			Position: t.position,
			Risk:     t.risk,
			Order:    t.order,
			Strategy: t.strat,
		}
		dashServer := web.NewServer(deps, t.logger.Named("dashboard"))

		bridge := ws.NewBridge(t.bus, dashServer.Hub(), t.logger.Named("ws-bridge"))
		g.Go(func() error {
			bridge.Run(gCtx)
			return nil
		})
		g.Go(func() error {
			return dashServer.Run(gCtx)
		})
	}

	// Telegram notifications
	t.logger.Info("telegram config", zap.Bool("enabled", t.cfg.Telegram.Enabled), zap.String("chat_id", t.cfg.Telegram.ChatID))
	if t.cfg.Telegram.Enabled {
		tgCfg := notify.TelegramConfig{
			Enabled: t.cfg.Telegram.Enabled,
			Token:   t.cfg.Telegram.Token,
			ChatID:  t.cfg.Telegram.ChatID,
		}
		tgNotifier := notify.NewTelegramNotifier(tgCfg, t.bus, t.logger.Named("telegram"))
		g.Go(func() error {
			return tgNotifier.Run(gCtx)
		})
	}

	return g.Wait()
}

// Shutdown gracefully shuts down all components.
func (t *Trader) Shutdown() {
	t.logger.Info("shutting down trader...")

	// Cancel all open orders
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, sym := range t.cfg.Exchange.Symbols {
		if err := t.order.CancelAllOrders(ctx, sym); err != nil {
			t.logger.Error("cancel orders on shutdown", zap.String("symbol", sym), zap.Error(err))
		}
	}

	// Close event bus
	t.bus.Close()

	// Close database
	t.store.Close()

	t.logger.Info("trader shutdown complete")
}

// runKlineIngestion subscribes to kline WS and persists + publishes data.
func (t *Trader) runKlineIngestion(ctx context.Context, symbol, interval string) error {
	ch, err := t.exchange.SubscribeKlines(ctx, symbol, interval)
	if err != nil {
		return fmt.Errorf("subscribe klines %s %s: %w", symbol, interval, err)
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case kline, ok := <-ch:
			if !ok {
				return nil
			}

			// Persist final klines
			if kline.IsFinal {
				if err := t.store.SaveKlines(ctx, []exchange.Kline{kline}); err != nil {
					t.logger.Error("save kline", zap.Error(err))
				}
			}

			// Publish to event bus
			t.bus.Publish(eventbus.Event{
				Type:      eventbus.EventKlineUpdate,
				Timestamp: time.Now(),
				Payload: eventbus.KlineEvent{
					Symbol:   symbol,
					Interval: interval,
					Kline:    kline,
				},
			})

			// Update position price
			t.position.UpdatePrice(symbol, kline.Close)
		}
	}
}

// runStrategyLoop listens for kline events and evaluates the strategy.
func (t *Trader) runStrategyLoop(ctx context.Context) error {
	klineCh := t.bus.Subscribe(eventbus.EventKlineUpdate, 1000)

	// Maintain kline windows per symbol+interval
	windows := make(map[string][]exchange.Kline)
	historySize := t.strat.RequiredHistory()

	// Determine the strategy's target interval from config
	targetInterval := "1h"
	if v, ok := t.cfg.Strategy.Config["interval"]; ok {
		if s, ok := v.(string); ok {
			targetInterval = s
		}
	}

	// Multi-timeframe: detect if strategy supports HTF
	var htfInterval string
	var htfHistSize int
	var htfIndReqs []strategy.IndicatorRequirement
	if cw, ok := t.strat.(*trend.CustomWeightedStrategy); ok {
		htfInterval = cw.HTFInterval()
		htfHistSize = cw.HTFHistoryRequired()
		htfIndReqs = cw.HTFIndicatorRequirements()
		if htfInterval != "" {
			t.logger.Info("multi-timeframe enabled",
				zap.String("htf_interval", htfInterval),
				zap.Int("htf_history", htfHistSize),
			)
		}
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case evt, ok := <-klineCh:
			if !ok {
				return nil
			}

			ke, ok := evt.Payload.(eventbus.KlineEvent)
			if !ok {
				continue
			}

			// Accumulate HTF klines (e.g., 4h) into their own window
			if htfInterval != "" && ke.Interval == htfInterval && ke.Kline.IsFinal {
				htfKey := ke.Symbol + ":" + ke.Interval
				w := windows[htfKey]
				w = append(w, ke.Kline)
				if len(w) > htfHistSize*2 {
					w = w[len(w)-htfHistSize*2:]
				}
				windows[htfKey] = w
			}

			// Only evaluate on the target interval and final klines
			if ke.Interval != targetInterval || !ke.Kline.IsFinal {
				continue
			}

			// Update primary window
			key := ke.Symbol + ":" + ke.Interval
			window := windows[key]
			window = append(window, ke.Kline)
			if len(window) > historySize*2 {
				window = window[len(window)-historySize*2:]
			}
			windows[key] = window

			if len(window) < historySize {
				continue
			}

			// Compute indicators on primary timeframe
			indicators := t.indComp.ComputeAll(window, t.strat.RequiredIndicators())

			// Build snapshot
			pos := t.position.GetPosition(ke.Symbol)
			snapshot := &strategy.MarketSnapshot{
				Symbol:     ke.Symbol,
				Klines:     window,
				Indicators: indicators,
				Position: &strategy.PositionInfo{
					Quantity:      pos.Quantity,
					AvgEntryPrice: pos.AvgEntryPrice,
					UnrealizedPnL: pos.UnrealizedPnL,
					Side:          pos.Side,
				},
				Timestamp: ke.Kline.CloseTime,
			}

			// Attach HTF data if available
			if htfInterval != "" {
				htfKey := ke.Symbol + ":" + htfInterval
				htfWindow := windows[htfKey]
				if len(htfWindow) >= htfHistSize {
					snapshot.HTFKlines = htfWindow
					snapshot.HTFInterval = htfInterval
					snapshot.HTFIndicators = t.indComp.ComputeAll(htfWindow, htfIndReqs)
				}
			}

			// Evaluate strategy
			sig, err := t.strat.Evaluate(ctx, snapshot)
			if err != nil {
				t.logger.Error("strategy evaluate", zap.Error(err))
				continue
			}

			if sig.Action != strategy.Hold {
				t.logger.Info("strategy signal",
					zap.String("symbol", sig.Symbol),
					zap.String("action", sig.Action.String()),
					zap.Float64("strength", sig.Strength),
					zap.String("reason", sig.Reason),
				)

				// Publish signal event
				t.bus.Publish(eventbus.Event{
					Type:      eventbus.EventSignal,
					Timestamp: time.Now(),
					Payload: eventbus.SignalEvent{
						Symbol:   sig.Symbol,
						Action:   sig.Action.String(),
						Strength: sig.Strength,
						Strategy: sig.Strategy,
						Reason:   sig.Reason,
					},
				})

				// Execute order or alert
				if sig.Action.IsShort() {
					// Short/Cover: alert only — publish event (→ Telegram) but do NOT place orders
					// Update strategy's virtual short state
					if shortHandler, ok := t.strat.(interface {
						OnShortSignalProcessed(strategy.Action, float64)
					}); ok {
						price := float64(0)
						if len(snapshot.Klines) > 0 {
							price = snapshot.Klines[len(snapshot.Klines)-1].Close
						}
						shortHandler.OnShortSignalProcessed(sig.Action, price)
					}
					t.store.SaveSignal(ctx, sig, false)
				} else if t.cfg.App.Mode == "live" {
					if err := t.order.ProcessSignal(ctx, sig); err != nil {
						t.logger.Error("process signal", zap.Error(err))
					}
				} else {
					// Paper mode: just log and persist signal
					t.store.SaveSignal(ctx, sig, false)
				}
			}
		}
	}
}

// runUserDataStream subscribes to user data for order/balance updates.
func (t *Trader) runUserDataStream(ctx context.Context) error {
	ch, err := t.exchange.SubscribeUserData(ctx)
	if err != nil {
		t.logger.Warn("user data stream unavailable (testnet may not support)", zap.Error(err))
		// Don't fail the whole app
		<-ctx.Done()
		return ctx.Err()
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case evt, ok := <-ch:
			if !ok {
				return nil
			}

			if evt.OrderUpdate != nil {
				t.bus.Publish(eventbus.Event{
					Type:      eventbus.EventAccountUpdate,
					Timestamp: time.Now(),
					Payload:   eventbus.OrderUpdateEvent{Order: *evt.OrderUpdate},
				})
			}
		}
	}
}

// runSnapshotLoop periodically saves account snapshots.
func (t *Trader) runSnapshotLoop(ctx context.Context) error {
	interval := t.cfg.Snapshot.Interval
	if interval == 0 {
		interval = 5 * time.Minute
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			t.saveSnapshot(ctx)
		}
	}
}

func (t *Trader) saveSnapshot(ctx context.Context) {
	acc, err := t.exchange.GetAccount(ctx)
	if err != nil {
		t.logger.Error("snapshot: get account", zap.Error(err))
		return
	}

	var freeCash float64
	for _, b := range acc.Balances {
		if b.Asset == "USDT" {
			freeCash = b.Free
			break
		}
	}

	// Update position prices before calculating equity
	for _, sym := range t.cfg.Exchange.Symbols {
		ticker, err := t.exchange.GetTicker(ctx, sym)
		if err == nil && ticker.LastPrice > 0 {
			t.position.UpdatePrice(sym, ticker.LastPrice)
		}
	}

	positions := t.position.GetAllPositions()
	posMap := make(map[string]float64)
	posValue := 0.0
	for sym, pos := range positions {
		posMap[sym] = pos.Quantity
		posValue += pos.Quantity * pos.CurrentPrice
	}

	totalEquity := freeCash + posValue
	// Paper 模式下若无持仓且余额极小，跳过风控更新防误触
	if totalEquity < 1 {
		t.logger.Debug("snapshot: equity near zero, skipping risk update",
			zap.Float64("freeCash", freeCash),
			zap.Float64("posValue", posValue),
		)
		return
	}
	unrealizedPnL := t.position.TotalUnrealizedPnL()
	realizedPnL := t.position.TotalRealizedPnL()

	riskStatus := t.risk.GetStatus()

	snap := &storage.AccountSnapshot{
		Timestamp:     time.Now(),
		TotalEquity:   totalEquity,
		FreeCash:      freeCash,
		PositionValue: posValue,
		UnrealizedPnL: unrealizedPnL,
		RealizedPnL:   realizedPnL,
		DailyPnL:      riskStatus.DailyPnL,
		DrawdownPct:   riskStatus.CurrentDrawdown,
		Positions:     posMap,
	}

	// Update risk manager with live equity
	t.risk.UpdateEquity(totalEquity)

	if err := t.store.SaveSnapshot(ctx, snap); err != nil {
		t.logger.Error("save snapshot", zap.Error(err))
	}
}

// backfillKlines fetches recent historical klines from Binance REST API
// and upserts them into the database. This fills gaps from previous restarts
// and ensures charts always have enough data to display.
func (t *Trader) backfillKlines(ctx context.Context) {
	const backfillLimit = 500 // enough for the chart's default view

	for _, symbol := range t.cfg.Exchange.Symbols {
		for _, interval := range t.cfg.Exchange.KlineIntervals {
			klines, err := t.exchange.GetKlines(ctx, exchange.KlineRequest{
				Symbol:   symbol,
				Interval: interval,
				Limit:    backfillLimit,
			})
			if err != nil {
				t.logger.Warn("backfill klines failed",
					zap.String("symbol", symbol),
					zap.String("interval", interval),
					zap.Error(err),
				)
				continue
			}

			if len(klines) == 0 {
				continue
			}

			if err := t.store.SaveKlines(ctx, klines); err != nil {
				t.logger.Warn("backfill save failed",
					zap.String("symbol", symbol),
					zap.String("interval", interval),
					zap.Error(err),
				)
				continue
			}

			t.logger.Info("backfill klines complete",
				zap.String("symbol", symbol),
				zap.String("interval", interval),
				zap.Int("count", len(klines)),
			)
		}
	}
}

func createStrategy(cfg config.StrategyConfig) (strategy.Strategy, error) {
	reg := strategy.NewRegistry()
	reg.Register("custom_weighted", func() strategy.Strategy { return trend.NewCustomWeightedStrategy() })
	return reg.Create(cfg.Name, cfg.Config)
}
