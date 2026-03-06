package trend

import (
	"context"
	"testing"
	"time"

	"github.com/jayce/btc-trader/internal/exchange"
	"github.com/jayce/btc-trader/internal/strategy"
)

func TestEMACrossStrategy_Name(t *testing.T) {
	s := NewEMACrossStrategy()
	if s.Name() != "ema_crossover" {
		t.Errorf("Name() = %s, want ema_crossover", s.Name())
	}
}

func TestEMACrossStrategy_Init(t *testing.T) {
	s := NewEMACrossStrategy()
	err := s.Init(map[string]interface{}{
		"fast_period":    8,
		"slow_period":    21,
		"rsi_filter":     false,
		"rsi_overbought": 80.0,
	})
	if err != nil {
		t.Fatalf("Init error: %v", err)
	}
	if s.fastPeriod != 8 {
		t.Errorf("fastPeriod = %d, want 8", s.fastPeriod)
	}
	if s.slowPeriod != 21 {
		t.Errorf("slowPeriod = %d, want 21", s.slowPeriod)
	}
	if s.rsiFilter != false {
		t.Error("rsiFilter should be false")
	}
}

func TestEMACrossStrategy_RequiredIndicators(t *testing.T) {
	s := NewEMACrossStrategy()
	reqs := s.RequiredIndicators()

	// Should require at least 2 EMAs + RSI
	if len(reqs) < 2 {
		t.Errorf("RequiredIndicators: got %d, want at least 2", len(reqs))
	}

	hasRSI := false
	emaCount := 0
	for _, r := range reqs {
		if r.Name == "RSI" {
			hasRSI = true
		}
		if r.Name == "EMA" {
			emaCount++
		}
	}
	if emaCount != 2 {
		t.Errorf("Expected 2 EMA requirements, got %d", emaCount)
	}
	if !hasRSI {
		t.Error("Expected RSI requirement when rsiFilter is true")
	}
}

func TestEMACrossStrategy_Evaluate_Hold(t *testing.T) {
	s := NewEMACrossStrategy()
	s.Init(map[string]interface{}{
		"fast_period": 12,
		"slow_period": 26,
		"rsi_filter":  false,
	})

	ctx := context.Background()

	// First call: initializes prev values, returns Hold
	snap := &strategy.MarketSnapshot{
		Symbol: "BTCUSDT",
		Indicators: strategy.IndicatorSet{
			EMA: map[int]float64{12: 105.0, 26: 103.0},
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

	// Second call with same relative positions: no crossover = Hold
	snap2 := &strategy.MarketSnapshot{
		Symbol: "BTCUSDT",
		Indicators: strategy.IndicatorSet{
			EMA: map[int]float64{12: 106.0, 26: 104.0},
		},
		Timestamp: time.Now(),
	}
	sig2, err := s.Evaluate(ctx, snap2)
	if err != nil {
		t.Fatalf("Evaluate error: %v", err)
	}
	if sig2.Action != strategy.Hold {
		t.Errorf("No crossover should be Hold, got %s", sig2.Action)
	}
}

func TestEMACrossStrategy_Evaluate_GoldenCross(t *testing.T) {
	s := NewEMACrossStrategy()
	s.Init(map[string]interface{}{
		"fast_period": 12,
		"slow_period": 26,
		"rsi_filter":  false,
	})

	ctx := context.Background()

	// State 1: fast below slow
	snap1 := &strategy.MarketSnapshot{
		Symbol: "BTCUSDT",
		Indicators: strategy.IndicatorSet{
			EMA: map[int]float64{12: 99.0, 26: 100.0},
		},
		Timestamp: time.Now(),
	}
	s.Evaluate(ctx, snap1) // Initialize

	// State 2: fast crosses above slow → Golden Cross → BUY
	snap2 := &strategy.MarketSnapshot{
		Symbol: "BTCUSDT",
		Indicators: strategy.IndicatorSet{
			EMA: map[int]float64{12: 101.0, 26: 100.0},
		},
		Timestamp: time.Now(),
	}
	sig, err := s.Evaluate(ctx, snap2)
	if err != nil {
		t.Fatalf("Evaluate error: %v", err)
	}
	if sig.Action != strategy.Buy {
		t.Errorf("Golden cross should trigger Buy, got %s", sig.Action)
	}
	if sig.Strength <= 0 {
		t.Errorf("Signal strength should be > 0, got %.4f", sig.Strength)
	}
	if sig.Reason == "" {
		t.Error("Signal reason should not be empty")
	}
}

func TestEMACrossStrategy_Evaluate_DeathCross(t *testing.T) {
	s := NewEMACrossStrategy()
	s.Init(map[string]interface{}{
		"fast_period": 12,
		"slow_period": 26,
		"rsi_filter":  false,
	})

	ctx := context.Background()

	// State 1: fast above slow
	snap1 := &strategy.MarketSnapshot{
		Symbol: "BTCUSDT",
		Indicators: strategy.IndicatorSet{
			EMA: map[int]float64{12: 101.0, 26: 100.0},
		},
		Timestamp: time.Now(),
	}
	s.Evaluate(ctx, snap1)

	// State 2: fast crosses below slow → Death Cross → SELL
	snap2 := &strategy.MarketSnapshot{
		Symbol: "BTCUSDT",
		Indicators: strategy.IndicatorSet{
			EMA: map[int]float64{12: 99.0, 26: 100.0},
		},
		Timestamp: time.Now(),
	}
	sig, err := s.Evaluate(ctx, snap2)
	if err != nil {
		t.Fatalf("Evaluate error: %v", err)
	}
	if sig.Action != strategy.Sell {
		t.Errorf("Death cross should trigger Sell, got %s", sig.Action)
	}
}

func TestEMACrossStrategy_RSIFilter(t *testing.T) {
	s := NewEMACrossStrategy()
	s.Init(map[string]interface{}{
		"fast_period":    12,
		"slow_period":    26,
		"rsi_filter":     true,
		"rsi_period":     14,
		"rsi_overbought": 70.0,
	})

	ctx := context.Background()

	// State 1: fast below slow
	snap1 := &strategy.MarketSnapshot{
		Symbol: "BTCUSDT",
		Indicators: strategy.IndicatorSet{
			EMA: map[int]float64{12: 99.0, 26: 100.0},
			RSI: map[int]float64{14: 50.0},
		},
		Timestamp: time.Now(),
	}
	s.Evaluate(ctx, snap1)

	// Golden cross but RSI is overbought → should NOT buy
	snap2 := &strategy.MarketSnapshot{
		Symbol: "BTCUSDT",
		Indicators: strategy.IndicatorSet{
			EMA: map[int]float64{12: 101.0, 26: 100.0},
			RSI: map[int]float64{14: 75.0}, // Overbought
		},
		Timestamp: time.Now(),
	}
	sig, err := s.Evaluate(ctx, snap2)
	if err != nil {
		t.Fatalf("Evaluate error: %v", err)
	}
	if sig.Action != strategy.Hold {
		t.Errorf("RSI overbought should suppress Buy, got %s", sig.Action)
	}
}

func TestEMACrossStrategy_OnTradeExecuted(t *testing.T) {
	s := NewEMACrossStrategy()
	// Should not panic
	s.OnTradeExecuted(&exchange.Trade{
		Symbol:   "BTCUSDT",
		Side:     exchange.OrderSideBuy,
		Price:    50000,
		Quantity: 0.01,
	})
}
