package grid

import "time"

// TradeSide identifies what a grid fill did.
type TradeSide string

const (
	SideBuy        TradeSide = "BUY"
	SideSell       TradeSide = "SELL"
	SideForceClose TradeSide = "FORCE_CLOSE" // stop-loss liquidation at run end
)

// Trade is one grid-level fill.
type Trade struct {
	Timestamp time.Time `json:"timestamp"`
	Side      TradeSide `json:"side"`
	Price     float64   `json:"price"`
	Quantity  float64   `json:"quantity"`
	Fee       float64   `json:"fee"`
	PnL       float64   `json:"pnl"` // realized PnL for SELL/FORCE_CLOSE; 0 for BUY
	SlotLow   float64   `json:"slot_low"`
	SlotHigh  float64   `json:"slot_high"`
}

// EquityPoint is one point on the mark-to-market equity curve.
type EquityPoint struct {
	Time   time.Time `json:"time"`
	Equity float64   `json:"equity"`
}

// Metrics summarizes a grid backtest run.
type Metrics struct {
	CompletedRoundTrips   int     `json:"completed_round_trips"` // count of SELL fills (excludes FORCE_CLOSE)
	RealizedPnL           float64 `json:"realized_pnl"`
	TotalFees             float64 `json:"total_fees"`
	RemainingQty          float64 `json:"remaining_qty"`   // coin still held across all open slots at run end
	RemainingValue        float64 `json:"remaining_value"` // remaining_qty * final close price
	FinalCash             float64 `json:"final_cash"`
	FinalEquity           float64 `json:"final_equity"`
	TotalReturnPct        float64 `json:"total_return_pct"`
	MaxDrawdownPct        float64 `json:"max_drawdown_pct"`
	MaxConcurrentHoldings int     `json:"max_concurrent_holdings"` // most grid slots simultaneously holding inventory
}

// Result is the full output of a grid backtest run.
type Result struct {
	Symbol         string    `json:"symbol"`
	Interval       string    `json:"interval"`
	StartTime      time.Time `json:"start_time"`
	EndTime        time.Time `json:"end_time"`
	InitialCash    float64   `json:"initial_cash"`
	UpperPrice     float64   `json:"upper_price"`
	LowerPrice     float64   `json:"lower_price"`
	GridSpacingPct float64   `json:"grid_spacing_pct"`
	GridLevels     []float64 `json:"grid_levels"`

	Trades      []Trade       `json:"trades"`
	EquityCurve []EquityPoint `json:"equity_curve"`
	Metrics     Metrics       `json:"metrics"`

	StoppedOut bool   `json:"stopped_out"`
	StopReason string `json:"stop_reason,omitempty"`
}
