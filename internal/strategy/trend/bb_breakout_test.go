package trend

import (
	"context"
	"testing"
	"time"

	"github.com/jayce/btc-trader/internal/exchange"
	"github.com/jayce/btc-trader/internal/strategy"
)

func makeKlines(closes []float64, volumes []float64) []exchange.Kline {
	klines := make([]exchange.Kline, len(closes))
	for i, c := range closes {
		vol := 100.0
		if i < len(volumes) {
			vol = volumes[i]
		}
		klines[i] = exchange.Kline{
			Symbol:   "BTCUSDT",
			Interval: "5m",
			Close:    c,
			Open:     c,
			High:     c + 10,
			Low:      c - 10,
			Volume:   vol,
			OpenTime: time.Now().Add(time.Duration(-len(closes)+i) * 5 * time.Minute),
		}
	}
	return klines
}

func TestBBBreakoutStrategy_Name(t *testing.T) {
	s := NewBBBreakoutStrategy()
	if s.Name() != "bb_breakout" {
		t.Errorf("Name() = %s, want bb_breakout", s.Name())
	}
}

func TestBBBreakoutStrategy_Init(t *testing.T) {
	s := NewBBBreakoutStrategy()
	err := s.Init(map[string]interface{}{
		"bb_period":     30,
		"bb_mult":       2.5,
		"volume_period": 25,
		"rsi_period":    10,
	})
	if err != nil {
		t.Fatalf("Init error: %v", err)
	}
	if s.bbPeriod != 30 {
		t.Errorf("bbPeriod = %d, want 30", s.bbPeriod)
	}
	if s.bbMult != 2.5 {
		t.Errorf("bbMult = %f, want 2.5", s.bbMult)
	}
	if s.volumePeriod != 25 {
		t.Errorf("volumePeriod = %d, want 25", s.volumePeriod)
	}
}

func TestBBBreakoutStrategy_RequiredIndicators(t *testing.T) {
	s := NewBBBreakoutStrategy()
	reqs := s.RequiredIndicators()

	if len(reqs) != 2 {
		t.Fatalf("RequiredIndicators: got %d, want 2", len(reqs))
	}

	hasBB, hasRSI := false, false
	for _, r := range reqs {
		if r.Name == "BB" {
			hasBB = true
		}
		if r.Name == "RSI" {
			hasRSI = true
		}
	}
	if !hasBB {
		t.Error("Expected BB requirement")
	}
	if !hasRSI {
		t.Error("Expected RSI requirement")
	}
}

func TestBBBreakoutStrategy_Evaluate_FirstCallHold(t *testing.T) {
	s := NewBBBreakoutStrategy()
	s.Init(map[string]interface{}{"volume_period": 5})
	ctx := context.Background()

	klines := makeKlines(
		[]float64{100, 101, 102, 103, 104, 105},
		[]float64{100, 100, 100, 100, 100, 150},
	)

	snap := &strategy.MarketSnapshot{
		Symbol: "BTCUSDT",
		Klines: klines,
		Indicators: strategy.IndicatorSet{
			BB:  strategy.BollingerBands{Upper: 110, Middle: 100, Lower: 90, Width: 0.2},
			RSI: map[int]float64{14: 55.0},
		},
		Timestamp: time.Now(),
	}
	sig, err := s.Evaluate(ctx, snap)
	if err != nil {
		t.Fatalf("Evaluate error: %v", err)
	}
	if sig.Action != strategy.Hold {
		t.Errorf("First call should be Hold, got %s", sig.Action)
	}
}

func TestBBBreakoutStrategy_Evaluate_BuyBreakout(t *testing.T) {
	s := NewBBBreakoutStrategy()
	s.Init(map[string]interface{}{"volume_period": 5})
	ctx := context.Background()

	// State 1: close below upper band
	klines1 := makeKlines(
		[]float64{100, 101, 102, 103, 104, 108},
		[]float64{100, 100, 100, 100, 100, 100},
	)
	snap1 := &strategy.MarketSnapshot{
		Symbol: "BTCUSDT",
		Klines: klines1,
		Indicators: strategy.IndicatorSet{
			BB:  strategy.BollingerBands{Upper: 110, Middle: 100, Lower: 90, Width: 0.2},
			RSI: map[int]float64{14: 55.0},
		},
		Timestamp: time.Now(),
	}
	s.Evaluate(ctx, snap1)

	// State 2: close breaks above upper band with high volume
	klines2 := makeKlines(
		[]float64{100, 101, 102, 103, 104, 112},
		[]float64{100, 100, 100, 100, 100, 200}, // last candle volume = 200, avg = 100
	)
	snap2 := &strategy.MarketSnapshot{
		Symbol: "BTCUSDT",
		Klines: klines2,
		Indicators: strategy.IndicatorSet{
			BB:  strategy.BollingerBands{Upper: 110, Middle: 100, Lower: 90, Width: 0.2},
			RSI: map[int]float64{14: 60.0},
		},
		Timestamp: time.Now(),
	}
	sig, err := s.Evaluate(ctx, snap2)
	if err != nil {
		t.Fatalf("Evaluate error: %v", err)
	}
	if sig.Action != strategy.Buy {
		t.Errorf("BB breakout should trigger Buy, got %s", sig.Action)
	}
	if sig.Strength <= 0 {
		t.Errorf("Signal strength should be > 0, got %.4f", sig.Strength)
	}
}

func TestBBBreakoutStrategy_Evaluate_SellBreakdown(t *testing.T) {
	s := NewBBBreakoutStrategy()
	s.Init(map[string]interface{}{"volume_period": 5})
	ctx := context.Background()

	// State 1: close above lower band
	klines1 := makeKlines(
		[]float64{100, 99, 98, 97, 96, 92},
		[]float64{100, 100, 100, 100, 100, 100},
	)
	snap1 := &strategy.MarketSnapshot{
		Symbol: "BTCUSDT",
		Klines: klines1,
		Indicators: strategy.IndicatorSet{
			BB:  strategy.BollingerBands{Upper: 110, Middle: 100, Lower: 90, Width: 0.2},
			RSI: map[int]float64{14: 40.0},
		},
		Timestamp: time.Now(),
	}
	s.Evaluate(ctx, snap1)

	// State 2: close drops below lower band
	klines2 := makeKlines(
		[]float64{100, 99, 98, 97, 96, 88},
		[]float64{100, 100, 100, 100, 100, 150},
	)
	snap2 := &strategy.MarketSnapshot{
		Symbol: "BTCUSDT",
		Klines: klines2,
		Indicators: strategy.IndicatorSet{
			BB:  strategy.BollingerBands{Upper: 110, Middle: 100, Lower: 90, Width: 0.2},
			RSI: map[int]float64{14: 30.0},
		},
		Timestamp: time.Now(),
	}
	sig, err := s.Evaluate(ctx, snap2)
	if err != nil {
		t.Fatalf("Evaluate error: %v", err)
	}
	if sig.Action != strategy.Sell {
		t.Errorf("BB breakdown should trigger Sell, got %s", sig.Action)
	}
}

func TestBBBreakoutStrategy_Evaluate_NoVolumeConfirmation(t *testing.T) {
	s := NewBBBreakoutStrategy()
	s.Init(map[string]interface{}{"volume_period": 5})
	ctx := context.Background()

	// State 1: close below upper band
	klines1 := makeKlines(
		[]float64{100, 101, 102, 103, 104, 108},
		[]float64{200, 200, 200, 200, 200, 200}, // high avg volume
	)
	snap1 := &strategy.MarketSnapshot{
		Symbol: "BTCUSDT",
		Klines: klines1,
		Indicators: strategy.IndicatorSet{
			BB:  strategy.BollingerBands{Upper: 110, Middle: 100, Lower: 90, Width: 0.2},
			RSI: map[int]float64{14: 55.0},
		},
		Timestamp: time.Now(),
	}
	s.Evaluate(ctx, snap1)

	// State 2: close breaks above upper band but volume is LOW (< avg)
	klines2 := makeKlines(
		[]float64{100, 101, 102, 103, 104, 112},
		[]float64{200, 200, 200, 200, 200, 50}, // last candle volume = 50 < avg = 200
	)
	snap2 := &strategy.MarketSnapshot{
		Symbol: "BTCUSDT",
		Klines: klines2,
		Indicators: strategy.IndicatorSet{
			BB:  strategy.BollingerBands{Upper: 110, Middle: 100, Lower: 90, Width: 0.2},
			RSI: map[int]float64{14: 60.0},
		},
		Timestamp: time.Now(),
	}
	sig, err := s.Evaluate(ctx, snap2)
	if err != nil {
		t.Fatalf("Evaluate error: %v", err)
	}
	if sig.Action != strategy.Hold {
		t.Errorf("Low volume should suppress Buy, got %s", sig.Action)
	}
}

func TestBBBreakoutStrategy_OnTradeExecuted(t *testing.T) {
	s := NewBBBreakoutStrategy()
	// Should not panic
	s.OnTradeExecuted(&exchange.Trade{
		Symbol:   "BTCUSDT",
		Side:     exchange.OrderSideBuy,
		Price:    50000,
		Quantity: 0.01,
	})
}
