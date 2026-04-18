package order

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/jayce/btc-trader/internal/config"
	"github.com/jayce/btc-trader/internal/eventbus"
	"github.com/jayce/btc-trader/internal/exchange"
	"github.com/jayce/btc-trader/internal/position"
	"github.com/jayce/btc-trader/internal/risk"
	"github.com/jayce/btc-trader/internal/storage"
	"github.com/jayce/btc-trader/internal/strategy"
	"go.uber.org/zap"
)

// Manager handles order lifecycle: signal → risk check → place → track → update position.
// It also manages stop-loss, take-profit, and trailing-stop orders automatically.
type Manager struct {
	exchange exchange.Exchange
	risk     *risk.Manager
	riskCfg  config.RiskConfig
	position *position.Manager
	store    storage.Store
	bus      *eventbus.Bus
	logger   *zap.Logger
	strategy strategy.Strategy // optional: notified on trade execution to sync state

	mu           sync.RWMutex
	activeOrders map[int64]*exchange.Order

	// filledOrderIDs tracks orders we've already processed in onOrderFilled,
	// to prevent double-processing when both ProcessSignal's immediate FILLED
	// detection AND the subsequent pollActiveOrders/userDataStream callback
	// fire for the same order. Without dedup, position is updated twice and
	// drifts wildly from reality (observed: 1 BTC → -1 BTC → +1.28 BTC ...).
	filledOrderIDs map[int64]time.Time

	// pendingStrategy maps symbol → strategy name, populated by ProcessSignal
	// BEFORE calling PlaceOrder. onOrderFilled looks up this map when the
	// WS/poll path wins the dedup race (WS executionReport often arrives before
	// REST PlaceOrder returns!) and the `strategyName` parameter is empty.
	// Keyed by symbol (not order ID) because the order ID isn't known until
	// REST returns, but the WS fires first.
	pendingStrategy map[string]string

	// SL/TP tracking per symbol
	slOrders      map[string]int64   // symbol → SL order ID
	tpOrders      map[string]int64   // symbol → TP order ID
	trailingHighs map[string]float64 // symbol → highest price since entry (for trailing stop)
	lastATR       map[string]float64 // symbol → ATR at signal time (for dynamic SL/TP)

	// Alert dedup: only alert once per breach until price recovers
	emergencyAlerted map[string]bool // entry-based loss alert
	drawdownAlerted  map[string]bool // peak-based drawdown alert

	// Exchange symbol info cache (for quantity/price precision)
	symbolInfo map[string]exchange.SymbolInfo
}

// NewManager creates a new order manager.
func NewManager(
	ex exchange.Exchange,
	riskMgr *risk.Manager,
	pos *position.Manager,
	store storage.Store,
	bus *eventbus.Bus,
	logger *zap.Logger,
) *Manager {
	return &Manager{
		exchange:         ex,
		risk:             riskMgr,
		position:         pos,
		store:            store,
		bus:              bus,
		logger:           logger,
		activeOrders:     make(map[int64]*exchange.Order),
		filledOrderIDs:   make(map[int64]time.Time),
		pendingStrategy:  make(map[string]string),
		slOrders:         make(map[string]int64),
		tpOrders:         make(map[string]int64),
		trailingHighs:    make(map[string]float64),
		lastATR:          make(map[string]float64),
		emergencyAlerted: make(map[string]bool),
		drawdownAlerted:  make(map[string]bool),
		symbolInfo:       make(map[string]exchange.SymbolInfo),
	}
}

// LoadSymbolInfo fetches exchange info and caches symbol rules (stepSize, minQty, etc.).
// Must be called before placing any orders.
func (m *Manager) LoadSymbolInfo(ctx context.Context) error {
	info, err := m.exchange.GetExchangeInfo(ctx)
	if err != nil {
		return fmt.Errorf("get exchange info: %w", err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, si := range info.Symbols {
		m.symbolInfo[si.Symbol] = si
		m.logger.Debug("symbol info loaded",
			zap.String("symbol", si.Symbol),
			zap.Float64("stepSize", si.StepSize),
			zap.Float64("minQty", si.MinQty),
			zap.Float64("minNotional", si.MinNotional),
		)
	}
	return nil
}

// adjustQuantity truncates quantity to stepSize precision and validates min constraints.
func (m *Manager) adjustQuantity(symbol string, qty, price float64) (float64, error) {
	m.mu.RLock()
	si, ok := m.symbolInfo[symbol]
	m.mu.RUnlock()

	if !ok || si.StepSize <= 0 {
		// Fallback: round to 5 decimals (BTCUSDT default)
		qty = math.Floor(qty*100000) / 100000
		if qty <= 0 {
			return 0, fmt.Errorf("quantity too small after rounding")
		}
		return qty, nil
	}

	// Truncate to stepSize (floor, not round — never overshoot balance)
	steps := math.Floor(qty / si.StepSize)
	qty = steps * si.StepSize

	// Check minQty
	if qty < si.MinQty {
		return 0, fmt.Errorf("quantity %.8f below minQty %.8f for %s", qty, si.MinQty, symbol)
	}

	// Check minNotional (order value must exceed minimum, e.g., $5)
	if si.MinNotional > 0 && qty*price < si.MinNotional {
		return 0, fmt.Errorf("notional %.2f below minimum %.2f for %s", qty*price, si.MinNotional, symbol)
	}

	return qty, nil
}

// adjustPrice truncates price to tick precision.
func (m *Manager) adjustPrice(symbol string, price float64) float64 {
	m.mu.RLock()
	si, ok := m.symbolInfo[symbol]
	m.mu.RUnlock()

	if !ok || si.PricePrecision <= 0 {
		return roundPrice(price) // fallback to 2 decimals
	}
	factor := math.Pow(10, float64(si.PricePrecision))
	return math.Floor(price*factor) / factor
}

// SetRiskConfig allows injecting the risk config for SL/TP settings.
func (m *Manager) SetRiskConfig(cfg config.RiskConfig) {
	m.riskCfg = cfg
}

// SetStrategy injects the strategy reference so that order fills can sync
// strategy internal state (entryPrice, highWaterMark, cooldown, etc.).
// Without this, the strategy's OnTradeExecuted is never called and state
// drifts from reality (e.g. cooldown never starts after a sell fill).
func (m *Manager) SetStrategy(s strategy.Strategy) {
	m.strategy = s
}

// errAlreadyInPosition is returned when a BUY signal arrives while we
// already hold the asset (e.g. trader restarted with an open position).
// Treated as a silent no-op so the strategy stops looping without sending
// scary "quantity below minQty" Telegram alerts caused by ~$0 USDT free.
var errAlreadyInPosition = fmt.Errorf("already in position, skipping buy")

// errDustPosition is returned when the position is below the exchange's
// min step (1e-5 BTC) and should be treated as already flat.  ProcessSignal
// will swallow this error silently and zero out the local position so the
// strategy stops looping SELL forever.
var errDustPosition = fmt.Errorf("position is dust (below min step), treating as flat")

// alertOrderFailure publishes a RiskAlert event so Telegram notifies the user
// when a signal cannot be executed (build error, insufficient balance, retries
// exhausted, etc). Without this, ProcessSignal failures are silent in logs only.
func (m *Manager) alertOrderFailure(sig *strategy.Signal, stage string, err error) {
	if m.bus == nil {
		return
	}
	m.bus.Publish(eventbus.Event{
		Type:      eventbus.EventRiskAlert,
		Timestamp: time.Now(),
		Payload: eventbus.RiskAlertEvent{
			Rule:    "order_failure",
			Message: fmt.Sprintf("❌ %s %s 下单失败 (%s): %v", sig.Symbol, sig.Action.String(), stage, err),
			Level:   "warning",
		},
	})
}

// ProcessSignal takes a strategy signal and executes an order if risk allows.
func (m *Manager) ProcessSignal(ctx context.Context, sig *strategy.Signal) error {
	if sig.Action == strategy.Hold {
		return nil
	}

	// Short/Cover signals are alert-only, never execute orders
	if sig.Action.IsShort() {
		return fmt.Errorf("short/cover signals are alert-only, not executable via order manager")
	}

	// Build order request from signal
	req, err := m.buildOrderRequest(ctx, sig)
	if err != nil {
		// Dust position on a SELL signal — silently flatten local state and
		// return success so the strategy stops looping. No Telegram alert.
		// Already-in-position on a BUY signal — sync strategy state from
		// the live position so it stops emitting BUYs every cycle, then
		// return success silently. No Telegram alert.
		if errors.Is(err, errAlreadyInPosition) {
			pos := m.position.GetPosition(sig.Symbol)
			entryPx := pos.AvgEntryPrice
			entryTime := time.Now()
			if entryPx <= 0 {
				if tk, tkErr := m.exchange.GetTicker(ctx, sig.Symbol); tkErr == nil {
					entryPx = tk.LastPrice
				}
			}
			// 从 trades 表查最后一笔 BUY 的真实入场时间,
			// 这样重启后 barsSinceEntry 能正确恢复,而不是从 0 开始。
			if m.store != nil {
				trades, tErr := m.store.GetTrades(ctx, storage.TradeFilter{
					Symbol: sig.Symbol,
					Limit:  5,
				})
				if tErr == nil {
					for _, t := range trades {
						if t.Side == "BUY" {
							entryTime = t.Timestamp
							if entryPx <= 0 {
								entryPx = t.Price
							}
							break
						}
					}
				}
			}
			if m.strategy != nil && entryPx > 0 {
				m.strategy.OnTradeExecuted(&exchange.Trade{
					Symbol:    sig.Symbol,
					Side:      exchange.OrderSideBuy,
					Quantity:  pos.Quantity,
					Price:     entryPx,
					Timestamp: entryTime,
				})
			}
			m.logger.Info("buy signal ignored: already in position",
				zap.String("symbol", sig.Symbol),
				zap.Float64("position_qty", pos.Quantity),
				zap.Float64("entry_price", entryPx),
			)
			return nil
		}
		if errors.Is(err, errDustPosition) {
			m.position.ForceFlat(sig.Symbol)
			// Also reset strategy state so it can BUY again later
			if m.strategy != nil {
				m.strategy.OnTradeExecuted(&exchange.Trade{
					Symbol:    sig.Symbol,
					Side:      exchange.OrderSideSell,
					Quantity:  0,
					Price:     0,
					Timestamp: time.Now(),
				})
			}
			m.logger.Info("sell signal on dust position, flattened locally",
				zap.String("symbol", sig.Symbol),
			)
			return nil
		}
		m.alertOrderFailure(sig, "build order", err)
		return fmt.Errorf("build order: %w", err)
	}

	// Cache ATR from signal for dynamic SL/TP placement
	if sig.Action == strategy.Buy && sig.Indicators != nil {
		if atr, ok := sig.Indicators["atr"]; ok && atr > 0 {
			m.mu.Lock()
			m.lastATR[sig.Symbol] = atr
			m.mu.Unlock()
		}
	}

	// Pre-trade risk check
	decision, err := m.risk.PreTradeCheck(ctx, req, sig)
	if err != nil {
		return fmt.Errorf("risk check: %w", err)
	}
	if !decision.Allowed {
		m.logger.Warn("order rejected by risk",
			zap.String("symbol", sig.Symbol),
			zap.String("action", sig.Action.String()),
			zap.String("reason", decision.Reason),
		)
		// Save signal as not executed
		if m.store != nil {
			m.store.SaveSignal(ctx, sig, false)
		}
		return nil
	}

	// 在 PlaceOrder 之前登记策略名。WS executionReport 经常比 REST 响应
	// 先到(毫秒级差异),如果在 PlaceOrder 返回后才登记就来不及了。
	// 用 symbol 做 key,因为 order ID 此时还不知道。
	if sig.Strategy != "" {
		m.mu.Lock()
		m.pendingStrategy[sig.Symbol] = sig.Strategy
		m.mu.Unlock()
	}

	// Place order with retry
	var order *exchange.Order
	maxRetries := 3
	for attempt := 0; attempt <= maxRetries; attempt++ {
		order, err = m.exchange.PlaceOrder(ctx, *req)
		if err == nil {
			break
		}
		if attempt < maxRetries {
			backoff := time.Duration(1<<uint(attempt)) * time.Second // 1s, 2s, 4s
			m.logger.Warn("order placement failed, retrying",
				zap.Int("attempt", attempt+1),
				zap.Duration("backoff", backoff),
				zap.Error(err),
			)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
			}
		}
	}
	if err != nil {
		m.alertOrderFailure(sig, "place order", err)
		return fmt.Errorf("place order after %d retries: %w", maxRetries, err)
	}

	// Track active order
	m.mu.Lock()
	m.activeOrders[order.ID] = order
	m.mu.Unlock()

	// Persist order
	if m.store != nil {
		m.store.SaveOrder(ctx, &storage.OrderRecord{
			ExchangeID:    order.ID,
			ClientOrderID: order.ClientOrderID,
			Symbol:        order.Symbol,
			Side:          order.Side.String(),
			Type:          order.Type.String(),
			Status:        order.Status.String(),
			Price:         order.Price,
			Quantity:      order.Quantity,
			StrategyName:  sig.Strategy,
			SignalReason:  sig.Reason,
			CreatedAt:     order.CreatedAt,
			UpdatedAt:     order.UpdatedAt,
		})
	}

	// Save signal as executed
	if m.store != nil {
		m.store.SaveSignal(ctx, sig, true)
	}

	// 注:不在这里发布 EventOrderUpdate。Binance 现货市价单 REST 响应
	// 通常已经带 FILLED 状态,如果这里再发一次,Telegram 就会收到两条
	// "订单 FILLED" 消息 —— 一条从这里,一条从下面 onOrderFilled line 744。
	// 让 onOrderFilled 成为 FILLED 事件的唯一发布者。

	// If market order, it's likely already filled — update position
	if order.Status == exchange.OrderStatusFilled {
		m.onOrderFilled(ctx, order, sig.Strategy)
	}

	return nil
}

// Run starts listening for user data events, kline updates for trailing stop,
// and polls open order status (fallback for deprecated userDataStream).
func (m *Manager) Run(ctx context.Context) error {
	userDataCh := m.bus.Subscribe(eventbus.EventAccountUpdate, 100)
	klineCh := m.bus.Subscribe(eventbus.EventKlineUpdate, 1000)

	// Poll open orders every 10 seconds as fallback for broken userDataStream
	pollTicker := time.NewTicker(10 * time.Second)
	defer pollTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case evt, ok := <-userDataCh:
			if !ok {
				return nil
			}
			if ude, ok := evt.Payload.(eventbus.OrderUpdateEvent); ok {
				m.handleOrderUpdate(ctx, &ude.Order)
			}
		case evt, ok := <-klineCh:
			if !ok {
				return nil
			}
			if ke, ok := evt.Payload.(eventbus.KlineEvent); ok {
				// Only check on 1m klines for responsiveness
				if ke.Interval == "1m" {
					m.checkTrailingStop(ctx, ke.Symbol, ke.Kline.Close)
					m.checkEmergencyStop(ctx, ke.Symbol, ke.Kline.Close)
					m.checkPeakDrawdownAlert(ctx, ke.Symbol, ke.Kline.Close)
				}
			}
		case <-pollTicker.C:
			m.pollActiveOrders(ctx)
		}
	}
}

// pollActiveOrders checks each tracked active order via REST API.
// This is the fallback mechanism for when userDataStream is unavailable (410 Gone).
func (m *Manager) pollActiveOrders(ctx context.Context) {
	m.mu.RLock()
	ordersCopy := make(map[int64]*exchange.Order, len(m.activeOrders))
	for id, o := range m.activeOrders {
		ordersCopy[id] = o
	}
	m.mu.RUnlock()

	if len(ordersCopy) == 0 {
		return
	}

	for id, tracked := range ordersCopy {
		order, err := m.exchange.GetOrder(ctx, tracked.Symbol, id)
		if err != nil {
			m.logger.Debug("poll order status failed", zap.Int64("order_id", id), zap.Error(err))
			continue
		}

		// Only process if status changed
		if order.Status != tracked.Status {
			m.logger.Info("order status changed (poll)",
				zap.Int64("order_id", id),
				zap.String("old_status", tracked.Status.String()),
				zap.String("new_status", order.Status.String()),
			)
			m.handleOrderUpdate(ctx, order)
		}
	}
}

// CancelAllOrders cancels all open orders for a symbol.
func (m *Manager) CancelAllOrders(ctx context.Context, symbol string) error {
	orders, err := m.exchange.GetOpenOrders(ctx, symbol)
	if err != nil {
		return fmt.Errorf("get open orders: %w", err)
	}

	for _, o := range orders {
		if err := m.exchange.CancelOrder(ctx, symbol, o.ID); err != nil {
			m.logger.Error("cancel order failed",
				zap.Int64("order_id", o.ID),
				zap.Error(err),
			)
		}
	}

	// Clean up SL/TP tracking
	m.mu.Lock()
	delete(m.slOrders, symbol)
	delete(m.tpOrders, symbol)
	delete(m.trailingHighs, symbol)
	m.mu.Unlock()

	return nil
}

// GetActiveOrders returns currently tracked active orders.
func (m *Manager) GetActiveOrders() []*exchange.Order {
	m.mu.RLock()
	defer m.mu.RUnlock()

	orders := make([]*exchange.Order, 0, len(m.activeOrders))
	for _, o := range m.activeOrders {
		orders = append(orders, o)
	}
	return orders
}

func (m *Manager) buildOrderRequest(ctx context.Context, sig *strategy.Signal) (*exchange.OrderRequest, error) {
	// For simplicity, use market orders
	req := &exchange.OrderRequest{
		Symbol: sig.Symbol,
		Type:   exchange.OrderTypeMarket,
	}

	if sig.Action == strategy.Buy {
		req.Side = exchange.OrderSideBuy
	} else {
		req.Side = exchange.OrderSideSell
	}

	// Calculate quantity based on signal strength and risk config
	ticker, err := m.exchange.GetTicker(ctx, sig.Symbol)
	if err != nil {
		return nil, fmt.Errorf("get ticker for sizing: %w", err)
	}

	balance, err := m.exchange.GetBalance(ctx, "USDT")
	if err != nil {
		return nil, fmt.Errorf("get balance: %w", err)
	}

	if sig.Action == strategy.Buy {
		// Guard: 已经持有该 symbol → 拒绝再次买入。
		// 触发场景: trader 重启后, position manager 从交易所同步出已有 BTC,
		// 但 strategy 内部状态是空的, 仍会发出 BUY 信号。USDT 几乎为 0,
		// 计算出的 quantity 必然低于 minQty, 每根 K 线触发一次 Telegram 风控告警。
		// 此处直接返回 errAlreadyInPosition, 由 ProcessSignal 同步策略状态后静默退出。
		existing := m.position.GetPosition(sig.Symbol)
		// 用美元价值判断是否真正持仓,与 SyncFromAccount 的 $10 dust 阈值对齐。
		// 避免交易所残留 dust（如 0.00004 BTC ≈ $3）误触拦截。
		existingValue := existing.Quantity * ticker.LastPrice
		if existingValue >= 10 {
			return nil, errAlreadyInPosition
		}

		// 买入: 使用 alloc_pct 配置决定仓位大小
		allocPct := m.riskCfg.AllocPct
		if allocPct <= 0 {
			allocPct = 0.5 // 默认 50%
		}
		if allocPct > 1.0 {
			allocPct = 1.0
		}
		allocUSDT := balance.Free * allocPct

		// Binance 现货 BUY 手续费从收到的 BTC 里扣,USDT 侧不扣费。
		// 但市价单实际成交价可能略高于 askPrice（滑点），如果 qty × 实际价 > free
		// 就会报 -2010 insufficient balance。预留 0.1% 缓冲防滑点。
		if ticker.AskPrice > 0 {
			req.Quantity = allocUSDT / (ticker.AskPrice * 1.001)
		}

		// 按 stepSize 截断并检查最小值
		adjQty, err := m.adjustQuantity(sig.Symbol, req.Quantity, ticker.AskPrice)
		if err != nil {
			return nil, fmt.Errorf("adjust buy quantity: %w", err)
		}
		req.Quantity = adjQty
	} else {
		// 卖出: 使用当前持仓数量，但绝不超过交易所可用 free 余额
		// （Locked 部分被 SL/TP 等挂单占用，无法直接卖出，否则 Binance 报 -2010）
		pos := m.position.GetPosition(sig.Symbol)
		if pos.Quantity <= 0 {
			return nil, errDustPosition
		}
		// Position 介于 0 和 1 个 stepSize 之间 → 不可卖，视为 dust。
		if pos.Quantity < 1e-5 {
			return nil, errDustPosition
		}

		// Extract base asset from pair (e.g. "BTCUSDT" -> "BTC")
		baseAsset := strings.TrimSuffix(sig.Symbol, "USDT")
		baseBal, err := m.exchange.GetBalance(ctx, baseAsset)
		if err != nil {
			return nil, fmt.Errorf("get base balance %s: %w", baseAsset, err)
		}

		sellQty := pos.Quantity
		if baseBal.Free < sellQty {
			m.logger.Warn("sell qty capped by free balance",
				zap.String("symbol", sig.Symbol),
				zap.Float64("position_qty", pos.Quantity),
				zap.Float64("free_balance", baseBal.Free),
				zap.Float64("locked", baseBal.Locked),
			)
			sellQty = baseBal.Free
		}

		// 在卖出之前先取消所有挂着的 SL/TP 单，把 Locked 释放成 Free，
		// 否则市价卖出会因为 free 不够而失败。
		if baseBal.Locked > 1e-8 {
			m.logger.Info("cancelling existing SL/TP before market sell",
				zap.String("symbol", sig.Symbol),
				zap.Float64("locked", baseBal.Locked),
			)
			m.cancelExistingSLTP(ctx, sig.Symbol)
			// 重新查询 free 余额（取消可能还没立即生效，给一个简单等待）
			time.Sleep(500 * time.Millisecond)
			if rebal, err := m.exchange.GetBalance(ctx, baseAsset); err == nil {
				sellQty = math.Min(pos.Quantity, rebal.Free)
			}
		}

		if sellQty < 1e-5 {
			return nil, errDustPosition
		}

		adjQty, err := m.adjustQuantity(sig.Symbol, sellQty, ticker.BidPrice)
		if err != nil {
			// Most common cause here is "quantity below minQty" — the residual
			// is dust, not a real error. Treat the same as errDustPosition.
			return nil, errDustPosition
		}
		req.Quantity = adjQty
	}

	return req, nil
}

func (m *Manager) handleOrderUpdate(ctx context.Context, order *exchange.Order) {
	m.mu.Lock()
	if _, exists := m.activeOrders[order.ID]; exists {
		m.activeOrders[order.ID] = order
	}
	m.mu.Unlock()

	switch order.Status {
	case exchange.OrderStatusFilled:
		// Check if this was a SL or TP fill
		m.handleSLTPFilled(order)
		m.onOrderFilled(ctx, order, "")
		m.mu.Lock()
		delete(m.activeOrders, order.ID)
		m.mu.Unlock()

	case exchange.OrderStatusPartiallyFilled:
		// Track partial fill — update position with what we've received so far
		m.logger.Warn("order partially filled",
			zap.Int64("order_id", order.ID),
			zap.String("symbol", order.Symbol),
			zap.Float64("filled", order.FilledQty),
			zap.Float64("total", order.Quantity),
		)
		// Position will be fully updated when FILLED arrives.
		// Keep tracking in activeOrders.

	case exchange.OrderStatusCanceled, exchange.OrderStatusRejected, exchange.OrderStatusExpired:
		// If partially filled before cancel, record the partial fill as a trade
		if order.FilledQty > 0 {
			m.logger.Warn("order ended with partial fill",
				zap.Int64("order_id", order.ID),
				zap.Float64("filled_qty", order.FilledQty),
				zap.String("status", order.Status.String()),
			)
			partialOrder := *order
			partialOrder.Status = exchange.OrderStatusFilled
			partialOrder.Quantity = order.FilledQty
			m.onOrderFilled(ctx, &partialOrder, "partial_"+order.Status.String())
		}
		m.mu.Lock()
		delete(m.activeOrders, order.ID)
		m.mu.Unlock()
	}
}

// handleSLTPFilled cleans up SL/TP state when one of them fills.
// When SL fills, cancel TP, and vice versa (OCO-like behavior).
func (m *Manager) handleSLTPFilled(order *exchange.Order) {
	m.mu.Lock()
	defer m.mu.Unlock()

	symbol := order.Symbol

	// If SL was filled, cancel TP
	if slID, ok := m.slOrders[symbol]; ok && slID == order.ID {
		delete(m.slOrders, symbol)
		delete(m.trailingHighs, symbol)
		if tpID, tpOk := m.tpOrders[symbol]; tpOk {
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				if err := m.exchange.CancelOrder(ctx, symbol, tpID); err != nil {
					m.logger.Warn("cancel TP after SL fill", zap.Error(err))
				}
			}()
			delete(m.tpOrders, symbol)
		}
		m.logger.Info("stop-loss triggered",
			zap.String("symbol", symbol),
			zap.Float64("price", order.AvgPrice),
		)
		return
	}

	// If TP was filled, cancel SL
	if tpID, ok := m.tpOrders[symbol]; ok && tpID == order.ID {
		delete(m.tpOrders, symbol)
		delete(m.trailingHighs, symbol)
		if slID, slOk := m.slOrders[symbol]; slOk {
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				if err := m.exchange.CancelOrder(ctx, symbol, slID); err != nil {
					m.logger.Warn("cancel SL after TP fill", zap.Error(err))
				}
			}()
			delete(m.slOrders, symbol)
		}
		m.logger.Info("take-profit triggered",
			zap.String("symbol", symbol),
			zap.Float64("price", order.AvgPrice),
		)
		return
	}
}

func (m *Manager) onOrderFilled(ctx context.Context, order *exchange.Order, strategyName string) {
	// Dedup: any given order ID may only be processed ONCE.
	// ProcessSignal's immediate-FILLED branch and pollActiveOrders/WS callback
	// can both fire for the same market order. Without this guard the position
	// gets double-decremented and the entire local state diverges from Binance.
	m.mu.Lock()
	if _, already := m.filledOrderIDs[order.ID]; already {
		m.mu.Unlock()
		m.logger.Debug("onOrderFilled: skipping duplicate", zap.Int64("order_id", order.ID))
		return
	}
	m.filledOrderIDs[order.ID] = time.Now()
	// GC: drop entries older than 1h to prevent unbounded growth
	cutoff := time.Now().Add(-1 * time.Hour)
	for id, t := range m.filledOrderIDs {
		if t.Before(cutoff) {
			delete(m.filledOrderIDs, id)
		}
	}
	// 兜底:strategyName 为空时从 pendingStrategy map 查。
	// WS executionReport 经常比 REST PlaceOrder 响应先到,
	// 导致 handleOrderUpdate 路径以 strategyName="" 进来。
	// ProcessSignal 在 PlaceOrder 之前已按 symbol 登记了策略名。
	if strategyName == "" {
		if s, ok := m.pendingStrategy[order.Symbol]; ok {
			strategyName = s
		}
	}
	// 用完即删,防止 map 无限增长。
	delete(m.pendingStrategy, order.Symbol)
	m.mu.Unlock()

	// Update position
	trade := &exchange.Trade{
		OrderID:   order.ID,
		Symbol:    order.Symbol,
		Side:      order.Side,
		Price:     order.AvgPrice,
		Quantity:  order.FilledQty,
		Timestamp: order.UpdatedAt,
	}
	if trade.Price == 0 {
		trade.Price = order.Price
	}

	// 捕获 OnTrade 前后 RealizedPnL 的差值,作为本笔 trade 的实际已实现盈亏。
	// pos.RealizedPnL 是累计值,如果直接把累计值存进 trades 表,BUY 单会把
	// 上一笔 SELL 的盈亏继续带出来(观测到 BUY realized_pnl = -51.63,
	// 和上一笔 SELL 完全一样),导致下游统计重复计数。
	prevRealized := m.position.GetPosition(order.Symbol).RealizedPnL
	m.position.OnTrade(trade)
	newRealized := m.position.GetPosition(order.Symbol).RealizedPnL
	tradeRealizedPnL := newRealized - prevRealized

	// Authoritative reconciliation: re-fetch real balance from the exchange
	// and overwrite local quantity. This makes the local state self-correcting
	// even if any double-processing or missed updates occur upstream.
	baseAsset := strings.TrimSuffix(order.Symbol, "USDT")
	if bal, balErr := m.exchange.GetBalance(ctx, baseAsset); balErr == nil {
		var px float64
		if tk, tkErr := m.exchange.GetTicker(ctx, order.Symbol); tkErr == nil {
			px = tk.LastPrice
		}
		m.position.ReconcileFromAccount(order.Symbol, baseAsset, bal.Free, px)
	} else {
		m.logger.Warn("post-fill reconcile: get balance failed",
			zap.String("asset", baseAsset),
			zap.Error(balErr),
		)
	}

	// Post-trade risk check
	m.risk.PostTradeCheck(ctx, trade)

	// Persist trade
	if m.store != nil {
		m.store.SaveTrade(ctx, &storage.TradeRecord{
			ExchangeID:   order.ID,
			OrderID:      order.ID,
			Symbol:       order.Symbol,
			Side:         order.Side.String(),
			Price:        trade.Price,
			Quantity:     trade.Quantity,
			StrategyName: strategyName,
			RealizedPnL:  tradeRealizedPnL, // 本笔 trade 的增量,而非累计
			Timestamp:    trade.Timestamp,
			CreatedAt:    time.Now(),
		})

		// Update order record with fill data (market orders have price=0 at insertion)
		m.store.UpdateOrder(ctx, &storage.OrderRecord{
			ExchangeID: order.ID,
			Status:     order.Status.String(),
			FilledQty:  order.FilledQty,
			AvgPrice:   order.AvgPrice,
			UpdatedAt:  time.Now(),
		})
	}

	// After a BUY fill, place automatic SL/TP orders
	if order.Side == exchange.OrderSideBuy {
		m.placeSLTPOrders(ctx, order)
	}

	// If a SELL closes the position, clean up SL/TP state
	if order.Side == exchange.OrderSideSell {
		pos := m.position.GetPosition(order.Symbol)
		if pos.Quantity <= 0 {
			m.cleanupSLTP(ctx, order.Symbol)
		}
	}

	// Publish position update
	pos := m.position.GetPosition(order.Symbol)
	m.bus.Publish(eventbus.Event{
		Type:      eventbus.EventPositionUpdate,
		Timestamp: time.Now(),
		Payload: eventbus.PositionUpdateEvent{
			Symbol:        order.Symbol,
			Quantity:      pos.Quantity,
			AvgEntryPrice: pos.AvgEntryPrice,
			UnrealizedPnL: pos.UnrealizedPnL,
			RealizedPnL:   pos.RealizedPnL,
		},
	})

	// Publish FILLED order update so Telegram can notify the user.
	// The earlier publish in ProcessSignal sends NEW status which Telegram
	// ignores; this is the canonical "trade executed" event.
	filledOrder := *order
	filledOrder.Status = exchange.OrderStatusFilled
	m.bus.Publish(eventbus.Event{
		Type:      eventbus.EventOrderUpdate,
		Timestamp: time.Now(),
		Payload:   eventbus.OrderUpdateEvent{Order: filledOrder},
	})

	// Sync strategy internal state (entryPrice, highWaterMark, cooldown, etc.).
	// Without this the strategy never enters cooldown after a sell, and entryPrice
	// stays 0 forever, breaking ATR trailing-stop tightening.
	if m.strategy != nil {
		m.strategy.OnTradeExecuted(trade)
	}

	m.logger.Info("order filled",
		zap.Int64("order_id", order.ID),
		zap.String("symbol", order.Symbol),
		zap.String("side", order.Side.String()),
		zap.Float64("price", trade.Price),
		zap.Float64("qty", trade.Quantity),
	)
}

// placeSLTPOrders places stop-loss and take-profit orders after a buy fill.
// Uses ATR-based dynamic levels when available, falls back to fixed percentage.
func (m *Manager) placeSLTPOrders(ctx context.Context, buyOrder *exchange.Order) {
	entryPrice := buyOrder.AvgPrice
	if entryPrice == 0 {
		entryPrice = buyOrder.Price
	}
	qty := buyOrder.FilledQty
	symbol := buyOrder.Symbol

	if entryPrice == 0 || qty == 0 {
		return
	}

	// Try ATR-based dynamic SL/TP first, fall back to fixed percentage
	m.mu.RLock()
	atr := m.lastATR[symbol]
	m.mu.RUnlock()

	var slPrice, tpPrice float64
	var slMethod, tpMethod string

	// Stop-loss calculation
	if atr > 0 && m.riskCfg.ATRStopMultiplier > 0 {
		// ATR-based: SL = entry - ATR * multiplier
		slPrice = entryPrice - atr*m.riskCfg.ATRStopMultiplier
		slMethod = fmt.Sprintf("ATR(%.0f)*%.1f", atr, m.riskCfg.ATRStopMultiplier)
	} else if m.riskCfg.DefaultStopLossPct > 0 {
		// Fixed percentage fallback
		slPrice = entryPrice * (1 - m.riskCfg.DefaultStopLossPct)
		slMethod = fmt.Sprintf("fixed %.1f%%", m.riskCfg.DefaultStopLossPct*100)
	}

	// Take-profit calculation
	if atr > 0 && m.riskCfg.ATRTPMultiplier > 0 {
		// ATR-based: TP = entry + ATR * multiplier
		tpPrice = entryPrice + atr*m.riskCfg.ATRTPMultiplier
		tpMethod = fmt.Sprintf("ATR(%.0f)*%.1f", atr, m.riskCfg.ATRTPMultiplier)
	} else if m.riskCfg.DefaultTakeProfitPct > 0 {
		// Fixed percentage fallback
		tpPrice = entryPrice * (1 + m.riskCfg.DefaultTakeProfitPct)
		tpMethod = fmt.Sprintf("fixed %.1f%%", m.riskCfg.DefaultTakeProfitPct*100)
	}

	// Place stop-loss
	if slPrice > 0 {
		slPrice = roundPrice(slPrice)
		slPct := (entryPrice - slPrice) / entryPrice * 100

		slOrder, err := m.exchange.PlaceOrder(ctx, exchange.OrderRequest{
			Symbol:    symbol,
			Side:      exchange.OrderSideSell,
			Type:      exchange.OrderTypeStopLoss,
			Quantity:  qty,
			StopPrice: slPrice,
		})
		if err != nil {
			m.logger.Error("place stop-loss failed",
				zap.String("symbol", symbol),
				zap.Float64("sl_price", slPrice),
				zap.Error(err),
			)
		} else {
			m.mu.Lock()
			m.slOrders[symbol] = slOrder.ID
			m.activeOrders[slOrder.ID] = slOrder
			m.mu.Unlock()

			m.logger.Info("stop-loss placed",
				zap.String("symbol", symbol),
				zap.String("method", slMethod),
				zap.Float64("entry", entryPrice),
				zap.Float64("sl_price", slPrice),
				zap.Float64("sl_pct", slPct),
			)
		}
	}

	// Place take-profit
	if tpPrice > 0 {
		tpPrice = roundPrice(tpPrice)
		tpPct := (tpPrice - entryPrice) / entryPrice * 100

		tpOrder, err := m.exchange.PlaceOrder(ctx, exchange.OrderRequest{
			Symbol:    symbol,
			Side:      exchange.OrderSideSell,
			Type:      exchange.OrderTypeTakeProfit,
			Quantity:  qty,
			StopPrice: tpPrice,
		})
		if err != nil {
			m.logger.Error("place take-profit failed",
				zap.String("symbol", symbol),
				zap.Float64("tp_price", tpPrice),
				zap.Error(err),
			)
		} else {
			m.mu.Lock()
			m.tpOrders[symbol] = tpOrder.ID
			m.activeOrders[tpOrder.ID] = tpOrder
			m.mu.Unlock()

			m.logger.Info("take-profit placed",
				zap.String("symbol", symbol),
				zap.String("method", tpMethod),
				zap.Float64("entry", entryPrice),
				zap.Float64("tp_price", tpPrice),
				zap.Float64("tp_pct", tpPct),
			)
		}
	}

	// Initialize trailing stop tracking
	if m.riskCfg.TrailingStopEnabled && m.riskCfg.TrailingStopPct > 0 {
		m.mu.Lock()
		m.trailingHighs[symbol] = entryPrice
		m.mu.Unlock()

		m.logger.Info("trailing stop initialized",
			zap.String("symbol", symbol),
			zap.Float64("entry", entryPrice),
			zap.Float64("trail_pct", m.riskCfg.TrailingStopPct*100),
		)
	}

	// Clean up cached ATR
	m.mu.Lock()
	delete(m.lastATR, symbol)
	m.mu.Unlock()
}

// checkTrailingStop evaluates the trailing stop condition on each price tick.
// If the price drops below (highestPrice * (1 - trailingStopPct)), sell the position.
func (m *Manager) checkTrailingStop(ctx context.Context, symbol string, currentPrice float64) {
	if !m.riskCfg.TrailingStopEnabled || m.riskCfg.TrailingStopPct <= 0 {
		return
	}

	m.mu.Lock()
	highPrice, tracked := m.trailingHighs[symbol]
	if !tracked {
		m.mu.Unlock()
		return
	}

	// Update highest price
	if currentPrice > highPrice {
		m.trailingHighs[symbol] = currentPrice
		m.mu.Unlock()
		return
	}
	m.mu.Unlock()

	// Check if trailing stop should trigger
	trailStop := highPrice * (1 - m.riskCfg.TrailingStopPct)
	if currentPrice > trailStop {
		return
	}

	// Trailing stop triggered — sell the entire position
	pos := m.position.GetPosition(symbol)
	if pos.Quantity <= 0 {
		m.mu.Lock()
		delete(m.trailingHighs, symbol)
		m.mu.Unlock()
		return
	}

	m.logger.Warn("trailing stop triggered",
		zap.String("symbol", symbol),
		zap.Float64("current_price", currentPrice),
		zap.Float64("high_price", highPrice),
		zap.Float64("trail_stop", trailStop),
	)

	// Cancel existing SL/TP first
	m.cancelExistingSLTP(ctx, symbol)

	// 在 PlaceOrder 之前登记策略名,防止 WS 先到时丢失。
	m.mu.Lock()
	m.pendingStrategy[symbol] = "trailing_stop"
	m.mu.Unlock()

	// Place market sell
	sellOrder, err := m.exchange.PlaceOrder(ctx, exchange.OrderRequest{
		Symbol:   symbol,
		Side:     exchange.OrderSideSell,
		Type:     exchange.OrderTypeMarket,
		Quantity: pos.Quantity,
	})
	if err != nil {
		m.logger.Error("trailing stop sell failed", zap.Error(err))
		return
	}

	m.mu.Lock()
	m.activeOrders[sellOrder.ID] = sellOrder
	delete(m.trailingHighs, symbol)
	m.mu.Unlock()

	if sellOrder.Status == exchange.OrderStatusFilled {
		m.onOrderFilled(ctx, sellOrder, "trailing_stop")
	}

	m.bus.Publish(eventbus.Event{
		Type:      eventbus.EventRiskAlert,
		Timestamp: time.Now(),
		Payload: eventbus.RiskAlertEvent{
			Rule:    "trailing_stop",
			Message: fmt.Sprintf("trailing stop triggered for %s at %.2f (high: %.2f)", symbol, currentPrice, highPrice),
			Level:   "warning",
		},
	})
}

// checkEmergencyStop monitors unrealized loss on every 1m kline.
// When loss exceeds emergency_stop_pct, it sends an alert (Telegram + event bus)
// but does NOT auto-sell — spot positions don't get liquidated, and flash crashes
// often V-shape recover. The human decides whether to intervene.
func (m *Manager) checkEmergencyStop(ctx context.Context, symbol string, currentPrice float64) {
	if m.riskCfg.EmergencyAlertPct <= 0 {
		return
	}

	pos := m.position.GetPosition(symbol)
	if pos.Quantity <= 0 || pos.AvgEntryPrice <= 0 {
		return
	}

	lossPct := (pos.AvgEntryPrice - currentPrice) / pos.AvgEntryPrice
	if lossPct < m.riskCfg.EmergencyAlertPct {
		// Price recovered above threshold — reset alert so it can fire again
		m.mu.Lock()
		delete(m.emergencyAlerted, symbol)
		m.mu.Unlock()
		return
	}

	// Deduplicate: only alert once per breach (until price recovers above threshold)
	m.mu.Lock()
	if m.emergencyAlerted[symbol] {
		m.mu.Unlock()
		return
	}
	m.emergencyAlerted[symbol] = true
	m.mu.Unlock()

	m.logger.Warn("EMERGENCY ALERT: large unrealized loss",
		zap.String("symbol", symbol),
		zap.Float64("current_price", currentPrice),
		zap.Float64("entry_price", pos.AvgEntryPrice),
		zap.Float64("loss_pct", lossPct*100),
		zap.Float64("threshold_pct", m.riskCfg.EmergencyAlertPct*100),
	)

	m.bus.Publish(eventbus.Event{
		Type:      eventbus.EventRiskAlert,
		Timestamp: time.Now(),
		Payload: eventbus.RiskAlertEvent{
			Rule:    "emergency_alert",
			Message: fmt.Sprintf("⚠️ %s 浮亏 %.1f%% (入场=%.2f, 现价=%.2f, 阈值=%.0f%%), 请关注是否需要手动平仓", symbol, lossPct*100, pos.AvgEntryPrice, currentPrice, m.riskCfg.EmergencyAlertPct*100),
			Level:   "critical",
		},
	})
}

// checkPeakDrawdownAlert monitors price drop from the highest point since entry.
// Protects unrealized profit: e.g., up 10% then drops 8% from peak → alert.
// Uses trailingHighs which is already updated by checkTrailingStop.
func (m *Manager) checkPeakDrawdownAlert(ctx context.Context, symbol string, currentPrice float64) {
	if m.riskCfg.PeakDrawdownAlertPct <= 0 {
		return
	}

	pos := m.position.GetPosition(symbol)
	if pos.Quantity <= 0 {
		return
	}

	m.mu.RLock()
	highPrice := m.trailingHighs[symbol]
	m.mu.RUnlock()

	if highPrice <= 0 {
		return
	}

	drawdownPct := (highPrice - currentPrice) / highPrice
	if drawdownPct < m.riskCfg.PeakDrawdownAlertPct {
		// Below threshold — reset alert
		m.mu.Lock()
		delete(m.drawdownAlerted, symbol)
		m.mu.Unlock()
		return
	}

	// Deduplicate
	m.mu.Lock()
	if m.drawdownAlerted[symbol] {
		m.mu.Unlock()
		return
	}
	m.drawdownAlerted[symbol] = true
	m.mu.Unlock()

	profitFromEntry := (currentPrice - pos.AvgEntryPrice) / pos.AvgEntryPrice * 100
	profitAtPeak := (highPrice - pos.AvgEntryPrice) / pos.AvgEntryPrice * 100

	m.logger.Warn("PEAK DRAWDOWN ALERT: significant pullback from high",
		zap.String("symbol", symbol),
		zap.Float64("peak_price", highPrice),
		zap.Float64("current_price", currentPrice),
		zap.Float64("drawdown_pct", drawdownPct*100),
		zap.Float64("profit_at_peak_pct", profitAtPeak),
		zap.Float64("profit_now_pct", profitFromEntry),
	)

	m.bus.Publish(eventbus.Event{
		Type:      eventbus.EventRiskAlert,
		Timestamp: time.Now(),
		Payload: eventbus.RiskAlertEvent{
			Rule:    "peak_drawdown_alert",
			Message: fmt.Sprintf("📉 %s 从最高点回撤 %.1f%% (最高=%.2f, 现价=%.2f, 当前盈亏=%.1f%%, 峰值盈利=%.1f%%)", symbol, drawdownPct*100, highPrice, currentPrice, profitFromEntry, profitAtPeak),
			Level:   "warning",
		},
	})
}

// cancelExistingSLTP cancels any active SL/TP orders for a symbol.
func (m *Manager) cancelExistingSLTP(ctx context.Context, symbol string) {
	m.mu.Lock()
	slID := m.slOrders[symbol]
	tpID := m.tpOrders[symbol]
	delete(m.slOrders, symbol)
	delete(m.tpOrders, symbol)
	m.mu.Unlock()

	if slID != 0 {
		if err := m.exchange.CancelOrder(ctx, symbol, slID); err != nil {
			m.logger.Warn("cancel existing SL", zap.Error(err))
		}
	}
	if tpID != 0 {
		if err := m.exchange.CancelOrder(ctx, symbol, tpID); err != nil {
			m.logger.Warn("cancel existing TP", zap.Error(err))
		}
	}
}

// cleanupSLTP removes all SL/TP state for a symbol when position is closed.
func (m *Manager) cleanupSLTP(ctx context.Context, symbol string) {
	m.cancelExistingSLTP(ctx, symbol)

	m.mu.Lock()
	delete(m.trailingHighs, symbol)
	m.mu.Unlock()
}

// roundPrice rounds price to 2 decimal places to avoid floating point issues.
func roundPrice(p float64) float64 {
	return math.Round(p*100) / 100
}
