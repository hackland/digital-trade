// Package mtfmacd implements the 4h/1h/15m MACD resonance signal: when the 4h
// and 1h MACD agree (both above/below the zero axis and on the same side of
// their signal line), a fresh 15m cross in the same direction raises a
// signal. Alert-only: it never places orders.
package mtfmacd

import (
	"time"

	"github.com/jayce/btc-trader/internal/exchange"
	"github.com/jayce/btc-trader/internal/market"
)

const (
	DirLong  = "long"
	DirShort = "short"

	TrendUp   = "up"
	TrendDown = "down"
	TrendNone = "none"
)

// Params are the MACD parameters and signal options.
type Params struct {
	Fast, Slow, Signal int
	// Require15mBeyondZero: long needs the 15m DIF below the zero axis at the
	// golden cross (pullback ended), short needs it above (bounce ended).
	Require15mBeyondZero bool
	// Reversal selects how a 15m reversal is detected:
	//   "" / "cross"    histogram sign flip (golden/dead cross)
	//   "turn"          DIF line turns (local trough/peak), earlier than the cross
	//   "turn_pre"      DIF turns while the histogram has not crossed yet (still <=0 for long, >=0 for short)
	Reversal string
	// Trend-strength filters on the higher timeframes (0/false = off).
	LongOnly    bool    // ignore short-side setups entirely
	DivMode     string  // regular (default) | near | hidden | dif_up; used with Divergence
	Divergence  bool    // require 15m bullish (long) / bearish (short) MACD divergence on the pullback leg
	Expanding1h bool    // 1h histogram must be growing in the trade direction
	Expanding4h bool    // 4h histogram must be growing in the trade direction
	Min1hDifPct float64 // |1h DIF| / price must be >= this percent (DIF far enough from the zero axis)
}

// DefaultParams returns MACD(12,26,9) with no extra filter.
func DefaultParams() Params { return Params{Fast: 12, Slow: 26, Signal: 9} }

// TFState is the MACD state of one timeframe at one closed candle.
type TFState struct {
	Interval  string    `json:"interval"`
	CandleEnd time.Time `json:"candle_end"`
	Close     float64   `json:"close"`
	MACD      float64   `json:"macd"`
	Signal    float64   `json:"signal"`
	Histogram float64   `json:"histogram"`
	Trend     string    `json:"trend"` // up | down | none
	Valid     bool      `json:"valid"`
}

// Series is a kline slice with its precomputed MACD.
type Series struct {
	Interval string
	Klines   []exchange.Kline
	macd     market.MACDSeries
}

// BuildSeries computes MACD over klines (which must be closed candles, ascending).
func BuildSeries(interval string, klines []exchange.Kline, p Params) *Series {
	closes := make([]float64, len(klines))
	for i, k := range klines {
		closes[i] = k.Close
	}
	return &Series{
		Interval: interval,
		Klines:   klines,
		macd:     market.NewIndicatorComputer().ComputeMACDSeries(closes, p.Fast, p.Slow, p.Signal),
	}
}

func (s *Series) valid(i int) bool {
	return s.macd.MACD != nil && i >= s.macd.First && i < len(s.Klines)
}

// State returns the MACD state at index i.
func (s *Series) State(i int) TFState {
	st := TFState{Interval: s.Interval, Trend: TrendNone}
	if i < 0 || i >= len(s.Klines) {
		return st
	}
	st.CandleEnd = s.Klines[i].CloseTime
	st.Close = s.Klines[i].Close
	if !s.valid(i) {
		return st
	}
	st.Valid = true
	st.MACD, st.Signal, st.Histogram = s.macd.MACD[i], s.macd.Signal[i], s.macd.Histogram[i]
	switch {
	case st.MACD > 0 && st.MACD > st.Signal:
		st.Trend = TrendUp
	case st.MACD < 0 && st.MACD < st.Signal:
		st.Trend = TrendDown
	}
	return st
}

// Cross reports a histogram sign flip at index i: "up" (golden), "down" (dead) or "".
func (s *Series) Cross(i int) string { return s.Reversal(i, "cross") }

// Reversal detects a 15m reversal at index i using the given mode (see Params.Reversal).
func (s *Series) Reversal(i int, mode string) string {
	if mode == "turn" || mode == "turn_pre" {
		if !s.valid(i) || !s.valid(i-2) {
			return ""
		}
		m := s.macd.MACD
		h := s.macd.Histogram[i]
		switch {
		case m[i-1] <= m[i-2] && m[i] > m[i-1] && (mode == "turn" || h <= 0):
			return TrendUp
		case m[i-1] >= m[i-2] && m[i] < m[i-1] && (mode == "turn" || h >= 0):
			return TrendDown
		}
		return ""
	}
	if !s.valid(i) || !s.valid(i-1) {
		return ""
	}
	prev, cur := s.macd.Histogram[i-1], s.macd.Histogram[i]
	switch {
	case prev <= 0 && cur > 0:
		return TrendUp
	case prev >= 0 && cur < 0:
		return TrendDown
	}
	return ""
}

// IndexAtOrBefore returns the last candle index whose CloseTime <= t, or -1.
// from is a lower bound hint (monotonic scans); pass 0 otherwise.
func (s *Series) IndexAtOrBefore(t time.Time, from int) int {
	i := from
	if i < 0 {
		i = 0
	}
	for i < len(s.Klines) && !s.Klines[i].CloseTime.After(t) {
		i++
	}
	return i - 1
}

// Eval is the resonance evaluation at one 15m candle.
type Eval struct {
	Symbol    string    `json:"symbol"`
	H4        TFState   `json:"h4"`
	H1        TFState   `json:"h1"`
	M15       TFState   `json:"m15"`
	HTFTrend  string    `json:"htf_trend"` // 二周期共振方向: up | down | none
	Cross15m  string    `json:"cross_15m"` // up | down | ""
	ZeroOK    bool      `json:"zero_ok"`   // 15m 0轴条件是否满足(未启用选项时恒为true)
	Triggered bool      `json:"triggered"`
	Direction string    `json:"direction"` // long | short when triggered
	Price     float64   `json:"price"`
	Time      time.Time `json:"time"` // 触发的15m K线收盘时间
	Note      string    `json:"note"`
}

// Evaluate computes the resonance at 15m index i2 using the latest 1h/4h candles
// closed no later than that 15m candle's close. h4/h1 indices are found by time.
func Evaluate(symbol string, s4h, s1h, s15 *Series, i15, hint4, hint1 int, p Params) (Eval, int, int) {
	e := Eval{Symbol: symbol, HTFTrend: TrendNone}
	e.M15 = s15.State(i15)
	e.Price = e.M15.Close
	e.Time = e.M15.CandleEnd
	i4 := s4h.IndexAtOrBefore(e.Time, hint4)
	i1 := s1h.IndexAtOrBefore(e.Time, hint1)
	e.H4, e.H1 = s4h.State(i4), s1h.State(i1)

	if e.H4.Trend != TrendNone && e.H4.Trend == e.H1.Trend {
		e.HTFTrend = e.H4.Trend
		sgn := 1.0
		if e.HTFTrend == TrendDown {
			sgn = -1
		}
		if p.Expanding4h && !(i4 >= 1 && s4h.valid(i4-1) && sgn*s4h.macd.Histogram[i4] > sgn*s4h.macd.Histogram[i4-1]) {
			e.HTFTrend = TrendNone
		}
		if p.Expanding1h && !(i1 >= 1 && s1h.valid(i1-1) && sgn*s1h.macd.Histogram[i1] > sgn*s1h.macd.Histogram[i1-1]) {
			e.HTFTrend = TrendNone
		}
		if p.Min1hDifPct > 0 && e.H1.Close > 0 && abs(e.H1.MACD)/e.H1.Close*100 < p.Min1hDifPct {
			e.HTFTrend = TrendNone
		}
	}
	e.Cross15m = s15.Reversal(i15, p.Reversal)
	e.ZeroOK = true
	if e.Cross15m != "" && p.Require15mBeyondZero {
		if e.Cross15m == TrendUp {
			e.ZeroOK = e.M15.MACD < 0
		} else {
			e.ZeroOK = e.M15.MACD > 0
		}
	}

	switch {
	case !e.H4.Valid || !e.H1.Valid || !e.M15.Valid:
		e.Note = "K线数据不足"
	case e.HTFTrend == TrendNone:
		e.Note = "4h 与 1h 未形成同向共振(需同在0轴一侧且DIF在信号线同侧)"
	case p.LongOnly && e.HTFTrend == TrendDown:
		e.Note = "仅做多模式：空头共振忽略"
	case e.Cross15m == "":
		e.Note = "二周期共振中，等待 15m 反转"
	case e.Cross15m != e.HTFTrend:
		e.Note = "15m 反转方向与大周期相反，忽略"
	case !e.ZeroOK:
		e.Note = "15m 已反转，但 DIF 不满足0轴位置条件，忽略"
	case p.Divergence && !s15.Divergence(i15, e.Cross15m == TrendUp, p.DivMode):
		e.Note = "15m 已反转，但没有 MACD 背离，忽略"
	default:
		e.Triggered = true
		if e.HTFTrend == TrendUp {
			e.Direction = DirLong
			e.Note = "4h/1h 多头共振 + 15m 金叉"
		} else {
			e.Direction = DirShort
			e.Note = "4h/1h 空头共振 + 15m 死叉"
		}
	}
	return e, i4, i1
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}
