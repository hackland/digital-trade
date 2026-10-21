package ict

import (
	"math"

	"github.com/jayce/btc-trader/internal/exchange"
)

type dayRange struct{ high, low float64 }

// atrSeries returns Wilder ATR(n) per bar; entries before index n are 0.
func atrSeries(ks []exchange.Kline, n int) []float64 {
	out := make([]float64, len(ks))
	if len(ks) <= n {
		return out
	}
	tr := func(i int) float64 {
		h, l, pc := ks[i].High, ks[i].Low, ks[i-1].Close
		return math.Max(h-l, math.Max(math.Abs(h-pc), math.Abs(l-pc)))
	}
	sum := 0.0
	for i := 1; i <= n; i++ {
		sum += tr(i)
	}
	out[n] = sum / float64(n)
	for i := n + 1; i < len(ks); i++ {
		out[i] = (out[i-1]*float64(n-1) + tr(i)) / float64(n)
	}
	return out
}

// swingFlags marks fractal swing points. A swing at j depends only on bars
// j-n..j+n, so it is knowable (non-repainting) from bar j+n onward.
// Equal neighbours resolve to the last bar of the plateau.
func swingFlags(ks []exchange.Kline, n int) (hi, lo []bool) {
	hi = make([]bool, len(ks))
	lo = make([]bool, len(ks))
	for j := n; j+n < len(ks); j++ {
		isH, isL := true, true
		for d := 1; d <= n; d++ {
			if ks[j-d].High > ks[j].High || ks[j+d].High >= ks[j].High {
				isH = false
			}
			if ks[j-d].Low < ks[j].Low || ks[j+d].Low <= ks[j].Low {
				isL = false
			}
		}
		hi[j], lo[j] = isH, isL
	}
	return hi, lo
}

// trendSeries tracks market structure: after each bar it is +1 once a close has
// broken the latest confirmed swing high, -1 once a close has broken the latest
// confirmed swing low, 0 before any break.
func trendSeries(ks []exchange.Kline, n int) []int8 {
	out := make([]int8, len(ks))
	if len(ks) == 0 {
		return out
	}
	hi, lo := swingFlags(ks, n)
	var refH, refL float64
	var haveH, haveL bool
	var tr int8
	for i := range ks {
		if j := i - n; j >= 0 {
			if hi[j] {
				refH, haveH = ks[j].High, true
			}
			if lo[j] {
				refL, haveL = ks[j].Low, true
			}
		}
		if haveH && ks[i].Close > refH {
			tr, haveH = 1, false
		}
		if haveL && ks[i].Close < refL {
			tr, haveL = -1, false
		}
		out[i] = tr
	}
	return out
}

// dayRanges aggregates bars into UTC-day high/low, keyed by unix-day number.
func dayRanges(ks []exchange.Kline) map[int64]dayRange {
	out := make(map[int64]dayRange)
	for _, k := range ks {
		d := k.OpenTime.Unix() / 86400
		r, ok := out[d]
		if !ok {
			out[d] = dayRange{high: k.High, low: k.Low}
			continue
		}
		r.high = math.Max(r.high, k.High)
		r.low = math.Min(r.low, k.Low)
		out[d] = r
	}
	return out
}

// flipKlines mirrors prices (p -> -p) so the long-side detector finds shorts.
func flipKlines(ks []exchange.Kline) []exchange.Kline {
	out := make([]exchange.Kline, len(ks))
	for i, k := range ks {
		k.Open, k.Close = -k.Open, -k.Close
		k.High, k.Low = -k.Low, -k.High
		out[i] = k
	}
	return out
}

func trendName(t int8) string {
	switch {
	case t > 0:
		return "up"
	case t < 0:
		return "down"
	default:
		return "none"
	}
}
