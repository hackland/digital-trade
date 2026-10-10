package mtfmacd

import (
	"context"
	"math"
	"sort"
	"time"

	"github.com/jayce/btc-trader/internal/exchange"
)

// ExitRule closes a trade when the 1h MACD histogram (in the trade direction)
// has peaked and then faded to <= PeakRatio of that peak (0 = back to the zero
// axis). StopLossPct > 0 adds a hard stop measured on 15m highs/lows.
type ExitRule struct {
	Name        string  `json:"name"`
	PeakRatio   float64 `json:"peak_ratio"`
	StopLossPct float64 `json:"stop_loss_pct"`
}

// ExitTrade is one simulated round trip.
type ExitTrade struct {
	Time      time.Time `json:"time"`
	Direction string    `json:"direction"`
	Entry     float64   `json:"entry"`
	Exit      float64   `json:"exit"`
	RetPct    float64   `json:"ret_pct"`
	HoldBars  int       `json:"hold_bars"` // 15m bars
	Reason    string    `json:"reason"`    // 1h_fade | stop | open
}

// ExitStat summarizes one rule over a group of trades.
type ExitStat struct {
	Rule       string  `json:"rule"`
	Group      string  `json:"group"`
	Trades     int     `json:"trades"`
	WinRate    float64 `json:"win_rate"`
	AvgRet     float64 `json:"avg_ret_pct"`
	AvgWin     float64 `json:"avg_win_pct"`
	AvgLoss    float64 `json:"avg_loss_pct"`
	SumRet     float64 `json:"sum_ret_pct"`
	ProfitFact float64 `json:"profit_factor"`
	AvgHoldH   float64 `json:"avg_hold_hours"`
	StopRate   float64 `json:"stop_rate"`
	MaxDD      float64 `json:"worst_trade_pct"`
}

// simulate runs one trade from signal bar i with the given rule.
func simulate(s1, s15 *Series, i int, long bool, r ExitRule) ExitTrade {
	entry := s15.Klines[i].Close
	sgn := 1.0
	if !long {
		sgn = -1
	}
	tr := ExitTrade{Time: s15.Klines[i].CloseTime, Entry: entry, Direction: DirLong}
	if !long {
		tr.Direction = DirShort
	}
	h1 := s1.IndexAtOrBefore(s15.Klines[i].CloseTime, 0)
	peak := math.Max(sgn*s1.macd.Histogram[h1], 0)
	lastH1 := h1

	finish := func(j int, px float64, reason string) ExitTrade {
		tr.Exit, tr.HoldBars, tr.Reason = px, j-i, reason
		tr.RetPct = sgn * (px/entry - 1) * 100
		return tr
	}
	for j := i + 1; j < len(s15.Klines); j++ {
		k := s15.Klines[j]
		if r.StopLossPct > 0 {
			stop := entry * (1 - sgn*r.StopLossPct/100)
			if (long && k.Low <= stop) || (!long && k.High >= stop) {
				return finish(j, stop, "stop")
			}
		}
		cur := s1.IndexAtOrBefore(k.CloseTime, lastH1)
		for ; lastH1 < cur; lastH1++ { // process each newly closed 1h bar
			h := sgn * s1.macd.Histogram[lastH1+1]
			if h > peak {
				peak = h
			} else if peak > 0 && h <= r.PeakRatio*peak {
				return finish(j, k.Close, "1h_fade")
			} else if peak == 0 && h <= 0 {
				return finish(j, k.Close, "1h_fade")
			}
		}
	}
	return finish(len(s15.Klines)-1, s15.Klines[len(s15.Klines)-1].Close, "open")
}

// ExitStudy simulates every resonance signal (no 15m zero filter) under each exit
// rule and returns stats per (rule, group). feePct is charged per side.
func ExitStudy(ctx context.Context, ex exchange.Exchange, symbol string, days int, p Params, rules []ExitRule, feePct float64) ([]ExitStat, int, error) {
	ss, start, err := loadSeries(ctx, ex, symbol, days, p)
	if err != nil {
		return nil, 0, err
	}
	s4, s1, s15 := ss[0], ss[1], ss[2]
	p.Require15mBeyondZero = false

	type sig struct {
		i    int
		long bool
	}
	var sigs []sig
	h4, h1 := 0, 0
	for i := 1; i < len(s15.Klines); i++ {
		if s15.Klines[i].CloseTime.Before(start) {
			continue
		}
		var ev Eval
		ev, h4, h1 = Evaluate(symbol, s4, s1, s15, i, h4, h1, p)
		h4, h1 = max(h4, 0), max(h1, 0)
		if ev.Triggered {
			sigs = append(sigs, sig{i, ev.Direction == DirLong})
		}
	}

	var out []ExitStat
	for _, r := range rules {
		groups := map[string][]ExitTrade{}
		for _, s := range sigs {
			t := simulate(s1, s15, s.i, s.long, r)
			t.RetPct -= 2 * feePct
			groups["全部"] = append(groups["全部"], t)
			g := "做空"
			if s.long {
				g = "做多"
			}
			groups[g] = append(groups[g], t)
		}
		for _, g := range []string{"全部", "做多", "做空"} {
			out = append(out, summarize(r.Name, g, groups[g]))
		}
	}
	sort.SliceStable(out, func(a, b int) bool { return false })
	return out, len(sigs), nil
}

func summarize(rule, group string, ts []ExitTrade) ExitStat {
	st := ExitStat{Rule: rule, Group: group, Trades: len(ts)}
	if len(ts) == 0 {
		return st
	}
	var wins, losses int
	var gw, gl, hold float64
	var stops int
	for _, t := range ts {
		st.SumRet += t.RetPct
		hold += float64(t.HoldBars) * 0.25
		if t.Reason == "stop" {
			stops++
		}
		if t.RetPct > 0 {
			wins++
			gw += t.RetPct
		} else {
			losses++
			gl += t.RetPct
		}
		st.MaxDD = math.Min(st.MaxDD, t.RetPct)
	}
	n := float64(len(ts))
	st.WinRate = float64(wins) / n * 100
	st.AvgRet = st.SumRet / n
	st.AvgHoldH = hold / n
	st.StopRate = float64(stops) / n * 100
	if wins > 0 {
		st.AvgWin = gw / float64(wins)
	}
	if losses > 0 {
		st.AvgLoss = gl / float64(losses)
		st.ProfitFact = gw / -gl
	}
	return st
}
