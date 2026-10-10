package mtfmacd

import (
	"context"
	"fmt"
	"time"

	"github.com/jayce/btc-trader/internal/exchange"
)

// Failure modes for "15m reversal failed".
const (
	FailDIF    = "dif"    // 15m DIF closes beyond the reversal-trough DIF (new low for longs)
	FailCross  = "cross"  // 15m histogram flips back against the trade after having confirmed it
	FailEither = "either" // dif or cross, whichever comes first
)

// RevExitRule: take profit when the 1h histogram has peaked and faded to PeakRatio of the peak,
// stop when the 15m reversal fails. HardStopPct / MaxBars are safety nets (0 = off / default 192).
type RevExitRule struct {
	Name        string
	PeakRatio   float64
	Fail        string
	HardStopPct float64
	MaxBars     int
	// StopMode, when set, replaces Fail with a price-based stop:
	//   "leg"    below the 15m pullback-leg extreme, minus Buffer %
	//   "pivot1h" below the latest confirmed 1h swing low, minus Buffer %
	//   "atr"    chandelier trail: highest high since entry - ATRMult * ATR14(1h)
	//   "none"   no structural stop (only HardStopPct)
	// TPInProfitOnly: the 1h colour-flip only closes the trade when it is in profit (close beyond entry);
	// otherwise the trade is held and only the stop / time cap can close it.
	TPInProfitOnly bool
	MinProfitPct   float64 // with TPInProfitOnly: required open profit (%) before the 1h flip may close the trade
	Stop1hAgree    bool    // price stops (leg/pivot1h) only fire while the latest closed 1h histogram is against the trade
	StopMode       string
	Buffer         float64
	ATRMult        float64
}

func simRevExit(s1, s15 *Series, i int, long bool, r RevExitRule, feePct float64) (stopTrade, bool) {
	sgn := 1.0
	if !long {
		sgn = -1
	}
	entry := s15.Klines[i].Close
	maxBars := r.MaxBars
	if maxBars <= 0 {
		maxBars = 192
	}
	ref := difExtreme(s15, legStart(s15, i, long), i, long) // reversal trough DIF
	h1 := s1.IndexAtOrBefore(s15.Klines[i].CloseTime, 0)
	if h1 < 0 || !s1.valid(h1) {
		return stopTrade{}, false
	}
	peak := maxF(sgn*s1.macd.Histogram[h1], 0)
	lastH1 := h1
	confirmed := sgn*s15.macd.Histogram[i] > 0 // 15m hist already on the trade side (cross entry)
	hard := 0.0
	if r.HardStopPct > 0 {
		hard = entry * (1 - sgn*r.HardStopPct/100)
	}
	// price stop level (static; the atr mode trails it)
	stop, trailK, hh := 0.0, 0.0, entry
	switch r.StopMode {
	case "leg":
		stop = legExtreme(s15, i, long) * (1 - sgn*r.Buffer/100)
	case "pivot1h":
		if v, _, ok := pivotExtreme(s1, h1, long, 3); ok {
			stop = v * (1 - sgn*r.Buffer/100)
		} else {
			stop = legExtreme(s15, i, long) * (1 - sgn*r.Buffer/100)
		}
	case "atr":
		trailK = r.ATRMult * atr14(s1, h1)
		stop = entry - sgn*trailK
	}
	if r.StopMode != "" && r.StopMode != "none" && sgn*(entry-stop) <= 0 {
		stop = legExtreme(s15, i, long) * (1 - sgn*r.Buffer/100)
		if sgn*(entry-stop) <= 0 {
			return stopTrade{}, false
		}
	}
	tr := stopTrade{Time: s15.Klines[i].CloseTime.Unix(), Long: long}
	end := min(i+maxBars, len(s15.Klines)-1)
	exit, reason, j := s15.Klines[end].Close, "time", end
loop:
	for k := i + 1; k <= end; k++ {
		kl := s15.Klines[k]
		if hard > 0 && ((long && kl.Low <= hard) || (!long && kl.High >= hard)) {
			exit, reason, j = hard, "hard", k
			break
		}
		if r.StopMode != "" {
			if r.StopMode == "atr" {
				if long {
					hh = maxF(hh, kl.High)
				} else {
					hh = -maxF(-hh, -kl.Low)
				}
				if ns := hh - sgn*trailK; sgn*(ns-stop) > 0 {
					stop = ns
				}
			}
			if r.StopMode != "none" && ((long && kl.Low <= stop) || (!long && kl.High >= stop)) {
				agree := true
				if r.Stop1hAgree {
					c := s1.IndexAtOrBefore(s15.Klines[k-1].CloseTime, lastH1) // 1h bar known before this 15m bar
					agree = c >= 0 && sgn*s1.macd.Histogram[c] <= 0
				}
				if agree {
					exit, reason, j = stop, "stop", k
					break
				}
			}
		} else {
			// 15m reversal failure (checked first: it is the stop)
			dif := s15.macd.MACD[k]
			failDIF := (long && dif < ref) || (!long && dif > ref)
			h := sgn * s15.macd.Histogram[k]
			if h > 0 {
				confirmed = true
			}
			failCross := confirmed && h <= 0
			if (r.Fail == FailDIF && failDIF) || (r.Fail == FailCross && failCross) || (r.Fail == FailEither && (failDIF || failCross)) {
				exit, reason, j = kl.Close, "stop", k
				break
			}
		}
		// 1h histogram take profit
		cur := s1.IndexAtOrBefore(kl.CloseTime, lastH1)
		for ; lastH1 < cur; lastH1++ {
			hh := sgn * s1.macd.Histogram[lastH1+1]
			if hh > peak {
				peak = hh
			} else if r.PeakRatio < 0 && hh <= 0 || r.PeakRatio >= 0 && ((peak > 0 && hh <= r.PeakRatio*peak) || (peak == 0 && hh <= 0)) {
				if r.TPInProfitOnly && sgn*(kl.Close/entry-1)*100 <= r.MinProfitPct {
					continue
				}
				exit, reason, j = kl.Close, "tp", k
				break loop
			}
		}
	}
	tr.Ret = sgn*(exit/entry-1)*100 - 2*feePct
	tr.Reason, tr.Bars = reason, j-i
	return tr, true
}

func maxF(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func segName(k int) string { return fmt.Sprintf("第%d段", k) }

// RevExitStudy runs every resonance signal through each RevExitRule.
func RevExitStudy(ctx context.Context, ex exchange.Exchange, symbol string, days int, p Params, rules []RevExitRule, feePct float64, splits int) ([]StopStat, int, error) {
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
	span := float64(time.Since(start))
	var out []StopStat
	for _, r := range rules {
		groups := map[string][]stopTrade{}
		for _, s := range sigs {
			t, ok := simRevExit(s1, s15, s.i, s.long, r, feePct)
			if !ok {
				continue
			}
			groups["全部"] = append(groups["全部"], t)
			if splits > 1 {
				idx := int(float64(s15.Klines[s.i].CloseTime.Sub(start)) / span * float64(splits))
				g := segName(min(max(idx, 0), splits-1) + 1)
				groups[g] = append(groups[g], t)
			}
		}
		names := []string{"全部"}
		for k := 1; splits > 1 && k <= splits; k++ {
			names = append(names, segName(k))
		}
		for _, g := range names {
			out = append(out, summarizeStop(r.Name, g, groups[g]))
		}
	}
	return out, len(sigs), nil
}
