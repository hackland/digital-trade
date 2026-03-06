package trend

import (
	"context"
	"testing"
	"time"

	"github.com/jayce/btc-trader/internal/exchange"
	"github.com/jayce/btc-trader/internal/strategy"
)

func TestMACDRSIStrategy_Name(t *testing.T) {
	s := NewMACDRSIStrategy()
	if s.Name() != "macd_rsi" {
		t.Errorf("Name() = %s, want macd_rsi", s.Name())
	}
}

func TestMACDRSIStrategy_Init(t *testing.T) {
	s := NewMACDRSIStrategy()
	err := s.Init(map[string]interface{}{
		"fast_period":    10,
		"slow_period":    20,
		"signal_period":  7,
		"rsi_period":     10,
		"rsi_overbought": 80.0,
		"rsi_oversold":   20.0,
	})
	if err != nil {
		t.Fatalf("Init error: %v", err)
	}
	if s.fastPeriod != 10 {
		t.Errorf("fastPeriod = %d, want 10", s.fastPeriod)
	}
	if s.slowPeriod != 20 {
		t.Errorf("slowPeriod = %d, want 20", s.slowPeriod)
	}
	if s.signalPeriod != 7 {
		t.Errorf("signalPeriod = %d, want 7", s.signalPeriod)
	}
	if s.rsiPeriod != 10 {
		t.Errorf("rsiPeriod = %d, want 10", s.rsiPeriod)
	}
}

func TestMACDRSIStrategy_RequiredIndicators(t *testing.T) {
	s := NewMACDRSIStrategy()
	reqs := s.RequiredIndicators()

	if len(reqs) != 2 {
		t.Fatalf("RequiredIndicators: got %d, want 2", len(reqs))
	}

	hasMACD, hasRSI := false, false
	for _, r := range reqs {
		if r.Name == "MACD" {
			hasMACD = true
		}
		if r.Name == "RSI" {
			hasRSI = true
		}
	}
	if !hasMACD {
		t.Error("Expected MACD requirement")
	}
	if !hasRSI {
		t.Error("Expected RSI requirement")
	}
}

func TestMACDRSIStrategy_Evaluate_FirstCallHold(t *testing.T) {
	s := NewMACDRSIStrategy()
	s.Init(nil)
	ctx := context.Background()

	snap := &strategy.MarketSnapshot{
		Symbol: "BTCUSDT",
		Indicators: strategy.IndicatorSet{
			MACD: strategy.MACDValue{MACD: 10, Signal: 8, Histogram: 2},
			RSI:  map[int]float64{14: 55.0},
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

func TestMACDRSIStrategy_Evaluate_BullishCrossover(t *testing.T) {
	s := NewMACDRSIStrategy()
	s.Init(nil)
	ctx := context.Background()

	// State 1: histogram is negative
	snap1 := &strategy.MarketSnapshot{
		Symbol: "BTCUSDT",
		Indicators: strategy.IndicatorSet{
			MACD: strategy.MACDValue{MACD: -2, Signal: -1, Histogram: -1},
			RSI:  map[int]float64{14: 45.0},
		},
		Timestamp: time.Now(),
	}
	s.Evaluate(ctx, snap1) // initialize

	// State 2: histogram crosses to positive → BUY
	snap2 := &strategy.MarketSnapshot{
		Symbol: "BTCUSDT",
		Indicators: strategy.IndicatorSet{
			MACD: strategy.MACDValue{MACD: 1, Signal: 0.5, Histogram: 0.5},
			RSI:  map[int]float64{14: 50.0},
		},
		Timestamp: time.Now(),
	}
	sig, err := s.Evaluate(ctx, snap2)
	if err != nil {
		t.Fatalf("Evaluate error: %v", err)
	}
	if sig.Action != strategy.Buy {
		t.Errorf("Bullish crossover should trigger Buy, got %s", sig.Action)
	}
	if sig.Strength <= 0 {
		t.Errorf("Signal strength should be > 0, got %.4f", sig.Strength)
	}
	if sig.Reason == "" {
		t.Error("Signal reason should not be empty")
	}
}

func TestMACDRSIStrategy_Evaluate_BearishCrossover(t *testing.T) {
	s := NewMACDRSIStrategy()
	s.Init(nil)
	ctx := context.Background()

	// State 1: histogram is positive
	snap1 := &strategy.MarketSnapshot{
		Symbol: "BTCUSDT",
		Indicators: strategy.IndicatorSet{
			MACD: strategy.MACDValue{MACD: 2, Signal: 1, Histogram: 1},
			RSI:  map[int]float64{14: 55.0},
		},
		Timestamp: time.Now(),
	}
	s.Evaluate(ctx, snap1)

	// State 2: histogram crosses to negative → SELL
	snap2 := &strategy.MarketSnapshot{
		Symbol: "BTCUSDT",
		Indicators: strategy.IndicatorSet{
			MACD: strategy.MACDValue{MACD: -1, Signal: -0.5, Histogram: -0.5},
			RSI:  map[int]float64{14: 50.0},
		},
		Timestamp: time.Now(),
	}
	sig, err := s.Evaluate(ctx, snap2)
	if err != nil {
		t.Fatalf("Evaluate error: %v", err)
	}
	if sig.Action != strategy.Sell {
		t.Errorf("Bearish crossover should trigger Sell, got %s", sig.Action)
	}
}

func TestMACDRSIStrategy_Evaluate_RSIOverboughtSuppressesBuy(t *testing.T) {
	s := NewMACDRSIStrategy()
	s.Init(nil)
	ctx := context.Background()

	// State 1: histogram negative
	snap1 := &strategy.MarketSnapshot{
		Symbol: "BTCUSDT",
		Indicators: strategy.IndicatorSet{
			MACD: strategy.MACDValue{MACD: -2, Signal: -1, Histogram: -1},
			RSI:  map[int]float64{14: 45.0},
		},
		Timestamp: time.Now(),
	}
	s.Evaluate(ctx, snap1)

	// State 2: bullish crossover but RSI is overbought → suppress
	snap2 := &strategy.MarketSnapshot{
		Symbol: "BTCUSDT",
		Indicators: strategy.IndicatorSet{
			MACD: strategy.MACDValue{MACD: 1, Signal: 0.5, Histogram: 0.5},
			RSI:  map[int]float64{14: 75.0}, // overbought
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

func TestMACDRSIStrategy_Evaluate_RSIOversoldSuppressesSell(t *testing.T) {
	s := NewMACDRSIStrategy()
	s.Init(nil)
	ctx := context.Background()

	// State 1: histogram positive
	snap1 := &strategy.MarketSnapshot{
		Symbol: "BTCUSDT",
		Indicators: strategy.IndicatorSet{
			MACD: strategy.MACDValue{MACD: 2, Signal: 1, Histogram: 1},
			RSI:  map[int]float64{14: 55.0},
		},
		Timestamp: time.Now(),
	}
	s.Evaluate(ctx, snap1)

	// State 2: bearish crossover but RSI is oversold → suppress
	snap2 := &strategy.MarketSnapshot{
		Symbol: "BTCUSDT",
		Indicators: strategy.IndicatorSet{
			MACD: strategy.MACDValue{MACD: -1, Signal: -0.5, Histogram: -0.5},
			RSI:  map[int]float64{14: 25.0}, // oversold
		},
		Timestamp: time.Now(),
	}
	sig, err := s.Evaluate(ctx, snap2)
	if err != nil {
		t.Fatalf("Evaluate error: %v", err)
	}
	if sig.Action != strategy.Hold {
		t.Errorf("RSI oversold should suppress Sell, got %s", sig.Action)
	}
}

func TestMACDRSIStrategy_Evaluate_NoCrossoverHold(t *testing.T) {
	s := NewMACDRSIStrategy()
	s.Init(nil)
	ctx := context.Background()

	// Both calls have positive histogram → no crossover → Hold
	snap1 := &strategy.MarketSnapshot{
		Symbol: "BTCUSDT",
		Indicators: strategy.IndicatorSet{
			MACD: strategy.MACDValue{MACD: 2, Signal: 1, Histogram: 1},
			RSI:  map[int]float64{14: 55.0},
		},
		Timestamp: time.Now(),
	}
	s.Evaluate(ctx, snap1)

	snap2 := &strategy.MarketSnapshot{
		Symbol: "BTCUSDT",
		Indicators: strategy.IndicatorSet{
			MACD: strategy.MACDValue{MACD: 3, Signal: 1.5, Histogram: 1.5},
			RSI:  map[int]float64{14: 55.0},
		},
		Timestamp: time.Now(),
	}
	sig, err := s.Evaluate(ctx, snap2)
	if err != nil {
		t.Fatalf("Evaluate error: %v", err)
	}
	if sig.Action != strategy.Hold {
		t.Errorf("No crossover should be Hold, got %s", sig.Action)
	}
}

func TestMACDRSIStrategy_OnTradeExecuted(t *testing.T) {
	s := NewMACDRSIStrategy()
	// Should not panic
	s.OnTradeExecuted(&exchange.Trade{
		Symbol:   "BTCUSDT",
		Side:     exchange.OrderSideBuy,
		Price:    50000,
		Quantity: 0.01,
	})
}
