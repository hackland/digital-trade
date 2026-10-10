package mtfmacd

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/jayce/btc-trader/internal/exchange"
)

// Structural stop modes: where the stop sits relative to the 15m swing.
const (
	StopPivot = "pivot" // below the most recent confirmed 15m swing low (above swing high for shorts)
	StopLeg   = "leg"   // below the lowest low of the pullback leg that just reversed
)

// StopRule combines a structural stop with an R-multiple take profit.
type StopRule struct {
	Name     string  `json:"name"`
	Stop     string  `json:"stop"`
	TPMult   float64 `json:"tp_mult"`  // take profit at TPMult * risk; 0 = no TP (time exit only)
	MaxBars  int     `json:"max_bars"` // time exit in 15m bars
	Buffer   float64 `json:"buffer_pct"`
	PivotLen int     `json:"pivot_len"`
	// MACDConfirm: exit only when price breaks the swing AND the 15m DIF makes a new extreme
	// beyond the DIF extreme of the pullback leg (checked on the bar close, exit at close).
	MACDConfirm bool    `json:"macd_confirm"`
	HardStopPct float64 `json:"hard_stop_pct"` // optional wide price stop from entry (0 = off)
	ATRMult     float64 `json:"atr_mult"`      // extra distance = ATRMult * ATR(14) of 15m, on top of Buffer
}

// StopStat summarizes one rule over one group of trades.
type StopStat struct {
	Rule     string  `json:"rule"`
	Group    string  `json:"group"`
	Trades   int     `json:"trades"`
	Skipped  int     `json:"skipped"`
	WinRate  float64 `json:"win_rate"`
	AvgRet   float64 `json:"avg_ret_pct"`
	AvgR     float64 `json:"avg_r"`
	PF       float64 `json:"profit_factor"`
	SumRet   float64 `json:"sum_ret_pct"`
	AvgRisk  float64 `json:"avg_risk_pct"`
	StopRate float64 `json:"stop_rate"`
	TPRate   float64 `json:"tp_rate"`
	AvgHoldH float64 `json:"avg_hold_hours"`
}

// atr14 is the simple mean true range of the 14 bars ending at i.
func atr14(s *Series, i int) float64 {
	if i < 15 {
		return 0
	}
	sum := 0.0
	for j := i - 13; j <= i; j++ {
		k, pc := s.Klines[j], s.Klines[j-1].Close
		sum += math.Max(k.High-k.Low, math.Max(math.Abs(k.High-pc), math.Abs(k.Low-pc)))
	}
	return sum / 14
}

func lowOf(s *Series, j int, long bool) float64 {
	if long {
		return s.Klines[j].Low
	}
	return s.Klines[j].High
}

// better reports whether a is a more extreme swing point than b (lower for longs, higher for shorts).
func better(a, b float64, long bool) bool {
	if long {
		return a < b
	}
	return a > b
}

// legExtreme is the extreme low/high of the counter-trend leg that just reversed at i.
func legExtreme(s *Series, i int, long bool) float64 {
	ext := lowOf(s, i, long)
	j := i
	// skip bars already on the trade side (e.g. the golden-cross bar itself)
	for j > 0 && i-j < 96 {
		h := s.macd.Histogram[j]
		if (long && h > 0) || (!long && h < 0) {
			j--
			continue
		}
		break
	}
	for ; j > 0 && i-j < 96; j-- {
		h := s.macd.Histogram[j]
		if (long && h > 0) || (!long && h < 0) {
			break
		}
		if v := lowOf(s, j, long); better(v, ext, long) {
			ext = v
		}
	}
	return ext
}

// pivotExtreme is the most recent confirmed swing low/high (n bars each side) before i.
func pivotExtreme(s *Series, i int, long bool, n int) (float64, int, bool) {
	for j := i - n; j > n && i-j < 160; j-- {
		v := lowOf(s, j, long)
		ok := true
		for k := j - n; k <= j+n; k++ {
			if k == j {
				continue
			}
			if !better(v, lowOf(s, k, long), long) {
				ok = false
				break
			}
		}
		if ok {
			return v, j, true
		}
	}
	return 0, 0, false
}

// difExtreme is the min (long) / max (short) DIF over bars [from, to].
func difExtreme(s *Series, from, to int, long bool) float64 {
	from = max(from, s.macd.First)
	ext := s.macd.MACD[to]
	for j := from; j <= to; j++ {
		if v := s.macd.MACD[j]; better(v, ext, long) {
			ext = v
		}
	}
	return ext
}

// legStart returns the first bar index of the counter-trend leg that just reversed at i.
func legStart(s *Series, i int, long bool) int {
	j := i
	for j > 0 && i-j < 96 {
		h := s.macd.Histogram[j]
		if (long && h > 0) || (!long && h < 0) {
			j--
			continue
		}
		break
	}
	for ; j > 0 && i-j < 96; j-- {
		h := s.macd.Histogram[j]
		if (long && h > 0) || (!long && h < 0) {
			break
		}
	}
	return j + 1
}

type stopTrade struct {
	Time   int64
	Long   bool
	Ret    float64 // net %, after fees
	Risk   float64 // % distance entry->stop
	Reason string  // stop | tp | time
	Bars   int
}

func simStop(s15 *Series, i int, long bool, r StopRule, feePct float64) (stopTrade, bool) {
	entry := s15.Klines[i].Close
	sgn := 1.0
	if !long {
		sgn = -1
	}
	n := r.PivotLen
	if n <= 0 {
		n = 3
	}
	var ext float64
	ref := 0 // DIF reference range start/end for MACD confirmation
	refEnd := i
	switch r.Stop {
	case StopPivot:
		v, j, ok := pivotExtreme(s15, i, long, n)
		if ok {
			ext, ref, refEnd = v, j-n, min(j+n, i)
		} else {
			ext, ref = legExtreme(s15, i, long), legStart(s15, i, long)
		}
	default:
		ext, ref = legExtreme(s15, i, long), legStart(s15, i, long)
	}
	pad := r.ATRMult * atr14(s15, i)
	stop := ext*(1-sgn*r.Buffer/100) - sgn*pad
	if sgn*(entry-stop) <= 0 { // swing already broken / on the wrong side of price
		ext, ref, refEnd = legExtreme(s15, i, long), legStart(s15, i, long), i
		stop = ext*(1-sgn*r.Buffer/100) - sgn*pad
		if sgn*(entry-stop) <= 0 {
			return stopTrade{}, false
		}
	}
	risk := sgn * (entry - stop)
	tp := 0.0
	if r.TPMult > 0 {
		tp = entry + sgn*r.TPMult*risk
	}
	tr := stopTrade{Time: s15.Klines[i].CloseTime.Unix(), Long: long, Risk: risk / entry * 100}
	difRef := difExtreme(s15, ref, refEnd, long)
	hard := 0.0
	if r.HardStopPct > 0 {
		hard = entry * (1 - sgn*r.HardStopPct/100)
	}
	end := min(i+r.MaxBars, len(s15.Klines)-1)
	exit, reason, j := s15.Klines[end].Close, "time", end
	for k := i + 1; k <= end; k++ {
		kl := s15.Klines[k]
		if r.MACDConfirm {
			if hard > 0 && ((long && kl.Low <= hard) || (!long && kl.High >= hard)) {
				exit, reason, j = hard, "stop", k
				break
			}
			priceBreak := (long && kl.Low <= stop) || (!long && kl.High >= stop)
			dif := s15.macd.MACD[k]
			difBreak := (long && dif < difRef) || (!long && dif > difRef)
			if priceBreak && difBreak {
				exit, reason, j = kl.Close, "stop", k
				break
			}
			continue
		}
		hitStop := (long && kl.Low <= stop) || (!long && kl.High >= stop)
		hitTP := tp > 0 && ((long && kl.High >= tp) || (!long && kl.Low <= tp))
		if hitStop { // conservative: stop wins when both touch in one bar
			exit, reason, j = stop, "stop", k
			break
		}
		if hitTP {
			exit, reason, j = tp, "tp", k
			break
		}
	}
	tr.Ret = sgn*(exit/entry-1)*100 - 2*feePct
	tr.Reason, tr.Bars = reason, j-i
	return tr, true
}

// StopStudy simulates every resonance signal with each structural stop rule.
func StopStudy(ctx context.Context, ex exchange.Exchange, symbol string, days int, p Params, rules []StopRule, feePct float64, splits int) ([]StopStat, int, error) {
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
	var out []StopStat
	for _, r := range rules {
		groups := map[string][]stopTrade{}
		skipped := map[string]int{}
		for _, s := range sigs {
			g := "做空"
			if s.long {
				g = "做多"
			}
			t, ok := simStop(s15, s.i, s.long, r, feePct)
			if !ok {
				skipped["全部"]++
				skipped[g]++
				continue
			}
			groups["全部"] = append(groups["全部"], t)
			groups[g] = append(groups[g], t)
			if splits > 1 {
				span := time.Since(start)
				idx := int(float64(s15.Klines[s.i].CloseTime.Sub(start)) / float64(span) * float64(splits))
				idx = min(max(idx, 0), splits-1)
				groups[fmt.Sprintf("第%d段", idx+1)] = append(groups[fmt.Sprintf("第%d段", idx+1)], t)
			}
		}
		names := []string{"全部", "做多", "做空"}
		for k := 1; splits > 1 && k <= splits; k++ {
			names = append(names, fmt.Sprintf("第%d段", k))
		}
		for _, g := range names {
			st := summarizeStop(r.Name, g, groups[g])
			st.Skipped = skipped[g]
			out = append(out, st)
		}
	}
	return out, len(sigs), nil
}

func summarizeStop(rule, group string, ts []stopTrade) StopStat {
	st := StopStat{Rule: rule, Group: group, Trades: len(ts)}
	if len(ts) == 0 {
		return st
	}
	var wins, stops, tps int
	var gw, gl, hold, risk, rsum float64
	for _, t := range ts {
		st.SumRet += t.Ret
		risk += t.Risk
		hold += float64(t.Bars) * 0.25
		if t.Risk > 0 {
			rsum += t.Ret / t.Risk
		}
		switch t.Reason {
		case "stop":
			stops++
		case "tp":
			tps++
		}
		if t.Ret > 0 {
			wins++
			gw += t.Ret
		} else {
			gl += t.Ret
		}
	}
	n := float64(len(ts))
	st.WinRate = float64(wins) / n * 100
	st.AvgRet = st.SumRet / n
	st.AvgR = rsum / n
	st.AvgRisk = risk / n
	st.AvgHoldH = hold / n
	st.StopRate = float64(stops) / n * 100
	st.TPRate = float64(tps) / n * 100
	if gl < 0 {
		st.PF = gw / math.Abs(gl)
	}
	return st
}

// Divergence reports a 15m divergence at signal bar i for the given direction:
// long  = the pullback leg made a lower low in price than the previous swing low while its DIF low is higher;
// short = a higher high in price while its DIF high is lower.
func (s *Series) Divergence(i int, long bool, mode string) bool {
	const n = 3
	if !s.valid(i) {
		return false
	}
	ls := legStart(s, i, long)
	if ls < 1 {
		return false
	}
	legPx := legExtreme(s, i, long)
	legDif := difExtreme(s, ls, i, long)
	prev, j, ok := pivotExtreme(s, ls-1, long, n)
	if !ok || !s.valid(j-n) {
		return false
	}
	prevDif := difExtreme(s, j-n, min(j+n, ls-1), long)
	// Normalise to the "long" orientation: lower = more extreme in the pullback direction.
	px, pv, dif, pdif := legPx, prev, legDif, prevDif
	if !long {
		px, pv, dif, pdif = -px, -pv, -dif, -pdif
	}
	switch mode {
	case "near": // price roughly retests the previous swing (within 0.2%) while DIF is higher
		return px <= pv+0.002*math.Abs(pv) && dif > pdif
	case "hidden": // price higher low, DIF lower low (trend continuation)
		return px > pv && dif < pdif
	case "dif_up": // DIF low higher than the previous swing's DIF low, any price
		return dif > pdif
	default: // regular: price lower low, DIF higher low
		return px < pv && dif > pdif
	}
}
