package eventbus

import (
	"time"

	"github.com/jayce/btc-trader/internal/exchange"
)

// EventType identifies the kind of event.
type EventType int

const (
	EventKlineUpdate EventType = iota
	EventDepthUpdate
	EventTradeUpdate
	EventSignal
	EventOrderUpdate
	EventPositionUpdate
	EventRiskAlert
	EventAccountUpdate
)

// Event wraps a typed payload with metadata.
type Event struct {
	Type      EventType
	Timestamp time.Time
	Payload   interface{}
}

// KlineEvent carries a kline update.
type KlineEvent struct {
	Symbol   string
	Interval string
	Kline    exchange.Kline
}

// SignalEvent carries a strategy signal.
type SignalEvent struct {
	Symbol   string
	Action   string  // BUY, SELL, HOLD
	Strength float64 // 0.0 to 1.0
	Strategy string
	Reason   string
	Metadata map[string]float64
}

// OrderUpdateEvent carries an order state change.
type OrderUpdateEvent struct {
	Order exchange.Order
}

// PositionUpdateEvent carries a position change.
type PositionUpdateEvent struct {
	Symbol        string
	Quantity      float64
	AvgEntryPrice float64
	UnrealizedPnL float64
	RealizedPnL   float64
}

// RiskAlertEvent carries a risk limit breach.
type RiskAlertEvent struct {
	Rule    string
	Message string
	Level   string // warn, critical
}
