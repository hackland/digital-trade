package order

import (
	"context"
	"fmt"
	"math"
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

	mu           sync.RWMutex
	activeOrders map[int64]*exchange.Order

	// SL/TP tracking per symbol
	slOrders      map[string]int64   // symbol → SL order ID
	tpOrders      map[string]int64   // symbol → TP order ID
	trailingHighs map[string]float64 // symbol → highest price since entry (for trailing stop)
	lastATR       map[string]float64 // symbol → ATR at signal time (for dynamic SL/TP)
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
		exchange:      ex,
		risk:          riskMgr,
		position:      pos,
		store:         store,
		bus:           bus,
		logger:        logger,
		activeOrders:  make(map[int64]*exchange.Order),
		slOrders:      make(map[string]int64),
		tpOrders:      make(map[string]int64),
		trailingHighs: make(map[string]float64),
		lastATR:       make(map[string]float64),
	}
}

// SetRiskConfig allows injecting the risk config for SL/TP settings.
func (m *Manager) SetRiskConfig(cfg config.RiskConfig) {
	m.riskCfg = cfg
}

// ProcessSignal takes a strategy signal and executes an order if risk allows.
func (m *Manager) ProcessSignal(ctx context.Context, sig *strategy.Signal) error {
	if sig.Action == strategy.Hold {
		return nil
	}

	// Build order request from signal
	req, err := m.buildOrderRequest(ctx, sig)
	if err != nil {
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

	// Publish order event
	m.bus.Publish(eventbus.Event{
		Type:      eventbus.EventOrderUpdate,
		Timestamp: time.Now(),
		Payload:   eventbus.OrderUpdateEvent{Order: *order},
	})

	// If market order, it's likely already filled — update position
	if order.Status == exchange.OrderStatusFilled {
		m.onOrderFilled(ctx, order, sig.Strategy)
	}

	return nil
}

// Run starts listening for user data events and kline updates for trailing stop.
func (m *Manager) Run(ctx context.Context) error {
	userDataCh := m.bus.Subscribe(eventbus.EventAccountUpdate, 100)
	klineCh := m.bus.Subscribe(eventbus.EventKlineUpdate, 1000)

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
				// Only check trailing stop on 1m klines for responsiveness
				if ke.Interval == "1m" {
					m.checkTrailingStop(ctx, ke.Symbol, ke.Kline.Close)
				}
			}
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
		// 买入: 使用 alloc_pct 配置决定仓位大小
		allocPct := m.riskCfg.AllocPct
		if allocPct <= 0 {
			allocPct = 0.5 // 默认 50%
		}
		if allocPct > 1.0 {
			allocPct = 1.0
		}
		allocUSDT := balance.Free * allocPct

		if ticker.AskPrice > 0 {
			req.Quantity = allocUSDT / ticker.AskPrice
		}
	} else {
		// 卖出: 使用当前持仓全部数量
		pos := m.position.GetPosition(sig.Symbol)
		if pos.Quantity > 0 {
			req.Quantity = pos.Quantity
		} else {
			return nil, fmt.Errorf("no position to sell for %s", sig.Symbol)
		}
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

	m.position.OnTrade(trade)

	// Post-trade risk check
	m.risk.PostTradeCheck(ctx, trade)

	// Persist trade
	if m.store != nil {
		pos := m.position.GetPosition(order.Symbol)
		m.store.SaveTrade(ctx, &storage.TradeRecord{
			ExchangeID:   order.ID,
			OrderID:      order.ID,
			Symbol:       order.Symbol,
			Side:         order.Side.String(),
			Price:        trade.Price,
			Quantity:     trade.Quantity,
			StrategyName: strategyName,
			RealizedPnL:  pos.RealizedPnL,
			Timestamp:    trade.Timestamp,
			CreatedAt:    time.Now(),
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
