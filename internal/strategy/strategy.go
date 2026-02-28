package strategy

import (
	"context"
	"time"

	"github.com/jayce/btc-trader/internal/exchange"
)

// Action represents the recommended trading action.
type Action int

const (
	Hold Action = iota
	Buy
	Sell
)

func (a Action) String() string {
	switch a {
	case Buy:
		return "BUY"
	case Sell:
		return "SELL"
	default:
		return "HOLD"
	}
}

// Signal is the output of a strategy evaluation.
type Signal struct {
	Action     Action
	Strength   float64            // 0.0 (weak) to 1.0 (strong)
	Symbol     string
	Strategy   string             // Name of the producing strategy
	Reason     string             // Human-readable explanation
	Indicators map[string]float64 // Snapshot of indicator values at signal time
	Timestamp  time.Time
}

// MarketSnapshot contains all data a strategy needs to make a decision.
type MarketSnapshot struct {
	Symbol     string
	Klines     []exchange.Kline
	Indicators IndicatorSet
	OrderBook  *exchange.OrderBook
	Position   *PositionInfo
	Timestamp  time.Time
}

// PositionInfo provides current position state to the strategy.
type PositionInfo struct {
	Quantity      float64
	AvgEntryPrice float64
	UnrealizedPnL float64
	Side          string // "LONG", "SHORT", "FLAT"
}

// IndicatorSet holds computed indicator values.
type IndicatorSet struct {
	SMA  map[int]float64 // period -> value
	EMA  map[int]float64 // period -> value
	MACD MACDValue
	RSI  map[int]float64 // period -> value
	BB   BollingerBands
	ATR  map[int]float64 // period -> value
	OBV  float64
	MFI  map[int]float64 // period -> value
	VWAP float64
}

// MACDValue holds MACD indicator components.
type MACDValue struct {
	MACD      float64
	Signal    float64
	Histogram float64
}

// BollingerBands holds Bollinger Bands indicator components.
type BollingerBands struct {
	Upper  float64
	Middle float64
	Lower  float64
	Width  float64
}

// IndicatorRequirement specifies an indicator that a strategy needs.
type IndicatorRequirement struct {
	Name   string         // "SMA", "EMA", "MACD", "RSI", "BB", "ATR", "OBV", "MFI", "VWAP"
	Params map[string]int // e.g., {"period": 14} or {"fast": 12, "slow": 26, "signal": 9}
}

// Strategy is the core interface that all trading strategies must implement.
// The same interface is used for both live trading and backtesting.
type Strategy interface {
	// Name returns the unique identifier for this strategy.
	Name() string

	// Init initializes the strategy with its configuration.
	Init(cfg map[string]interface{}) error

	// RequiredIndicators returns the indicators this strategy needs.
	RequiredIndicators() []IndicatorRequirement

	// RequiredHistory returns the minimum number of klines needed.
	RequiredHistory() int

	// Evaluate examines the current market snapshot and produces a signal.
	Evaluate(ctx context.Context, snapshot *MarketSnapshot) (*Signal, error)

	// OnTradeExecuted is called after a trade is executed.
	OnTradeExecuted(trade *exchange.Trade)
}
