package trend

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jayce/btc-trader/internal/exchange"
	"github.com/jayce/btc-trader/internal/strategy"
)

// TestCustomWeighted_SymbolStateIsolation guards the multi-symbol contamination
// bug: a single strategy instance serves both BTCUSDT and ETHUSDT, so per-symbol
// runtime state (entry price, etc.) must be keyed by symbol. Before the fix these
// were scalar fields and BTC's entry price leaked into ETH's state.
func TestCustomWeighted_SymbolStateIsolation(t *testing.T) {
	s := NewCustomWeightedStrategy()

	// BTC long entry at a BTC-scale price.
	s.OnTradeExecuted(&exchange.Trade{
		Symbol: "BTCUSDT", Side: exchange.OrderSideBuy, Price: 80204.05, Timestamp: time.Now(),
	})

	if btc := s.states["BTCUSDT"]; btc == nil || btc.entryPrice != 80204.05 {
		t.Fatalf("BTC entryPrice not recorded: %+v", btc)
	}
	// ETH must be untouched by the BTC trade.
	if eth, ok := s.states["ETHUSDT"]; ok && eth.entryPrice != 0 {
		t.Fatalf("ETH state polluted by BTC entry: entryPrice=%v", eth.entryPrice)
	}

	// ETH long entry at an ETH-scale price — must not clobber BTC.
	s.OnTradeExecuted(&exchange.Trade{
		Symbol: "ETHUSDT", Side: exchange.OrderSideBuy, Price: 2600, Timestamp: time.Now(),
	})
	if got := s.states["ETHUSDT"].entryPrice; got != 2600 {
		t.Fatalf("ETH entryPrice = %v, want 2600", got)
	}
	if got := s.states["BTCUSDT"].entryPrice; got != 80204.05 {
		t.Fatalf("BTC entryPrice clobbered by ETH trade: %v", got)
	}

	// Closing BTC must not clear ETH's position state.
	s.OnTradeExecuted(&exchange.Trade{
		Symbol: "BTCUSDT", Side: exchange.OrderSideSell, Price: 81000, Timestamp: time.Now(),
	})
	if got := s.states["BTCUSDT"].entryPrice; got != 0 {
		t.Fatalf("BTC entryPrice = %v after sell, want 0", got)
	}
	if got := s.states["ETHUSDT"].entryPrice; got != 2600 {
		t.Fatalf("ETH entryPrice = %v after BTC sell, want 2600 (state leaked)", got)
	}
}

// TestCustomWeighted_NoCrossSymbolHardStop reproduces the reported production
// symptom: after a BTC buy at ~80204, evaluating an in-profit ETH position at
// ~2510 must NOT trigger a hard stop citing BTC's entry price.
func TestCustomWeighted_NoCrossSymbolHardStop(t *testing.T) {
	s := NewCustomWeightedStrategy()
	s.hardStopPct = 1.5 // enable hard stop
	s.minHoldBars = 0   // skip min-hold gate so we reach the stop check
	ctx := context.Background()

	// BTC entry pollutes shared state in the buggy version.
	s.OnTradeExecuted(&exchange.Trade{
		Symbol: "BTCUSDT", Side: exchange.OrderSideBuy, Price: 80204.05, Timestamp: time.Now(),
	})

	// ETH: real entry 2500, current price 2510 (in profit → no legitimate stop).
	klines := makeKlines([]float64{2490, 2495, 2500, 2505, 2510}, nil)
	snap := &strategy.MarketSnapshot{
		Symbol:     "ETHUSDT",
		Klines:     klines,
		Indicators: strategy.IndicatorSet{ATR: map[int]float64{14: 20}},
		Position:   &strategy.PositionInfo{Quantity: 1.0, AvgEntryPrice: 2500},
		Timestamp:  time.Now(),
	}

	sig, err := s.Evaluate(ctx, snap)
	if err != nil {
		t.Fatalf("Evaluate error: %v", err)
	}
	if sig.Action == strategy.Sell {
		t.Fatalf("ETH wrongly sold: action=%s reason=%q", sig.Action, sig.Reason)
	}
	if strings.Contains(sig.Reason, "80204") || strings.Contains(sig.Reason, "Hard stop") {
		t.Fatalf("ETH evaluated against BTC entry price: reason=%q", sig.Reason)
	}
	// ETH's own state must hold the ETH entry, not BTC's.
	if got := s.states["ETHUSDT"].entryPrice; got != 2500 {
		t.Fatalf("ETH entryPrice = %v, want 2500 (restored from snapshot)", got)
	}
}
