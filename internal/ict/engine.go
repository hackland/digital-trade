package ict

import (
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/jayce/btc-trader/internal/exchange"
)

const (
	atrPeriod = 14
	h1Dur     = time.Hour
	h4Dur     = 4 * time.Hour
	m15Dur    = 15 * time.Minute
)

// market bundles the three timeframes the model needs.
type market struct {
	h4, h1, m15 []exchange.Kline
}

type level struct {
	price float64
	idx   int // swing bar index, -1 for the previous-day level
	kind  string
}

// candidate is a liquidity sweep waiting for a structure shift.
type candidate struct {
	sweepIdx int
	level    float64
	kind     string
	extreme  float64 // low of the sweep bar; the stop hides beyond it
	mssIdx   int     // -1 until the shift is confirmed
	refHigh  float64
	breakout bool // displacement breakout with no preceding sweep
}

type zone struct {
	kind        string
	top, bottom float64
	t           time.Time
}

type tpCand struct {
	price float64
	kind  string
}

// detector runs the long-side model. The short side is the same model run on
// price-mirrored data (see Run), so every comparison here assumes "long".
type detector struct {
	m           market
	p           Params
	windowStart time.Time
	rej         map[string]int

	atr      []float64
	swH, swL []bool
	trend4   []int8
	days     map[int64]dayRange

	q4        int // index of the latest closed 4h bar
	busyUntil time.Time
	farFuture time.Time

	sweeps, shifts, breakouts int // funnel counters (bars inside the reporting window only)
}

func newDetector(m market, windowStart time.Time, p Params, rej map[string]int) *detector {
	return &detector{
		m:           m,
		p:           p,
		windowStart: windowStart,
		rej:         rej,
		atr:         atrSeries(m.h1, atrPeriod),
		trend4:      trendSeries(m.h4, p.SwingN),
		days:        dayRanges(m.h1),
		q4:          -1,
		farFuture:   time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC),
	}
}

func (d *detector) run() []Setup {
	h1, n, p := d.m.h1, d.p.SwingN, d.p
	if len(h1) < atrPeriod+n+3 {
		return nil
	}
	d.swH, d.swL = swingFlags(h1, n)

	var (
		levels  []level
		cands   []*candidate
		out     []Setup
		pdSwept = map[int64]bool{}
	)

	for i := range h1 {
		bar := h1[i]
		closeT := bar.OpenTime.Add(h1Dur)
		for d.q4+1 < len(d.m.h4) && !d.m.h4[d.q4+1].OpenTime.Add(h4Dur).After(closeT) {
			d.q4++
		}

		// A swing low becomes a known liquidity pool once its right side has printed.
		if j := i - n; j >= 0 && d.swL[j] {
			lv := level{price: h1[j].Low, idx: j, kind: "SSL"}
			for k := range levels {
				if math.Abs(levels[k].price-lv.price)/math.Abs(lv.price)*100 <= p.EqualTolPct {
					levels[k].kind, lv.kind = "EQL", "EQL"
				}
			}
			levels = append(levels, lv)
		}

		// Sweep: the wick takes the level out, the close finishes back above it.
		var hit *level
		keep := levels[:0]
		for _, lv := range levels {
			if i-lv.idx > p.LiqLookback {
				continue
			}
			if bar.Low < lv.price {
				if bar.Close > lv.price && (hit == nil || lv.price < hit.price) {
					c := lv
					hit = &c
				}
				continue
			}
			keep = append(keep, lv)
		}
		levels = keep

		dayKey := bar.OpenTime.Unix() / 86400
		if pd, ok := d.days[dayKey-1]; ok && !pdSwept[dayKey] && bar.Low < pd.low {
			pdSwept[dayKey] = true
			if bar.Close > pd.low && (hit == nil || pd.low < hit.price) {
				hit = &level{price: pd.low, idx: -1, kind: "PDL"}
			}
		}
		if hit != nil {
			if !closeT.Before(d.windowStart) {
				d.sweeps++
			}
			cands = append(cands, &candidate{sweepIdx: i, level: hit.price, kind: hit.kind, extreme: bar.Low, mssIdx: -1})
		}

		shiftedNow := false
		next := cands[:0]
		for _, c := range cands {
			if c.sweepIdx < i && bar.Low < c.extreme {
				continue // more liquidity was taken; a fresh sweep starts its own candidate
			}
			if c.mssIdx < 0 {
				if i-c.sweepIdx > p.MaxSweepToMSS {
					continue
				}
				if d.atr[i] > 0 && bar.Close-bar.Open >= p.DispATR*d.atr[i] {
					if ref, ok := d.refSwingHigh(c.sweepIdx, i); ok && bar.Close > ref {
						c.mssIdx, c.refHigh = i, ref
						shiftedNow = true
						if !closeT.Before(d.windowStart) {
							d.shifts++
						}
					}
				}
				next = append(next, c)
				continue
			}
			// The bar after the shift is where the displacement FVG is complete.
			if i == c.mssIdx+1 {
				if s, reason := d.arm(c, i); reason != "" {
					d.rej[reason]++
				} else if s != nil {
					out = append(out, *s)
				}
			}
		}
		// Breakout mode: a displacement bar that is the first close above the latest swing
		// high, with no sweep before it. Sweep-based shifts on this bar take precedence.
		if p.Breakout && !shiftedNow && i > 0 && d.atr[i] > 0 && bar.Close-bar.Open >= p.DispATR*d.atr[i] {
			if ref, ok := d.refSwingHigh(i, i); ok && bar.Close > ref && h1[i-1].Close <= ref {
				if !closeT.Before(d.windowStart) {
					d.breakouts++
				}
				next = append(next, &candidate{
					sweepIdx: i, level: ref, kind: "BOS", mssIdx: i, refHigh: ref, breakout: true,
					extreme: math.Min(h1[i-1].Low, bar.Low),
				})
			}
		}
		cands = next
	}
	return out
}

// refSwingHigh is the latest swing high before the sweep that is already confirmed at bar k.
func (d *detector) refSwingHigh(sweepIdx, k int) (float64, bool) {
	lo := sweepIdx - d.p.LiqLookback
	if lo < 0 {
		lo = 0
	}
	for j := sweepIdx - 1; j >= lo; j-- {
		if d.swH[j] && j+d.p.SwingN <= k {
			return d.m.h1[j].High, true
		}
	}
	return 0, false
}

// arm validates the candidate on bar k and, if it passes, plans and simulates the trade.
// reason == "" with a nil setup means "skipped silently" (outside the window).
func (d *detector) arm(c *candidate, k int) (*Setup, string) {
	h1, p := d.m.h1, d.p
	armT := h1[k].OpenTime.Add(h1Dur)
	if armT.Before(d.windowStart) {
		return nil, ""
	}
	if armT.Before(d.busyUntil) {
		return nil, "busy"
	}
	var tr int8
	if d.q4 >= 0 {
		tr = d.trend4[d.q4]
	}
	if p.RequireBias && tr != 1 {
		return nil, "counter_bias"
	}

	mss := c.mssIdx
	atrK := d.atr[mss]
	impHigh := h1[c.sweepIdx].High
	for x := c.sweepIdx; x <= mss; x++ {
		impHigh = math.Max(impHigh, h1[x].High)
	}

	z, reason := d.pickZone(c, mss, k, atrK)
	if reason != "" {
		return nil, reason
	}
	entry := z.top - p.EntryDepth*(z.top-z.bottom)
	if p.RequireDiscount && entry > (c.extreme+impHigh)/2 {
		return nil, "not_discount"
	}
	if h1[k].Low <= entry {
		return nil, "retraced_early"
	}
	stop := c.extreme - p.StopBufATR*atrK
	risk := entry - stop
	riskPct := risk / math.Abs(entry) * 100
	if riskPct > p.MaxRiskPct {
		return nil, "risk_too_wide"
	}
	if risk <= 0 || riskPct < p.MinRiskPct {
		return nil, "risk_too_small"
	}
	tp, tpKind := d.pickTP(entry, risk, impHigh, k)

	model := "reversal"
	if c.breakout {
		model = "breakout"
	}
	s := &Setup{
		ID:           fmt.Sprintf("long-%d", armT.Unix()),
		Model:        model,
		Dir:          "long",
		Bias:         trendName(tr),
		SweepTime:    h1[c.sweepIdx].OpenTime,
		SweepKind:    c.kind,
		SweepLevel:   c.level,
		SweepExtreme: c.extreme,
		MSSTime:      h1[mss].OpenTime,
		MSSLevel:     c.refHigh,
		ZoneKind:     z.kind,
		ZoneTime:     z.t,
		ZoneTop:      z.top,
		ZoneBottom:   z.bottom,
		ArmTime:      armT,
		Entry:        entry,
		Stop:         stop,
		TP:           tp,
		TPKind:       tpKind,
		PlannedRR:    (tp - entry) / risk,
		Status:       StatusPending,
		EndTime:      armT,
	}
	d.simulate(s)
	if s.Status == StatusPending || s.Status == StatusOpen {
		d.busyUntil = d.farFuture
	} else {
		d.busyUntil = s.EndTime
	}
	return s, ""
}

// pickZone prefers the largest unmitigated bullish FVG created by the move, falling
// back to the last bearish candle before the displacement (order block). An order
// block that price has already traded back into is discarded, not replaced by an older one.
// The reason is "" on success, otherwise the rejection key.
func (d *detector) pickZone(c *candidate, mss, k int, atrK float64) (zone, string) {
	h1 := d.m.h1
	var best zone
	bestSize := -1.0
	for m := maxInt(c.sweepIdx, 1); m <= mss && m+1 <= k; m++ {
		gapB, gapT := h1[m-1].High, h1[m+1].Low
		if gapT <= gapB {
			continue
		}
		size := gapT - gapB
		if size < d.p.MinFVGATR*atrK {
			continue
		}
		filled := false
		for x := m + 2; x <= k; x++ {
			if h1[x].Low <= gapB {
				filled = true
				break
			}
		}
		if filled {
			continue
		}
		if size >= bestSize {
			best, bestSize = zone{kind: "FVG", top: gapT, bottom: gapB, t: h1[m-1].OpenTime}, size
		}
	}
	if bestSize >= 0 {
		return best, ""
	}
	for x := mss - 1; x >= c.sweepIdx; x-- {
		if h1[x].Close >= h1[x].Open {
			continue
		}
		// The bar right after the block is the displacement itself, so mitigation is
		// any later bar (up to the arming bar) that trades back up to the block's top.
		for y := x + 2; y <= k; y++ {
			if h1[y].Low <= h1[x].High {
				return zone{}, "ob_mitigated"
			}
		}
		return zone{kind: "OB", top: h1[x].High, bottom: h1[x].Low, t: h1[x].OpenTime}, ""
	}
	return zone{}, "no_zone"
}

// pickTP returns the nearest liquidity pool above the entry that pays at least
// MinRR; with none available it falls back to a fixed MinRR multiple.
func (d *detector) pickTP(entry, risk, impHigh float64, k int) (float64, string) {
	h1, p := d.m.h1, d.p
	need := entry + p.MinRR*risk
	cands := []tpCand{{impHigh, "BSL"}}
	for j := k - p.SwingN; j >= 0 && j >= k-2*p.LiqLookback; j-- {
		if d.swH[j] && h1[j].High > entry {
			cands = append(cands, tpCand{h1[j].High, "BSL"})
		}
	}
	if pd, ok := d.days[h1[k].OpenTime.Unix()/86400-1]; ok && pd.high > entry {
		cands = append(cands, tpCand{pd.high, "PDH"})
	}
	sort.SliceStable(cands, func(a, b int) bool { return cands[a].price < cands[b].price })
	for _, c := range cands {
		if c.price >= need {
			return c.price, c.kind
		}
	}
	return need, "rr"
}

// simulate replays the 15m bars after arming: a limit order at Entry, then a
// stop and a target. When both are inside one bar the stop is assumed first, and
// the fill bar can only stop out (its high is not credited toward the target).
func (d *detector) simulate(s *Setup) {
	m15, p := d.m.m15, d.p
	idx := sort.Search(len(m15), func(i int) bool { return !m15[i].OpenTime.Before(s.ArmTime) })
	if idx >= len(m15) {
		return
	}
	finish := func(status string, exitAt time.Time, exitPrice float64) {
		s.Status, s.EndTime = status, exitAt
		if s.FillTime == nil {
			return
		}
		t, risk := exitAt, s.FillPrice-s.Stop
		s.ExitTime, s.ExitPrice = &t, exitPrice
		s.R = (exitPrice-s.FillPrice)/risk - p.FeeRate*(math.Abs(s.FillPrice)+math.Abs(exitPrice))/risk
	}

	waited, held := 0, 0
	for b := idx; b < len(m15); b++ {
		bar := m15[b]
		barEnd := bar.OpenTime.Add(m15Dur)
		if s.FillTime == nil {
			waited++
			if bar.Low > s.Entry {
				if bar.High >= s.TP {
					finish(StatusMissed, barEnd, 0)
					return
				}
				if waited >= p.ExpiryBars {
					finish(StatusExpired, barEnd, 0)
					return
				}
				continue
			}
			fill := s.Entry
			if bar.Open < fill {
				fill = bar.Open
			}
			if fill <= s.Stop {
				finish(StatusInvalid, barEnd, 0)
				return
			}
			t := bar.OpenTime
			s.FillTime, s.FillPrice = &t, fill
			if bar.Low <= s.Stop {
				finish(StatusSL, barEnd, s.Stop)
				return
			}
			continue
		}
		held++
		switch {
		case bar.Low <= s.Stop:
			finish(StatusSL, barEnd, s.Stop)
			return
		case bar.High >= s.TP:
			finish(StatusTP, barEnd, s.TP)
			return
		case held >= p.MaxHoldBars:
			finish(StatusTimeout, barEnd, bar.Close)
			return
		}
	}

	last := m15[len(m15)-1]
	s.EndTime = last.OpenTime.Add(m15Dur)
	if s.FillTime != nil {
		s.Status = StatusOpen
		risk := s.FillPrice - s.Stop
		s.R = (last.Close-s.FillPrice)/risk - p.FeeRate*(math.Abs(s.FillPrice)+math.Abs(last.Close))/risk
	}
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func mirrorKind(k string) string {
	switch k {
	case "SSL":
		return "BSL"
	case "BSL":
		return "SSL"
	case "EQL":
		return "EQH"
	case "PDL":
		return "PDH"
	case "PDH":
		return "PDL"
	}
	return k
}

// unflip converts a setup found on price-mirrored data back to real prices.
func unflip(s Setup) Setup {
	neg := func(v float64) float64 {
		if v == 0 {
			return 0
		}
		return -v
	}
	s.Dir = "short"
	s.ID = "short" + s.ID[len("long"):]
	s.SweepLevel, s.SweepExtreme, s.MSSLevel = neg(s.SweepLevel), neg(s.SweepExtreme), neg(s.MSSLevel)
	s.ZoneTop, s.ZoneBottom = neg(s.ZoneBottom), neg(s.ZoneTop)
	s.Entry, s.Stop, s.TP = neg(s.Entry), neg(s.Stop), neg(s.TP)
	s.FillPrice, s.ExitPrice = neg(s.FillPrice), neg(s.ExitPrice)
	s.SweepKind, s.TPKind = mirrorKind(s.SweepKind), mirrorKind(s.TPKind)
	switch s.Bias {
	case "up":
		s.Bias = "down"
	case "down":
		s.Bias = "up"
	}
	return s
}
