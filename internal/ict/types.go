// Package ict implements a rule-based ICT (Inner Circle Trader) signal engine:
// liquidity sweep -> displacement + market structure shift -> retrace into an
// FVG / order block. It is deterministic (no AI), alert-only and backtestable.
package ict

import "time"

// Setup statuses.
const (
	StatusPending = "pending" // armed, waiting for price to retrace into the entry
	StatusOpen    = "open"    // filled, neither stop nor target hit yet
	StatusTP      = "tp"
	StatusSL      = "sl"
	StatusTimeout = "timeout" // closed at market after MaxHoldBars
	StatusExpired = "expired" // never filled within ExpiryBars
	StatusMissed  = "missed"  // target traded before the entry was tagged
	StatusInvalid = "invalid" // gapped through the stop on fill
)

// Params tunes the detector. Zero values are replaced by normalize().
type Params struct {
	SwingN          int     `json:"swing_n"`          // fractal width: bars on each side of a swing point
	LiqLookback     int     `json:"liq_lookback"`     // 1h bars a swing level stays sweepable
	EqualTolPct     float64 `json:"equal_tol_pct"`    // equal-highs/lows tolerance, in %
	DispATR         float64 `json:"disp_atr"`         // displacement: candle body >= x * ATR(14)
	MaxSweepToMSS   int     `json:"max_sweep_to_mss"` // 1h bars allowed between the sweep and the shift
	MinFVGATR       float64 `json:"min_fvg_atr"`      // minimum FVG height, in ATRs
	EntryDepth      float64 `json:"entry_depth"`      // 0 = near edge of the zone, 1 = far edge
	StopBufATR      float64 `json:"stop_buf_atr"`     // stop distance beyond the sweep extreme, in ATRs
	MinRR           float64 `json:"min_rr"`           // minimum reward:risk for the target
	MaxRiskPct      float64 `json:"max_risk_pct"`     // skip setups whose stop is wider than this %
	MinRiskPct      float64 `json:"min_risk_pct"`     // skip setups whose stop is tighter than this %
	RequireBias     bool    `json:"require_bias"`     // trade only with the 4h structure trend
	RequireDiscount bool    `json:"require_discount"` // longs only in the discount half of the leg (shorts: premium)
	Breakout        bool    `json:"breakout"`         // also trade displacement breakouts that were not preceded by a sweep
	Direction       string  `json:"direction"`        // both | long | short
	ExpiryBars      int     `json:"expiry_bars"`      // 15m bars to wait for the entry fill
	MaxHoldBars     int     `json:"max_hold_bars"`    // 15m bars before a filled trade is closed at market
	FeeRate         float64 `json:"fee_rate"`         // per side, e.g. 0.001 = 0.1%
}

// DefaultParams returns the baseline configuration.
func DefaultParams() Params {
	return Params{
		SwingN:          3,
		LiqLookback:     72,
		EqualTolPct:     0.10,
		DispATR:         1.0,
		MaxSweepToMSS:   24,
		MinFVGATR:       0.10,
		EntryDepth:      0.5,
		StopBufATR:      0.25,
		MinRR:           2,
		MaxRiskPct:      3,
		MinRiskPct:      0.15,
		RequireBias:     true,
		RequireDiscount: false,
		Direction:       "both",
		ExpiryBars:      96,
		MaxHoldBars:     288,
		FeeRate:         0.001,
	}
}

func clampI(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func clampF(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func (p Params) normalize() Params {
	p.SwingN = clampI(p.SwingN, 1, 10)
	p.LiqLookback = clampI(p.LiqLookback, 12, 500)
	p.EqualTolPct = clampF(p.EqualTolPct, 0, 1)
	p.DispATR = clampF(p.DispATR, 0.3, 5)
	p.MaxSweepToMSS = clampI(p.MaxSweepToMSS, 1, 48)
	p.MinFVGATR = clampF(p.MinFVGATR, 0, 2)
	p.EntryDepth = clampF(p.EntryDepth, 0, 1)
	p.StopBufATR = clampF(p.StopBufATR, 0, 2)
	p.MinRR = clampF(p.MinRR, 0.5, 10)
	p.MaxRiskPct = clampF(p.MaxRiskPct, 0.2, 20)
	p.MinRiskPct = clampF(p.MinRiskPct, 0, p.MaxRiskPct)
	p.ExpiryBars = clampI(p.ExpiryBars, 4, 1000)
	p.MaxHoldBars = clampI(p.MaxHoldBars, 4, 3000)
	p.FeeRate = clampF(p.FeeRate, 0, 0.01)
	switch p.Direction {
	case "long", "short", "both":
	default:
		p.Direction = "both"
	}
	return p
}

// Setup is one ICT trade idea with its full lifecycle.
type Setup struct {
	ID    string `json:"id"`
	Model string `json:"model"` // reversal (sweep + MSS) | breakout (displacement through a swing, no sweep)
	Dir   string `json:"dir"`   // long | short
	Bias  string `json:"bias"`  // 4h structure trend at arming: up | down | none

	SweepTime    time.Time `json:"sweep_time"`
	SweepKind    string    `json:"sweep_kind"` // SSL/EQL/PDL (longs) or BSL/EQH/PDH (shorts)
	SweepLevel   float64   `json:"sweep_level"`
	SweepExtreme float64   `json:"sweep_extreme"`

	MSSTime  time.Time `json:"mss_time"`
	MSSLevel float64   `json:"mss_level"`

	ZoneKind   string    `json:"zone_kind"` // FVG | OB
	ZoneTime   time.Time `json:"zone_time"`
	ZoneTop    float64   `json:"zone_top"`
	ZoneBottom float64   `json:"zone_bottom"`

	ArmTime   time.Time `json:"arm_time"` // close of the bar on which the setup became actionable
	Entry     float64   `json:"entry"`
	Stop      float64   `json:"stop"`
	TP        float64   `json:"tp"`
	TPKind    string    `json:"tp_kind"` // BSL/SSL/PDH/PDL = liquidity target, rr = fixed multiple
	PlannedRR float64   `json:"planned_rr"`

	Status    string     `json:"status"`
	FillTime  *time.Time `json:"fill_time,omitempty"`
	FillPrice float64    `json:"fill_price,omitempty"`
	ExitTime  *time.Time `json:"exit_time,omitempty"`
	ExitPrice float64    `json:"exit_price,omitempty"`
	R         float64    `json:"r"`        // net R (realised; mark-to-market while open)
	EndTime   time.Time  `json:"end_time"` // where overlays stop on the chart
}

// Candle is a compact OHLC bar (T = unix seconds of the open time).
type Candle struct {
	T int64   `json:"t"`
	O float64 `json:"o"`
	H float64 `json:"h"`
	L float64 `json:"l"`
	C float64 `json:"c"`
}

// Stats summarises resolved trades.
type Stats struct {
	Armed        int     `json:"armed"`
	Filled       int     `json:"filled"`
	Resolved     int     `json:"resolved"` // tp + sl + timeout
	Open         int     `json:"open"`
	Pending      int     `json:"pending"`
	TP           int     `json:"tp"`
	SL           int     `json:"sl"`
	Timeout      int     `json:"timeout"`
	Expired      int     `json:"expired"`
	Missed       int     `json:"missed"`
	Invalid      int     `json:"invalid"`
	Wins         int     `json:"wins"`
	WinRate      float64 `json:"win_rate"`
	FillRate     float64 `json:"fill_rate"`
	AvgR         float64 `json:"avg_r"`
	TotalR       float64 `json:"total_r"`
	AvgWinR      float64 `json:"avg_win_r"`
	AvgLossR     float64 `json:"avg_loss_r"`
	ProfitFactor float64 `json:"profit_factor"` // 0 when there are no losing trades
	MaxDrawdownR float64 `json:"max_drawdown_r"`
	AvgPlannedRR float64 `json:"avg_planned_rr"`
}

// Bucket groups resolved trades by one attribute.
type Bucket struct {
	Group   string  `json:"group"`
	Name    string  `json:"name"`
	Count   int     `json:"count"`
	WinRate float64 `json:"win_rate"`
	AvgR    float64 `json:"avg_r"`
	TotalR  float64 `json:"total_r"`
}

// EquityPoint is the cumulative R after a trade exits.
type EquityPoint struct {
	T int64   `json:"t"`
	R float64 `json:"r"`
}

// Funnel counts how many ideas survive each stage (both directions combined).
type Funnel struct {
	Sweeps    int `json:"sweeps"`    // liquidity sweeps (wick through a level, close back)
	Shifts    int `json:"shifts"`    // ...followed by a displacement that breaks structure
	Breakouts int `json:"breakouts"` // displacement breakouts found without a sweep (Breakout mode)
	Armed     int `json:"armed"`     // ...that also passed bias/zone/discount/risk filters
}

// Result is the output of one analysis run.
type Result struct {
	Symbol      string         `json:"symbol"`
	Days        int            `json:"days"`
	Params      Params         `json:"params"`
	GeneratedAt time.Time      `json:"generated_at"`
	LastPrice   float64        `json:"last_price"`
	BiasNow     string         `json:"bias_now"` // 4h structure trend: up | down | none
	Candles     []Candle       `json:"candles"`  // 1h bars over the window, for charting
	Setups      []Setup        `json:"setups"`
	Stats       Stats          `json:"stats"`
	Buckets     []Bucket       `json:"buckets"`
	Equity      []EquityPoint  `json:"equity"`
	Rejected    map[string]int `json:"rejected"`
	Funnel      Funnel         `json:"funnel"`
	Bars1h      int            `json:"bars_1h"`
	Bars15m     int            `json:"bars_15m"`
}
