package position

import (
	"strings"
	"sync"
	"time"

	"github.com/jayce/btc-trader/internal/exchange"
	"go.uber.org/zap"
)

// Position represents a current position in a symbol.
type Position struct {
	Symbol        string  `json:"symbol"`
	Quantity      float64 `json:"quantity"` // Positive = long, negative would be short (for futures)
	AvgEntryPrice float64 `json:"avg_entry_price"`
	CurrentPrice  float64 `json:"current_price"`
	UnrealizedPnL float64 `json:"unrealized_pnl"`
	RealizedPnL   float64 `json:"realized_pnl"`
	Side          string  `json:"side"` // "LONG", "FLAT"
}

// Manager tracks positions across symbols.
type Manager struct {
	mu        sync.RWMutex
	positions map[string]*Position
	logger    *zap.Logger
}

// NewManager creates a new position manager.
func NewManager(logger *zap.Logger) *Manager {
	return &Manager{
		positions: make(map[string]*Position),
		logger:    logger,
	}
}

// GetPosition returns the current position for a symbol.
func (m *Manager) GetPosition(symbol string) *Position {
	m.mu.RLock()
	defer m.mu.RUnlock()

	pos, ok := m.positions[symbol]
	if !ok {
		return &Position{Symbol: symbol, Side: "FLAT"}
	}
	return pos
}

// GetAllPositions returns all current positions.
func (m *Manager) GetAllPositions() map[string]*Position {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make(map[string]*Position, len(m.positions))
	for k, v := range m.positions {
		cp := *v
		result[k] = &cp
	}
	return result
}

// OnTrade updates position based on a trade execution.
func (m *Manager) OnTrade(trade *exchange.Trade) {
	m.mu.Lock()
	defer m.mu.Unlock()

	pos, ok := m.positions[trade.Symbol]
	if !ok {
		pos = &Position{Symbol: trade.Symbol, Side: "FLAT"}
		m.positions[trade.Symbol] = pos
	}

	tradeQty := trade.Quantity
	if trade.Side == exchange.OrderSideSell {
		tradeQty = -tradeQty
	}

	oldQty := pos.Quantity
	newQty := oldQty + tradeQty

	if trade.Side == exchange.OrderSideBuy {
		if oldQty >= 0 {
			// Adding to long position
			totalCost := pos.AvgEntryPrice*oldQty + trade.Price*trade.Quantity
			pos.Quantity = newQty
			if pos.Quantity > 0 {
				pos.AvgEntryPrice = totalCost / pos.Quantity
			}
		} else {
			// Covering short (for futures, not applicable to spot yet)
			pos.Quantity = newQty
		}
	} else {
		// Sell
		if oldQty > 0 {
			// Reducing long position: realize PnL
			sellQty := trade.Quantity
			if sellQty > oldQty {
				sellQty = oldQty
			}
			realizedPnL := (trade.Price - pos.AvgEntryPrice) * sellQty
			pos.RealizedPnL += realizedPnL
			pos.Quantity = newQty
		}
	}

	// Dust collapse: residual qty after a sell that's smaller than 1e-5 BTC
	// (or whichever asset) is operationally untradeable — Binance min step
	// for BTCUSDT is 1e-5. Treat it as flat to prevent the strategy from
	// looping "SELL → quantity 0 → error" every K-line.
	if pos.Quantity > 0 && pos.Quantity < 1e-5 {
		m.logger.Info("position dust collapsed to flat",
			zap.String("symbol", trade.Symbol),
			zap.Float64("residual_qty", pos.Quantity),
		)
		pos.Quantity = 0
	}

	// Update side
	if pos.Quantity > 0 {
		pos.Side = "LONG"
	} else if pos.Quantity < 0 {
		pos.Side = "SHORT"
	} else {
		pos.Side = "FLAT"
		pos.AvgEntryPrice = 0
	}

	m.logger.Info("position updated",
		zap.String("symbol", trade.Symbol),
		zap.Float64("qty", pos.Quantity),
		zap.Float64("avg_price", pos.AvgEntryPrice),
		zap.Float64("realized_pnl", pos.RealizedPnL),
		zap.String("side", pos.Side),
	)
}

// ReconcileFromAccount overwrites the local position quantity for a symbol
// with the authoritative free balance from the exchange. AvgEntryPrice and
// RealizedPnL are preserved from local state because Binance doesn't expose
// the original cost basis.
//
// This is called after every order fill so the local state never drifts from
// the exchange even if there are double-fill events, manual interventions,
// or missed websocket updates.
func (m *Manager) ReconcileFromAccount(symbol, baseAsset string, freeQty, currentPrice float64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	pos, ok := m.positions[symbol]
	if !ok {
		pos = &Position{Symbol: symbol, Side: "FLAT"}
		m.positions[symbol] = pos
	}

	oldQty := pos.Quantity
	pos.Quantity = freeQty
	if currentPrice > 0 {
		pos.CurrentPrice = currentPrice
	}

	// Dust → flat. Use 1e-5 (Binance BTCUSDT min step) instead of 1e-8 because
	// any residual smaller than the min step is operationally unsellable.
	if pos.Quantity < 1e-5 || (currentPrice > 0 && pos.Quantity*currentPrice < 10) {
		pos.Quantity = 0
		pos.AvgEntryPrice = 0
		pos.UnrealizedPnL = 0
		pos.Side = "FLAT"
	} else {
		pos.Side = "LONG"
		if pos.AvgEntryPrice > 0 && currentPrice > 0 {
			pos.UnrealizedPnL = (currentPrice - pos.AvgEntryPrice) * pos.Quantity
		}
	}

	if oldQty != pos.Quantity {
		m.logger.Info("position reconciled from account",
			zap.String("symbol", symbol),
			zap.Float64("old_qty", oldQty),
			zap.Float64("new_qty", pos.Quantity),
			zap.String("side", pos.Side),
		)
	}
}

// ForceFlat forces a symbol to FLAT state. Used when a sell signal arrives
// on a dust position that can't actually be sold — we want to clear local
// state so the strategy stops looping SELL.
func (m *Manager) ForceFlat(symbol string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if pos, ok := m.positions[symbol]; ok {
		pos.Quantity = 0
		pos.AvgEntryPrice = 0
		pos.UnrealizedPnL = 0
		pos.Side = "FLAT"
	}
}

// UpdatePrice updates the current price and unrealized PnL for a symbol.
func (m *Manager) UpdatePrice(symbol string, price float64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	pos, ok := m.positions[symbol]
	if !ok || pos.Quantity == 0 {
		return
	}

	pos.CurrentPrice = price
	pos.UnrealizedPnL = (price - pos.AvgEntryPrice) * pos.Quantity
}

// TotalUnrealizedPnL returns the sum of unrealized PnL across all positions.
func (m *Manager) TotalUnrealizedPnL() float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()

	total := 0.0
	for _, pos := range m.positions {
		total += pos.UnrealizedPnL
	}
	return total
}

// TotalRealizedPnL returns the sum of realized PnL across all positions.
func (m *Manager) TotalRealizedPnL() float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()

	total := 0.0
	for _, pos := range m.positions {
		total += pos.RealizedPnL
	}
	return total
}

// SyncFromAccount initializes positions from exchange account balances.
// Must be called on startup to recover state from previous sessions.
func (m *Manager) SyncFromAccount(balances []exchange.Balance, symbols []string, getPrice func(string) float64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	symbolSet := make(map[string]bool, len(symbols))
	for _, s := range symbols {
		// Extract base asset from pair, e.g. "BTCUSDT" → "BTC"
		base := strings.TrimSuffix(s, "USDT")
		symbolSet[base] = true
	}

	for _, b := range balances {
		if !symbolSet[b.Asset] {
			continue
		}
		// 只用 Free 余额作为可交易持仓。Locked 部分通常是挂单（SL/TP）占用，
		// 把它算进持仓会导致：
		//   1) 策略每次评估都认为已有持仓 → 永远走 SELL 分支，BUY 永远不会触发
		//   2) 卖单尝试卖出 Free+Locked 数量 → Binance 返回 -2010 insufficient balance
		totalQty := b.Free
		if totalQty < 1e-8 {
			continue
		}

		// Dust 过滤：取不到价格 或 折算后 < $10 都视为零钱，不当持仓处理。
		// 之前的逻辑写成 `if price > 0 && totalQty*price < 10`，price=0 时
		// 整个表达式为 false，反而把 dust 当成正常持仓同步进来 → 策略一启动
		// 就以为有 1 BTC，导致永远走 SELL 分支。
		symbol := b.Asset + "USDT"
		price := getPrice(symbol)
		if price <= 0 || totalQty*price < 10 {
			m.logger.Info("position sync: dust balance ignored",
				zap.String("symbol", symbol),
				zap.Float64("qty", totalQty),
				zap.Float64("price", price),
				zap.Float64("value_usdt", totalQty*price),
			)
			continue
		}

		pos := &Position{
			Symbol:       symbol,
			Quantity:     totalQty,
			CurrentPrice: price,
			Side:         "LONG",
		}

		// We don't know the original entry price from Binance account API.
		// Use current price as a conservative estimate — PnL starts from 0.
		pos.AvgEntryPrice = price
		pos.UnrealizedPnL = 0

		m.positions[symbol] = pos
		m.logger.Info("position synced from account",
			zap.String("symbol", symbol),
			zap.Float64("qty", totalQty),
			zap.Float64("price", price),
		)
	}
}

// SetEntryPrice corrects the avg entry price for a position.
// Used on startup to replace the market-price estimate with the real entry
// price from the trades table.
func (m *Manager) SetEntryPrice(symbol string, price float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if pos, ok := m.positions[symbol]; ok && price > 0 {
		pos.AvgEntryPrice = price
	}
}

// Suppress unused import
var _ = time.Now
