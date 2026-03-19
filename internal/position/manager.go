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
		totalQty := b.Free + b.Locked
		if totalQty < 1e-8 {
			continue
		}

		symbol := b.Asset + "USDT"
		price := getPrice(symbol)

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

// Suppress unused import
var _ = time.Now
