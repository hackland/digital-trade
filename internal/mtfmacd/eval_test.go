package mtfmacd

import (
	"math"
	"testing"
	"time"

	"github.com/jayce/btc-trader/internal/exchange"
	"github.com/jayce/btc-trader/internal/market"
)

func mkKlines(closes []float64, step time.Duration) []exchange.Kline {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if step == 15*time.Minute {
		t0 = t0.Add(700 * time.Hour) // 15m window sits late inside the 4h/1h history
	}
	out := make([]exchange.Kline, len(closes))
	for i, c := range closes {
		out[i] = exchange.Kline{OpenTime: t0.Add(time.Duration(i) * step), CloseTime: t0.Add(time.Duration(i+1)*step - time.Millisecond), Close: c, High: c, Low: c}
	}
	return out
}

func TestSeriesMatchesComputeMACD(t *testing.T) {
	closes := make([]float64, 200)
	for i := range closes {
		closes[i] = 100 + 10*math.Sin(float64(i)/7) + float64(i)*0.1
	}
	ic := market.NewIndicatorComputer()
	one := ic.ComputeMACD(closes, 12, 26, 9)
	s := ic.ComputeMACDSeries(closes, 12, 26, 9)
	n := len(closes) - 1
	if math.Abs(s.MACD[n]-one.MACD) > 1e-9 || math.Abs(s.Signal[n]-one.Signal) > 1e-6 {
		t.Fatalf("series %v/%v != single %v/%v", s.MACD[n], s.Signal[n], one.MACD, one.Signal)
	}
}

func TestEvaluateLongResonance(t *testing.T) {
	// Strong uptrend on 4h/1h, then a 15m dip and recovery.
	up := func(n int) []float64 {
		c := make([]float64, n)
		for i := range c {
			c[i] = 100 * math.Pow(1.01, float64(i)) // accelerating => DIF > DEA > 0
		}
		return c
	}
	m15 := up(200)
	for i := range m15 {
		m15[i] = 100 + float64(i) // gentle 15m uptrend
	}
	for i := 150; i < 175; i++ { // pullback
		m15[i] = m15[149] - float64(i-149)*0.8
	}
	for i := 175; i < 200; i++ { // recovery
		m15[i] = m15[174] + float64(i-174)*1.5
	}
	p := DefaultParams()
	s4 := BuildSeries("4h", mkKlines(up(200), 4*time.Hour), p)
	s1 := BuildSeries("1h", mkKlines(up(800), time.Hour), p)
	s15 := BuildSeries("15m", mkKlines(m15, 15*time.Minute), p)

	fired := 0
	h4, h1 := 0, 0
	for i := 1; i < len(s15.Klines); i++ {
		var ev Eval
		ev, h4, h1 = Evaluate("BTCUSDT", s4, s1, s15, i, h4, h1, p)
		if ev.Triggered {
			fired++
			if ev.Direction != DirLong || ev.Cross15m != TrendUp {
				t.Fatalf("unexpected %+v", ev)
			}
		}
	}
	if fired == 0 {
		t.Fatal("expected at least one long signal")
	}
}
