package storage

import (
	"context"
	"time"

	"github.com/jayce/btc-trader/internal/exchange"
	"github.com/jayce/btc-trader/internal/strategy"
)

// KlineRepository manages kline data persistence.
type KlineRepository interface {
	SaveKlines(ctx context.Context, klines []exchange.Kline) error
	GetKlines(ctx context.Context, symbol, interval string, start, end time.Time, limit int) ([]exchange.Kline, error)
	GetLatestKline(ctx context.Context, symbol, interval string) (*exchange.Kline, error)
}

// TradeRepository manages trade history persistence.
type TradeRepository interface {
	SaveTrade(ctx context.Context, trade *TradeRecord) error
	GetTrades(ctx context.Context, filter TradeFilter) ([]TradeRecord, error)
	GetTradesByDateRange(ctx context.Context, symbol string, start, end time.Time) ([]TradeRecord, error)
}

// OrderRepository manages order history.
type OrderRepository interface {
	SaveOrder(ctx context.Context, order *OrderRecord) error
	UpdateOrder(ctx context.Context, order *OrderRecord) error
	GetOrder(ctx context.Context, orderID int64) (*OrderRecord, error)
	GetOpenOrders(ctx context.Context, symbol string) ([]OrderRecord, error)
	GetOrders(ctx context.Context, filter OrderFilter) ([]OrderRecord, error)
}

// SnapshotRepository manages periodic account snapshots.
type SnapshotRepository interface {
	SaveSnapshot(ctx context.Context, snapshot *AccountSnapshot) error
	GetSnapshots(ctx context.Context, start, end time.Time, interval string) ([]AccountSnapshot, error)
	GetLatestSnapshot(ctx context.Context) (*AccountSnapshot, error)
}

// SignalRepository saves strategy signals for audit.
type SignalRepository interface {
	SaveSignal(ctx context.Context, signal *strategy.Signal, wasExecuted bool) error
	GetSignals(ctx context.Context, filter SignalFilter) ([]SignalRecord, error)
}

// Store combines all repositories.
type Store interface {
	KlineRepository
	TradeRepository
	OrderRepository
	SnapshotRepository
	SignalRepository
	Close() error
	Migrate(ctx context.Context) error
}

// --- Record types ---

type TradeRecord struct {
	ID           int64
	ExchangeID   int64
	OrderID      int64
	Symbol       string
	Side         string
	Price        float64
	Quantity     float64
	Fee          float64
	FeeAsset     string
	StrategyName string
	RealizedPnL  float64
	Timestamp    time.Time
	CreatedAt    time.Time
}

type OrderRecord struct {
	ID            int64
	ExchangeID    int64
	ClientOrderID string
	Symbol        string
	Side          string
	Type          string
	Status        string
	Price         float64
	Quantity      float64
	FilledQty     float64
	AvgPrice      float64
	StopPrice     float64
	StrategyName  string
	SignalReason  string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type AccountSnapshot struct {
	Timestamp     time.Time
	TotalEquity   float64
	FreeCash      float64
	PositionValue float64
	UnrealizedPnL float64
	RealizedPnL   float64
	DailyPnL      float64
	DrawdownPct   float64
	Positions     map[string]float64 // symbol -> quantity
}

type SignalRecord struct {
	ID           int64
	Timestamp    time.Time
	Symbol       string
	StrategyName string
	Action       string
	Strength     float64
	Reason       string
	Indicators   map[string]float64
	WasExecuted  bool
}

// --- Filters ---

type TradeFilter struct {
	Symbol       string
	StrategyName string
	StartTime    *time.Time
	EndTime      *time.Time
	Limit        int
	Offset       int
}

type OrderFilter struct {
	Symbol    string
	Status    string
	StartTime *time.Time
	EndTime   *time.Time
	Limit     int
	Offset    int
}

type SignalFilter struct {
	Symbol       string
	StrategyName string
	Action       string
	StartTime    *time.Time
	EndTime      *time.Time
	Limit        int
	Offset       int
}
