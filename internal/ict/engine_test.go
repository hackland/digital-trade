package ict

import (
	"math"
	"testing"
	"time"

	"github.com/jayce/btc-trader/internal/exchange"
)

var t0 = time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

type ohlc struct{ o, h, l, c float64 }

// hourly builds 1h klines: 20 quiet warm-up bars (ATR ~2, no swing points) followed by
// a hand-drawn sequence containing one swing high (118), a swing low (99), a lower high
// (108), a sweep of 99 on bar 38, a displacement through 108 on bar 39 and the FVG-
// completing bar 40.
func hourly() []exchange.Kline {
	var bars []ohlc
	prev := 100.0
	for j := 0; j < 20; j++ {
		c := 100.0
		if j%2 == 1 {
			c = 101
		}
		bars = append(bars, ohlc{prev, c + 1, c - 1, c})
		prev = c
	}
	bars = append(bars, []ohlc{
		{101, 104, 100.5, 103.5},   // 20
		{103.5, 110, 103, 109},     // 21
		{109, 118, 108.5, 112},     // 22 swing high 118
		{112, 114, 108, 109},       // 23
		{109, 112, 106, 107},       // 24
		{107, 108, 102, 103},       // 25
		{103, 104, 100, 101},       // 26
		{101, 102, 99.5, 100},      // 27
		{100, 101, 99, 99.5},       // 28 swing low 99
		{99.5, 102, 99.2, 101.5},   // 29
		{101.5, 104, 100.5, 103.5}, // 30
		{103.5, 106, 102.5, 105},   // 31
		{105, 107, 104, 106},       // 32
		{106, 108, 105, 105.5},     // 33 swing high 108
		{105.5, 106, 103, 103.5},   // 34
		{103.5, 105, 101.5, 102},   // 35
		{102, 103, 100.5, 101},     // 36
		{101, 102, 100, 100.5},     // 37
		{100.5, 102.5, 98, 101.5},  // 38 sweep of 99
		{101.5, 109.5, 101.2, 109}, // 39 displacement, closes above 108
		{109, 111, 105, 110},       // 40 completes FVG [102.5, 105]
	}...)
	ks := make([]exchange.Kline, len(bars))
	for i, b := range bars {
		ks[i] = exchange.Kline{OpenTime: t0.Add(time.Duration(i) * time.Hour), Open: b.o, High: b.h, Low: b.l, Close: b.c, IsFinal: true}
	}
	return ks
}

// armTime is the close of 1h bar 40.
var armTime = t0.Add(41 * time.Hour)

func m15From(bars []ohlc) []exchange.Kline {
	ks := make([]exchange.Kline, len(bars))
	for i, b := range bars {
		ks[i] = exchange.Kline{OpenTime: armTime.Add(time.Duration(i) * 15 * time.Minute), Open: b.o, High: b.h, Low: b.l, Close: b.c, IsFinal: true}
	}
	return ks
}

func testParams() Params {
	p := DefaultParams()
	p.SwingN = 2
	p.DispATR = 1.0
	p.MinFVGATR = 0
	p.EntryDepth = 0.6
	p.MaxRiskPct = 10
	p.RequireBias = false
	p.RequireDiscount = true
	p.FeeRate = 0
	return p
}

func mirror(ks []exchange.Kline, c float64) []exchange.Kline {
	out := make([]exchange.Kline, len(ks))
	for i, k := range ks {
		k.Open, k.Close = c-k.Open, c-k.Close
		k.High, k.Low = c-k.Low, c-k.High
		out[i] = k
	}
	return out
}

func tpPath() []ohlc {
	return []ohlc{
		{110, 110, 103.4, 104}, // tags the entry (103.5), fill bar
		{104, 105, 103.8, 104.5},
		{104.5, 119, 104.4, 118.5}, // runs through 118
	}
}

func slPath() []ohlc {
	return []ohlc{
		{110, 110, 103.4, 104},
		{104, 104.2, 96, 97}, // through the stop (~97.1)
	}
}

func TestSwingFlags(t *testing.T) {
	ks := hourly()
	hi, lo := swingFlags(ks, 2)
	if !hi[22] || !hi[33] {
		t.Fatalf("expected swing highs at 22 and 33, got hi[22]=%v hi[33]=%v", hi[22], hi[33])
	}
	if !lo[28] {
		t.Fatalf("expected swing low at 28")
	}
	if hi[len(ks)-1] || lo[len(ks)-1] {
		t.Fatalf("last bar cannot be a confirmed swing")
	}
	for j := 0; j < 18; j++ { // bar 18 (low 99) is a genuine swing low once the rally starts
		if hi[j] || lo[j] {
			t.Fatalf("warm-up bar %d should not be a swing", j)
		}
	}
}

func TestTrendSeries(t *testing.T) {
	mk := func(closes []float64) []exchange.Kline {
		ks := make([]exchange.Kline, len(closes))
		for i, c := range closes {
			ks[i] = exchange.Kline{OpenTime: t0.Add(time.Duration(i) * h4Dur), Open: c, High: c + 1, Low: c - 1, Close: c}
		}
		return ks
	}
	// swing high at idx 2 (high 111), then a close at 115 breaks it -> up; a later
	// swing low at idx 6 (low 99) is broken by a close at 90 -> down.
	ks := mk([]float64{100, 105, 110, 106, 104, 108, 100, 103, 104, 115, 112, 109, 108, 90})
	tr := trendSeries(ks, 2)
	if tr[8] != 0 {
		t.Fatalf("no break yet at 8, got %d", tr[8])
	}
	if tr[9] != 1 {
		t.Fatalf("expected up after close above swing high, got %d", tr[9])
	}
	if tr[len(tr)-1] != -1 {
		t.Fatalf("expected down after close below swing low, got %d", tr[len(tr)-1])
	}
}

func TestLongSetupHitsTarget(t *testing.T) {
	m := market{h1: hourly(), m15: m15From(tpPath())}
	res := Run(m, t0, testParams())
	if len(res.Setups) != 1 {
		t.Fatalf("want 1 setup, got %d (rejected=%v)", len(res.Setups), res.Rejected)
	}
	s := res.Setups[0]
	if s.Dir != "long" || s.ZoneKind != "FVG" || s.SweepKind == "" {
		t.Fatalf("unexpected setup %+v", s)
	}
	if !s.ArmTime.Equal(armTime) {
		t.Fatalf("arm time %v, want %v", s.ArmTime, armTime)
	}
	if math.Abs(s.ZoneBottom-102.5) > 1e-9 || math.Abs(s.ZoneTop-105) > 1e-9 {
		t.Fatalf("FVG [%v, %v], want [102.5, 105]", s.ZoneBottom, s.ZoneTop)
	}
	if math.Abs(s.Entry-103.5) > 1e-9 {
		t.Fatalf("entry %v, want 103.5", s.Entry)
	}
	if s.Stop >= 98 || s.Stop < 96 {
		t.Fatalf("stop %v should sit just below the sweep low 98", s.Stop)
	}
	if s.TP != 118 || s.TPKind == "rr" {
		t.Fatalf("target %v (%s), want the 118 liquidity pool", s.TP, s.TPKind)
	}
	if s.PlannedRR < 2 {
		t.Fatalf("planned RR %v below minimum", s.PlannedRR)
	}
	if s.Status != StatusTP {
		t.Fatalf("status %s, want tp", s.Status)
	}
	wantR := (118 - 103.5) / (103.5 - s.Stop)
	if math.Abs(s.R-wantR) > 1e-6 {
		t.Fatalf("R %v, want %v", s.R, wantR)
	}
	if res.Stats.Resolved != 1 || res.Stats.Wins != 1 || res.Stats.WinRate != 1 {
		t.Fatalf("stats %+v", res.Stats)
	}
}

func TestLongSetupStopsOut(t *testing.T) {
	res := Run(market{h1: hourly(), m15: m15From(slPath())}, t0, testParams())
	if len(res.Setups) != 1 {
		t.Fatalf("want 1 setup, got %d", len(res.Setups))
	}
	s := res.Setups[0]
	if s.Status != StatusSL {
		t.Fatalf("status %s, want sl", s.Status)
	}
	if s.R > -0.99 || s.R < -1.01 {
		t.Fatalf("R %v, want about -1", s.R)
	}
}

func TestFeesReduceR(t *testing.T) {
	p := testParams()
	p.FeeRate = 0.001
	s := Run(market{h1: hourly(), m15: m15From(tpPath())}, t0, p).Setups[0]
	gross := (118 - 103.5) / (103.5 - s.Stop)
	if s.R >= gross {
		t.Fatalf("net R %v should be below gross %v", s.R, gross)
	}
}

func TestPendingWhenNoRetrace(t *testing.T) {
	away := []ohlc{{110, 112, 109, 111}, {111, 113, 110, 112}}
	s := Run(market{h1: hourly(), m15: m15From(away)}, t0, testParams()).Setups[0]
	if s.Status != StatusPending {
		t.Fatalf("status %s, want pending", s.Status)
	}
	if s.FillTime != nil {
		t.Fatalf("should not be filled")
	}
}

func TestMissedWhenTargetFirst(t *testing.T) {
	away := []ohlc{{110, 119, 109, 118.5}}
	s := Run(market{h1: hourly(), m15: m15From(away)}, t0, testParams()).Setups[0]
	if s.Status != StatusMissed {
		t.Fatalf("status %s, want missed", s.Status)
	}
}

func TestShortIsTheMirrorImage(t *testing.T) {
	const c = 300.0
	m := market{h1: mirror(hourly(), c), m15: mirror(m15From(tpPath()), c)}
	p := testParams()
	p.Direction = "short"
	res := Run(m, t0, p)
	if len(res.Setups) != 1 {
		t.Fatalf("want 1 short setup, got %d (rejected=%v)", len(res.Setups), res.Rejected)
	}
	s := res.Setups[0]
	if s.Dir != "short" || s.Status != StatusTP {
		t.Fatalf("unexpected setup %+v", s)
	}
	if math.Abs(s.Entry-(c-103.5)) > 1e-9 || s.TP != c-118 {
		t.Fatalf("entry %v tp %v", s.Entry, s.TP)
	}
	if s.Stop <= s.Entry || s.TP >= s.Entry {
		t.Fatalf("short needs stop above and target below the entry: %+v", s)
	}
	if s.ZoneTop <= s.ZoneBottom {
		t.Fatalf("zone inverted: %+v", s)
	}
	long := Run(market{h1: hourly(), m15: m15From(tpPath())}, t0, testParams()).Setups[0]
	if math.Abs(s.R-long.R) > 1e-9 {
		t.Fatalf("mirror R %v != long R %v", s.R, long.R)
	}
	if s.SweepKind == long.SweepKind {
		t.Fatalf("sweep kind should be mirrored, both %q", s.SweepKind)
	}
}

func TestDirectionFilter(t *testing.T) {
	m := market{h1: hourly(), m15: m15From(tpPath())}
	p := testParams()
	p.Direction = "short"
	if n := len(Run(m, t0, p).Setups); n != 0 {
		t.Fatalf("short-only run on a long structure produced %d setups", n)
	}
}

func TestBiasFilterRejects(t *testing.T) {
	p := testParams()
	p.RequireBias = true // no 4h data -> trend "none" -> counter-bias
	res := Run(market{h1: hourly(), m15: m15From(tpPath())}, t0, p)
	if len(res.Setups) != 0 || res.Rejected["counter_bias"] == 0 {
		t.Fatalf("expected a counter_bias rejection, got setups=%d rejected=%v", len(res.Setups), res.Rejected)
	}
}

func TestDiscountFilterRejects(t *testing.T) {
	p := testParams()
	p.EntryDepth = 0 // entry at the top of the FVG (105) is above the 103.75 equilibrium
	res := Run(market{h1: hourly(), m15: m15From(tpPath())}, t0, p)
	if len(res.Setups) != 0 || res.Rejected["not_discount"] == 0 {
		t.Fatalf("expected not_discount, got setups=%d rejected=%v", len(res.Setups), res.Rejected)
	}
}

func TestRiskTooWideRejects(t *testing.T) {
	p := testParams()
	p.MaxRiskPct = 1
	res := Run(market{h1: hourly(), m15: m15From(tpPath())}, t0, p)
	if len(res.Setups) != 0 || res.Rejected["risk_too_wide"] == 0 {
		t.Fatalf("expected risk_too_wide, got %v", res.Rejected)
	}
}

func TestWindowStartHidesWarmupSetups(t *testing.T) {
	res := Run(market{h1: hourly(), m15: m15From(tpPath())}, armTime.Add(time.Hour), testParams())
	if len(res.Setups) != 0 {
		t.Fatalf("setup armed before the window must not be reported")
	}
}

// Detection must not peek at the future: the setup found on a prefix that ends right
// at the arming bar is identical to the one found on the full series.
func TestNoLookahead(t *testing.T) {
	full := hourly()
	extra := make([]exchange.Kline, 0, 10)
	last := full[len(full)-1]
	for i := 1; i <= 8; i++ { // keep rallying after the arming bar
		c := last.Close + float64(i)
		extra = append(extra, exchange.Kline{OpenTime: last.OpenTime.Add(time.Duration(i) * time.Hour), Open: c - 1, High: c + 1, Low: c - 2, Close: c, IsFinal: true})
	}
	a := Run(market{h1: full, m15: m15From(tpPath())}, t0, testParams()).Setups
	b := Run(market{h1: append(append([]exchange.Kline{}, full...), extra...), m15: m15From(tpPath())}, t0, testParams()).Setups
	if len(a) != 1 || len(b) < 1 {
		t.Fatalf("setups: prefix=%d full=%d", len(a), len(b))
	}
	x, y := a[0], b[0]
	if x.ID != y.ID || x.Entry != y.Entry || x.Stop != y.Stop || x.TP != y.TP || x.ZoneBottom != y.ZoneBottom || x.ZoneTop != y.ZoneTop {
		t.Fatalf("prefix and full-series setups differ:\n%+v\n%+v", x, y)
	}
}

func TestSummarizeBuckets(t *testing.T) {
	tm := t0
	mk := func(dir, status string, r float64, exit time.Time) Setup {
		e := exit
		return Setup{Dir: dir, Status: status, R: r, ZoneKind: "FVG", SweepKind: "SSL", TPKind: "BSL", FillTime: &tm, ExitTime: &e, PlannedRR: 2}
	}
	setups := []Setup{
		mk("long", StatusTP, 2, t0.Add(1*time.Hour)),
		mk("long", StatusSL, -1, t0.Add(2*time.Hour)),
		mk("short", StatusSL, -1, t0.Add(3*time.Hour)),
		{Dir: "long", Status: StatusExpired, PlannedRR: 2},
	}
	st, buckets, eq := summarize(setups)
	if st.Armed != 4 || st.Resolved != 3 || st.Wins != 1 || st.Expired != 1 {
		t.Fatalf("stats %+v", st)
	}
	if math.Abs(st.TotalR-0) > 1e-9 || math.Abs(st.ProfitFactor-1) > 1e-9 {
		t.Fatalf("total %v pf %v", st.TotalR, st.ProfitFactor)
	}
	if math.Abs(st.MaxDrawdownR-2) > 1e-9 {
		t.Fatalf("max drawdown %v, want 2", st.MaxDrawdownR)
	}
	if len(eq) != 3 || eq[2].R != 0 {
		t.Fatalf("equity %+v", eq)
	}
	found := false
	for _, b := range buckets {
		if b.Group == "direction" && b.Name == "short" && b.Count == 1 && b.WinRate == 0 {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing short bucket: %+v", buckets)
	}
}

// noSweepHourly is hourly() with bar 38 no longer wicking below the 99 lows, so the
// bar-39 displacement through 108 is a plain breakout rather than a sweep reversal.
func noSweepHourly() []exchange.Kline {
	ks := hourly()
	ks[38].Low = 100.2
	return ks
}

func TestBreakoutOffIgnoresPlainBreakout(t *testing.T) {
	res := Run(market{h1: noSweepHourly(), m15: m15From(tpPath())}, t0, testParams())
	if len(res.Setups) != 0 {
		t.Fatalf("without the breakout switch a no-sweep move must not signal, got %d", len(res.Setups))
	}
}

func TestBreakoutLong(t *testing.T) {
	p := testParams()
	p.Breakout = true
	res := Run(market{h1: noSweepHourly(), m15: m15From(tpPath())}, t0, p)
	if len(res.Setups) != 1 {
		t.Fatalf("want 1 breakout setup, got %d (rejected=%v funnel=%+v)", len(res.Setups), res.Rejected, res.Funnel)
	}
	s := res.Setups[0]
	if s.Model != "breakout" || s.SweepKind != "BOS" || s.ZoneKind != "FVG" {
		t.Fatalf("unexpected setup %+v", s)
	}
	if s.MSSLevel != 108 || !s.ArmTime.Equal(armTime) {
		t.Fatalf("broken level %v / arm %v", s.MSSLevel, s.ArmTime)
	}
	// stop hides under the two-bar base (min low 100.2), not under a sweep wick
	if s.Stop >= 100.2 || s.Stop < 98 {
		t.Fatalf("stop %v should sit just below 100.2", s.Stop)
	}
	if s.Status != StatusTP || res.Funnel.Breakouts != 1 {
		t.Fatalf("status %s funnel %+v", s.Status, res.Funnel)
	}
}

func TestBreakoutShortMirror(t *testing.T) {
	const c = 300.0
	p := testParams()
	p.Breakout, p.Direction = true, "short"
	res := Run(market{h1: mirror(noSweepHourly(), c), m15: mirror(m15From(tpPath()), c)}, t0, p)
	if len(res.Setups) != 1 {
		t.Fatalf("want 1 short breakout, got %d (rejected=%v)", len(res.Setups), res.Rejected)
	}
	s := res.Setups[0]
	if s.Dir != "short" || s.Model != "breakout" || s.SweepKind != "BOS" || s.Stop <= s.Entry || s.TP >= s.Entry {
		t.Fatalf("unexpected short breakout %+v", s)
	}
}

// When a sweep already explains the shift on the same bar, the breakout path must not
// add a second setup for it.
func TestBreakoutDoesNotDuplicateSweepSetup(t *testing.T) {
	p := testParams()
	p.Breakout = true
	res := Run(market{h1: hourly(), m15: m15From(tpPath())}, t0, p)
	if len(res.Setups) != 1 || res.Setups[0].Model != "reversal" {
		t.Fatalf("want exactly the reversal setup, got %d: %+v", len(res.Setups), res.Setups)
	}
}

// obHourly makes bar 38 (the sweep bar) bearish so it becomes the order block [98, 102.5],
// and sets bar 40's low to control whether price has traded back into that block.
func obHourly(bar40Low float64) []exchange.Kline {
	ks := hourly()
	ks[38].Open, ks[38].Close = 102, 100.5 // still sweeps 99 and closes back above it
	ks[40].Low = bar40Low
	return ks
}

func obParams() Params {
	p := testParams()
	p.MinFVGATR = 1.0 // the 1.5-point FVG is too small, so the order block is the fallback
	return p
}

func obPath() []ohlc {
	return []ohlc{
		{110, 110, 99.7, 100}, // tags the entry (~99.8), fill bar
		{100, 104, 99.9, 103},
		{103, 110, 102.5, 109.8}, // reaches the 109.5 target
	}
}

func TestUnmitigatedOrderBlockTrades(t *testing.T) {
	res := Run(market{h1: obHourly(104), m15: m15From(obPath())}, t0, obParams())
	if len(res.Setups) != 1 {
		t.Fatalf("want 1 setup, got %d (rejected=%v)", len(res.Setups), res.Rejected)
	}
	s := res.Setups[0]
	if s.ZoneKind != "OB" || s.ZoneBottom != 98 || s.ZoneTop != 102.5 {
		t.Fatalf("expected OB [98, 102.5], got %+v", s)
	}
	if s.Status != StatusTP {
		t.Fatalf("status %s, want tp", s.Status)
	}
}

func TestMitigatedOrderBlockIsDiscarded(t *testing.T) {
	// bar 40 dips to 102, inside the block (top 102.5): price has come back to it
	res := Run(market{h1: obHourly(102), m15: m15From(obPath())}, t0, obParams())
	if len(res.Setups) != 0 || res.Rejected["ob_mitigated"] != 1 {
		t.Fatalf("want ob_mitigated rejection, got setups=%d rejected=%v", len(res.Setups), res.Rejected)
	}
}

func TestOrderBlockRevisitedBeforeShiftIsDiscarded(t *testing.T) {
	// sweep (38), bearish block [100.5, 103] (39), a launch bar (40), then a bar that dips
	// back into the block (41) before the displacement (42) and the arming bar (43).
	ks := hourly()[:38]
	rest := []ohlc{
		{100.5, 102.5, 98, 101.5}, // 38 sweep of 99
		{102, 103, 100.5, 101},    // 39 bearish -> order block
		{101, 104, 101, 103.5},    // 40 launch
		{102, 104.5, 101.5, 104},  // 41 comes back down into the block
		{104, 111, 103.8, 110.5},  // 42 displacement through 108
		{110.5, 112, 108.5, 111},  // 43 arming bar
	}
	for i, b := range rest {
		ks = append(ks, exchange.Kline{OpenTime: t0.Add(time.Duration(38+i) * time.Hour), Open: b.o, High: b.h, Low: b.l, Close: b.c, IsFinal: true})
	}
	p := testParams()
	p.MinFVGATR = 3 // keep the FVG out so the order block is the only candidate
	res := Run(market{h1: ks, m15: m15From(obPath())}, t0, p)
	if len(res.Setups) != 0 || res.Rejected["ob_mitigated"] != 1 {
		t.Fatalf("want ob_mitigated, got setups=%d rejected=%v", len(res.Setups), res.Rejected)
	}
}
