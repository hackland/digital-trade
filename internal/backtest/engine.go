package backtest

import (
	"context"
	"fmt"
	"time"

	"github.com/jayce/btc-trader/internal/exchange"
	"github.com/jayce/btc-trader/internal/exchange/simulated"
	"github.com/jayce/btc-trader/internal/market"
	"github.com/jayce/btc-trader/internal/strategy"
	"go.uber.org/zap"
)

// EngineConfig configures the backtest engine.
type EngineConfig struct {
	Symbol      string
	Interval    string
	InitialCash float64
	FeeRate     float64 // e.g., 0.001 = 0.1%
	AllocPct    float64 // fraction of equity to allocate per trade (e.g., 0.1 = 10%)
	DynamicSize bool    // if true, scale position size by signal.Strength (AllocPct is max)

	// Multi-timeframe support
	HTFKlines   []exchange.Kline              // higher-timeframe klines (e.g., 4h)
	HTFInterval string                        // e.g., "4h"
	HTFIndReqs  []strategy.IndicatorRequirement // indicator requirements for HTF
	HTFHistSize int                           // minimum HTF klines needed
}

// Engine drives the backtest simulation.
type Engine struct {
	cfg      EngineConfig
	strat    strategy.Strategy
	exchange *simulated.Exchange
	indComp  *market.IndicatorComputer
	logger   *zap.Logger
}

// NewEngine creates a backtest engine.
func NewEngine(cfg EngineConfig, strat strategy.Strategy, logger *zap.Logger) *Engine {
	return &Engine{
		cfg:      cfg,
		strat:    strat,
		exchange: simulated.NewExchange(cfg.InitialCash, cfg.FeeRate),
		indComp:  market.NewIndicatorComputer(),
		logger:   logger,
	}
}

// Run executes the backtest on the given kline data and returns results.
func (e *Engine) Run(ctx context.Context, klines []exchange.Kline) (*Result, error) {
	if len(klines) == 0 {
		return nil, fmt.Errorf("no kline data to backtest")
	}

	histSize := e.strat.RequiredHistory()
	if len(klines) < histSize {
		return nil, fmt.Errorf("not enough kline data: have %d, need %d", len(klines), histSize)
	}

	// Track state
	var equityCurve []EquityPoint
	var tradeRecords []TradeRecord
	var positionQty float64
	var avgEntryPrice float64

	symbol := e.cfg.Symbol
	allocPct := e.cfg.AllocPct
	if allocPct <= 0 {
		allocPct = 0.9 // default 90% for single-asset backtesting
	}

	e.logger.Info("backtest starting",
		zap.String("symbol", symbol),
		zap.String("strategy", e.strat.Name()),
		zap.Int("total_bars", len(klines)),
		zap.Int("history_required", histSize),
	)

	// Main bar loop
	for i := histSize; i < len(klines); i++ {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		currentBar := klines[i]
		window := klines[i-histSize : i+1]

		// Set market price in simulated exchange
		e.exchange.SetPrice(symbol, currentBar.Close)

		// Compute indicators
		indicators := e.indComp.ComputeAll(window, e.strat.RequiredIndicators())

		// Build market snapshot
		posInfo := &strategy.PositionInfo{
			Quantity:      positionQty,
			AvgEntryPrice: avgEntryPrice,
			Side:          "FLAT",
		}
		if positionQty > 0 {
			posInfo.Side = "LONG"
			posInfo.UnrealizedPnL = (currentBar.Close - avgEntryPrice) * positionQty
		}

		snapshot := &strategy.MarketSnapshot{
			Symbol:     symbol,
			Klines:     window,
			Indicators: indicators,
			Position:   posInfo,
			Timestamp:  currentBar.OpenTime,
		}

		// Attach HTF data if configured
		if len(e.cfg.HTFKlines) > 0 && e.cfg.HTFHistSize > 0 {
			htfWindow := e.findHTFWindow(currentBar.OpenTime)
			if len(htfWindow) >= e.cfg.HTFHistSize {
				snapshot.HTFKlines = htfWindow
				snapshot.HTFInterval = e.cfg.HTFInterval
				if len(e.cfg.HTFIndReqs) > 0 {
					snapshot.HTFIndicators = e.indComp.ComputeAll(htfWindow, e.cfg.HTFIndReqs)
				}
			}
		}

		// Evaluate strategy
		sig, err := e.strat.Evaluate(ctx, snapshot)
		if err != nil {
			e.logger.Warn("strategy error", zap.Error(err))
			continue
		}

		// Execute signals
		if sig.Action == strategy.Buy && positionQty == 0 {
			// Get current balance
			bal, _ := e.exchange.GetBalance(ctx, "USDT")
			effectiveAlloc := allocPct
			e.logger.Debug("buy signal",
				zap.Float64("balance", bal.Free),
				zap.Float64("alloc_pct", effectiveAlloc),
				zap.Float64("alloc_usdt", bal.Free*effectiveAlloc),
				zap.Float64("price", currentBar.Close),
			)
			if e.cfg.DynamicSize && sig.Strength > 0 {
				// Scale: 50% strength → 50% of allocPct, 100% → full allocPct
				// Minimum 30% of allocPct to avoid dust orders
				effectiveAlloc = allocPct * clampFloat(sig.Strength, 0.3, 1.0)
			}
			allocUSDT := bal.Free * effectiveAlloc
			// Reserve fee in sizing so alloc=1 with fee>0 can still be filled.
			denom := currentBar.Close * (1 + e.cfg.FeeRate)
			if denom <= 0 {
				continue
			}
			qty := allocUSDT / denom

			if qty > 0 {
				order, err := e.exchange.PlaceOrder(ctx, exchange.OrderRequest{
					Symbol:   symbol,
					Side:     exchange.OrderSideBuy,
					Type:     exchange.OrderTypeMarket,
					Quantity: qty,
				})
				if err != nil {
					e.logger.Warn("buy failed", zap.Error(err))
				} else if order.Status == exchange.OrderStatusFilled {
					positionQty = order.FilledQty
					avgEntryPrice = order.AvgPrice

					// Notify strategy: sets entryPrice, highWaterMark, resets counters
					e.strat.OnTradeExecuted(&exchange.Trade{
						Symbol:   symbol,
						Side:     exchange.OrderSideBuy,
						Price:    order.AvgPrice,
						Quantity: order.FilledQty,
					})

					tradeRecords = append(tradeRecords, TradeRecord{
						Timestamp: currentBar.OpenTime,
						Side:      "BUY",
						Price:     order.AvgPrice,
						Quantity:  order.FilledQty,
						Fee:       order.AvgPrice * order.FilledQty * e.cfg.FeeRate,
						Reason:    sig.Reason,
					})
				}
			}
		} else if sig.Action == strategy.Sell && positionQty > 0 {
			order, err := e.exchange.PlaceOrder(ctx, exchange.OrderRequest{
				Symbol:   symbol,
				Side:     exchange.OrderSideSell,
				Type:     exchange.OrderTypeMarket,
				Quantity: positionQty,
			})
			if err != nil {
				e.logger.Warn("sell failed", zap.Error(err))
			} else if order.Status == exchange.OrderStatusFilled {
				pnl := (order.AvgPrice - avgEntryPrice) * positionQty

				// Notify strategy: starts cooldown, resets tracking
				e.strat.OnTradeExecuted(&exchange.Trade{
					Symbol:   symbol,
					Side:     exchange.OrderSideSell,
					Price:    order.AvgPrice,
					Quantity: order.FilledQty,
				})

				tradeRecords = append(tradeRecords, TradeRecord{
					Timestamp: currentBar.OpenTime,
					Side:      "SELL",
					Price:     order.AvgPrice,
					Quantity:  order.FilledQty,
					Fee:       order.AvgPrice * order.FilledQty * e.cfg.FeeRate,
					PnL:       pnl,
					Reason:    sig.Reason,
				})

				positionQty = 0
				avgEntryPrice = 0
			}
		}

		// Record equity point
		equity := e.computeEquity(ctx, symbol, currentBar.Close, positionQty)
		equityCurve = append(equityCurve, EquityPoint{
			Time:   currentBar.OpenTime,
			Equity: equity,
		})
	}

	// Close any remaining position at the last price
	if positionQty > 0 {
		lastPrice := klines[len(klines)-1].Close
		order, err := e.exchange.PlaceOrder(ctx, exchange.OrderRequest{
			Symbol:   symbol,
			Side:     exchange.OrderSideSell,
			Type:     exchange.OrderTypeMarket,
			Quantity: positionQty,
		})
		if err == nil && order.Status == exchange.OrderStatusFilled {
			pnl := (lastPrice - avgEntryPrice) * positionQty

			e.strat.OnTradeExecuted(&exchange.Trade{
				Symbol:   symbol,
				Side:     exchange.OrderSideSell,
				Price:    order.AvgPrice,
				Quantity: order.FilledQty,
			})

			tradeRecords = append(tradeRecords, TradeRecord{
				Timestamp: klines[len(klines)-1].OpenTime,
				Side:      "SELL",
				Price:     order.AvgPrice,
				Quantity:  order.FilledQty,
				Fee:       order.AvgPrice * order.FilledQty * e.cfg.FeeRate,
				PnL:       pnl,
				Reason:    "backtest end: close position",
			})
			positionQty = 0
		}
	}

	// Compute final equity
	finalEquity := e.computeEquity(ctx, symbol, klines[len(klines)-1].Close, 0)
	if len(equityCurve) > 0 {
		equityCurve[len(equityCurve)-1].Equity = finalEquity
	}

	// Build result
	startTime := klines[histSize].OpenTime
	endTime := klines[len(klines)-1].OpenTime
	duration := endTime.Sub(startTime)

	// Get all simulated trades for metrics
	simTrades := e.exchange.GetTrades()

	metrics := ComputeMetrics(simTrades, equityCurve, e.cfg.InitialCash, duration)

	result := &Result{
		Symbol:      symbol,
		Strategy:    e.strat.Name(),
		Interval:    e.cfg.Interval,
		StartTime:   startTime,
		EndTime:     endTime,
		Duration:    duration,
		InitialCash: e.cfg.InitialCash,
		FeeRate:     e.cfg.FeeRate,
		AllocPct:    allocPct,
		Trades:      tradeRecords,
		Metrics:     metrics,
		EquityCurve: equityCurve,
	}

	e.logger.Info("backtest complete",
		zap.Int("total_trades", metrics.TotalTrades),
		zap.Float64("return_pct", metrics.TotalReturnPct),
		zap.Float64("max_drawdown_pct", metrics.MaxDrawdownPct),
		zap.Float64("sharpe", metrics.SharpeRatio),
	)

	return result, nil
}

// computeEquity calculates total equity = cash + position value.
func (e *Engine) computeEquity(ctx context.Context, symbol string, price float64, qty float64) float64 {
	bal, _ := e.exchange.GetBalance(ctx, "USDT")
	return bal.Free + qty*price
}

// LoadKlinesFromStore loads historical klines from the database.
// This is a convenience method for the CLI.
func LoadKlinesFromStore(
	ctx context.Context,
	store KlineLoader,
	symbol, interval string,
	start, end time.Time,
) ([]exchange.Kline, error) {
	klines, err := store.GetKlines(ctx, symbol, interval, start, end, 0)
	if err != nil {
		return nil, fmt.Errorf("load klines: %w", err)
	}
	return klines, nil
}

// KlineLoader is the minimal interface for loading klines.
type KlineLoader interface {
	GetKlines(ctx context.Context, symbol, interval string, start, end time.Time, limit int) ([]exchange.Kline, error)
}

// findHTFWindow returns the slice of HTF klines up to (and including) the bar at or before ts.
func (e *Engine) findHTFWindow(ts time.Time) []exchange.Kline {
	idx := -1
	for i := len(e.cfg.HTFKlines) - 1; i >= 0; i-- {
		if !e.cfg.HTFKlines[i].OpenTime.After(ts) {
			idx = i
			break
		}
	}
	if idx < 0 {
		return nil
	}
	start := 0
	if idx-e.cfg.HTFHistSize*2 > 0 {
		start = idx - e.cfg.HTFHistSize*2
	}
	return e.cfg.HTFKlines[start : idx+1]
}

func clampFloat(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
