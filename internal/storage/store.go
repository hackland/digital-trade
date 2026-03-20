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
	ID           int64     `json:"id"`
	ExchangeID   int64     `json:"exchange_id"`
	OrderID      int64     `json:"order_id"`
	Symbol       string    `json:"symbol"`
	Side         string    `json:"side"`
	Price        float64   `json:"price"`
	Quantity     float64   `json:"quantity"`
	Fee          float64   `json:"fee"`
	FeeAsset     string    `json:"fee_asset"`
	StrategyName string    `json:"strategy_name"`
	RealizedPnL  float64   `json:"realized_pnl"`
	Timestamp    time.Time `json:"timestamp"`
	CreatedAt    time.Time `json:"created_at"`
}

type OrderRecord struct {
	ID            int64     `json:"id"`
	ExchangeID    int64     `json:"exchange_id"`
	ClientOrderID string    `json:"client_order_id"`
	Symbol        string    `json:"symbol"`
	Side          string    `json:"side"`
	Type          string    `json:"type"`
	Status        string    `json:"status"`
	Price         float64   `json:"price"`
	Quantity      float64   `json:"quantity"`
	FilledQty     float64   `json:"filled_qty"`
	AvgPrice      float64   `json:"avg_price"`
	StopPrice     float64   `json:"stop_price"`
	StrategyName  string    `json:"strategy_name"`
	SignalReason  string    `json:"signal_reason"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type AccountSnapshot struct {
	Timestamp     time.Time          `json:"timestamp"`
	TotalEquity   float64            `json:"total_equity"`
	FreeCash      float64            `json:"free_cash"`
	PositionValue float64            `json:"position_value"`
	UnrealizedPnL float64            `json:"unrealized_pnl"`
	RealizedPnL   float64            `json:"realized_pnl"`
	DailyPnL      float64            `json:"daily_pnl"`
	DrawdownPct   float64            `json:"drawdown_pct"`
	Positions     map[string]float64 `json:"positions"` // symbol -> quantity
}

type SignalRecord struct {
	ID           int64              `json:"id"`
	Timestamp    time.Time          `json:"timestamp"`
	Symbol       string             `json:"symbol"`
	StrategyName string             `json:"strategy_name"`
	Action       string             `json:"action"`
	Strength     float64            `json:"strength"`
	Reason       string             `json:"reason"`
	Indicators   map[string]float64 `json:"indicators"`
	WasExecuted  bool               `json:"was_executed"`
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
