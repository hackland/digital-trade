package ict

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/jayce/btc-trader/internal/exchange"
)

// KlineSource is the slice of exchange.Exchange the analysis needs.
type KlineSource interface {
	GetKlines(ctx context.Context, req exchange.KlineRequest) ([]exchange.Kline, error)
}

// Run executes the model over the given data. Setups armed before windowStart are
// used only to warm up state and are not reported.
func Run(m market, windowStart time.Time, p Params) *Result {
	p = p.normalize()
	rej := map[string]int{}
	var setups []Setup

	var funnel Funnel
	if p.Direction != "short" {
		d := newDetector(m, windowStart, p, rej)
		setups = append(setups, d.run()...)
		funnel.Sweeps += d.sweeps
		funnel.Shifts += d.shifts
		funnel.Breakouts += d.breakouts
	}
	if p.Direction != "long" {
		fm := market{h4: flipKlines(m.h4), h1: flipKlines(m.h1), m15: flipKlines(m.m15)}
		d := newDetector(fm, windowStart, p, rej)
		for _, s := range d.run() {
			setups = append(setups, unflip(s))
		}
		funnel.Sweeps += d.sweeps
		funnel.Shifts += d.shifts
		funnel.Breakouts += d.breakouts
	}
	funnel.Armed = len(setups)
	sort.SliceStable(setups, func(i, j int) bool { return setups[i].ArmTime.Before(setups[j].ArmTime) })

	res := &Result{
		Params:      p,
		GeneratedAt: time.Now(),
		BiasNow:     "none",
		Setups:      setups,
		Rejected:    rej,
		Funnel:      funnel,
		Bars1h:      len(m.h1),
		Bars15m:     len(m.m15),
	}
	if tr := trendSeries(m.h4, p.SwingN); len(tr) > 0 {
		res.BiasNow = trendName(tr[len(tr)-1])
	}
	for _, k := range m.h1 {
		if !k.OpenTime.Before(windowStart) {
			res.Candles = append(res.Candles, Candle{T: k.OpenTime.Unix(), O: k.Open, H: k.High, L: k.Low, C: k.Close})
		}
	}
	if len(m.m15) > 0 {
		res.LastPrice = m.m15[len(m.m15)-1].Close
	}
	res.Stats, res.Buckets, res.Equity = summarize(setups)
	return res
}

func summarize(setups []Setup) (Stats, []Bucket, []EquityPoint) {
	var st Stats
	var resolved []Setup
	var sumWin, sumLoss, plannedRR float64
	for _, s := range setups {
		st.Armed++
		plannedRR += s.PlannedRR
		switch s.Status {
		case StatusPending:
			st.Pending++
		case StatusOpen:
			st.Open++
			st.Filled++
		case StatusExpired:
			st.Expired++
		case StatusMissed:
			st.Missed++
		case StatusInvalid:
			st.Invalid++
		case StatusTP, StatusSL, StatusTimeout:
			st.Filled++
			st.Resolved++
			switch s.Status {
			case StatusTP:
				st.TP++
			case StatusSL:
				st.SL++
			default:
				st.Timeout++
			}
			resolved = append(resolved, s)
			st.TotalR += s.R
			if s.R > 0 {
				st.Wins++
				sumWin += s.R
			} else {
				sumLoss += s.R
			}
		}
	}
	if st.Armed > 0 {
		st.FillRate = float64(st.Filled) / float64(st.Armed)
		st.AvgPlannedRR = plannedRR / float64(st.Armed)
	}
	if st.Resolved > 0 {
		st.WinRate = float64(st.Wins) / float64(st.Resolved)
		st.AvgR = st.TotalR / float64(st.Resolved)
	}
	if st.Wins > 0 {
		st.AvgWinR = sumWin / float64(st.Wins)
	}
	if losses := st.Resolved - st.Wins; losses > 0 {
		st.AvgLossR = sumLoss / float64(losses)
		if sumLoss < 0 {
			st.ProfitFactor = sumWin / -sumLoss
		}
	}

	sort.SliceStable(resolved, func(i, j int) bool { return resolved[i].ExitTime.Before(*resolved[j].ExitTime) })
	var equity []EquityPoint
	cum, peak := 0.0, 0.0
	for _, s := range resolved {
		cum += s.R
		equity = append(equity, EquityPoint{T: s.ExitTime.Unix(), R: cum})
		if cum > peak {
			peak = cum
		}
		if dd := peak - cum; dd > st.MaxDrawdownR {
			st.MaxDrawdownR = dd
		}
	}

	var buckets []Bucket
	add := func(group string, key func(Setup) string) {
		type agg struct {
			n, w int
			sum  float64
		}
		m := map[string]*agg{}
		var names []string
		for _, s := range resolved {
			k := key(s)
			a := m[k]
			if a == nil {
				a = &agg{}
				m[k] = a
				names = append(names, k)
			}
			a.n++
			a.sum += s.R
			if s.R > 0 {
				a.w++
			}
		}
		sort.Strings(names)
		for _, k := range names {
			a := m[k]
			buckets = append(buckets, Bucket{Group: group, Name: k, Count: a.n, WinRate: float64(a.w) / float64(a.n), AvgR: a.sum / float64(a.n), TotalR: a.sum})
		}
	}
	add("model", func(s Setup) string { return s.Model })
	add("direction", func(s Setup) string { return s.Dir })
	add("zone", func(s Setup) string { return s.ZoneKind })
	add("sweep", func(s Setup) string { return s.SweepKind })
	add("target", func(s Setup) string { return s.TPKind })
	return st, buckets, equity
}

var ivDur = map[string]time.Duration{"15m": m15Dur, "1h": h1Dur, "4h": h4Dur}

// fetchRange pages through closed klines covering [start, end].
func fetchRange(ctx context.Context, src KlineSource, symbol, interval string, start, end time.Time) ([]exchange.Kline, error) {
	dur := ivDur[interval]
	var all []exchange.Kline
	cur := start
	for cur.Before(end) {
		c := cur
		ks, err := src.GetKlines(ctx, exchange.KlineRequest{Symbol: symbol, Interval: interval, StartTime: &c, Limit: 1000})
		if err != nil {
			return nil, err
		}
		if len(ks) == 0 {
			break
		}
		all = append(all, ks...)
		next := ks[len(ks)-1].OpenTime.Add(dur)
		if !next.After(cur) {
			break
		}
		cur = next
	}
	now := time.Now()
	out := make([]exchange.Kline, 0, len(all))
	var last time.Time
	for _, k := range all {
		if k.OpenTime.Add(dur).After(now) {
			break
		}
		if !last.IsZero() && !k.OpenTime.After(last) {
			continue
		}
		out = append(out, k)
		last = k.OpenTime
	}
	return out, nil
}

type cacheEntry struct {
	at time.Time
	ks []exchange.Kline
}

var (
	cacheMu sync.Mutex
	cache   = map[string]cacheEntry{}
)

const cacheTTL = 90 * time.Second

// fetchCached trades a little extra history (start is rounded down to the UTC day)
// for much faster re-runs while tuning parameters. Callers must not mutate the result.
func fetchCached(ctx context.Context, src KlineSource, symbol, interval string, start, end time.Time) ([]exchange.Kline, error) {
	start = start.Truncate(24 * time.Hour)
	key := symbol + "|" + interval + "|" + strconv.FormatInt(start.Unix(), 10)
	cacheMu.Lock()
	if e, ok := cache[key]; ok && time.Since(e.at) < cacheTTL {
		cacheMu.Unlock()
		return e.ks, nil
	}
	cacheMu.Unlock()

	ks, err := fetchRange(ctx, src, symbol, interval, start, end)
	if err != nil {
		return nil, err
	}
	cacheMu.Lock()
	if len(cache) > 12 {
		cache = map[string]cacheEntry{}
	}
	cache[key] = cacheEntry{at: time.Now(), ks: ks}
	cacheMu.Unlock()
	return ks, nil
}

// Analyze fetches 4h/1h/15m history for the last `days` days and runs the model.
func Analyze(ctx context.Context, src KlineSource, symbol string, days int, p Params) (*Result, error) {
	days = clampI(days, 3, 365)
	now := time.Now()
	start := now.AddDate(0, 0, -days)
	warm := 20 * 24 * time.Hour

	h4, err := fetchCached(ctx, src, symbol, "4h", start.Add(-warm), now)
	if err != nil {
		return nil, fmt.Errorf("fetch 4h: %w", err)
	}
	h1, err := fetchCached(ctx, src, symbol, "1h", start.Add(-warm), now)
	if err != nil {
		return nil, fmt.Errorf("fetch 1h: %w", err)
	}
	m15, err := fetchCached(ctx, src, symbol, "15m", start, now)
	if err != nil {
		return nil, fmt.Errorf("fetch 15m: %w", err)
	}
	if len(h1) < 100 || len(m15) < 100 {
		return nil, fmt.Errorf("K线数据不足: 1h=%d 15m=%d", len(h1), len(m15))
	}
	res := Run(market{h4: h4, h1: h1, m15: m15}, start, p)
	res.Symbol, res.Days = symbol, days
	return res, nil
}
