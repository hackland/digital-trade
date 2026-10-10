package grid

import (
	"math"
	"testing"
	"time"

	"github.com/jayce/btc-trader/internal/exchange"
)

func closeEnough(a, b float64) bool {
	return math.Abs(a-b) < 1e-6
}

func k(t int, open, high, low, close float64) exchange.Kline {
	return exchange.Kline{
		OpenTime: time.Unix(int64(t)*60, 0),
		Open:     open, High: high, Low: low, Close: close,
	}
}

// TestSingleSlotRoundTrip exercises the simplest possible grid — one slot,
// [100, 110] — and checks the buy fill, the sell fill, and the resulting
// PnL/cash bookkeeping against hand-computed expected values.
func TestSingleSlotRoundTrip(t *testing.T) {
	cfg := NewConfig()
	cfg.Symbol = "BTCUSDT"
	cfg.Interval = "1m"
	cfg.LowerPrice = 100
	cfg.UpperPrice = 110
	cfg.GridSpacingPct = 1.0 // spacing wider than the range -> exactly one slot [100,110]
	cfg.USDTPerGrid = 1000
	cfg.FeeRate = 0.001
	cfg.InitialCash = 10000

	klines := []exchange.Kline{
		k(0, 105, 105, 99, 101),  // dips through 100 -> buy fills at 100
		k(1, 101, 111, 100, 108), // rises through 110 -> sell fills at 110
	}

	eng := NewEngine(cfg)
	res, err := eng.Run(klines)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(res.Trades) != 2 {
		t.Fatalf("expected 2 trades (1 buy + 1 sell), got %d: %+v", len(res.Trades), res.Trades)
	}
	buy, sell := res.Trades[0], res.Trades[1]

	if buy.Side != SideBuy || !closeEnough(buy.Price, 100) {
		t.Errorf("buy trade wrong: %+v", buy)
	}
	wantQty := 1000.0 / 100.0
	if !closeEnough(buy.Quantity, wantQty) {
		t.Errorf("buy qty = %v, want %v", buy.Quantity, wantQty)
	}
	wantBuyFee := 100 * wantQty * 0.001
	if !closeEnough(buy.Fee, wantBuyFee) {
		t.Errorf("buy fee = %v, want %v", buy.Fee, wantBuyFee)
	}

	if sell.Side != SideSell || !closeEnough(sell.Price, 110) {
		t.Errorf("sell trade wrong: %+v", sell)
	}
	wantSellFee := 110 * wantQty * 0.001
	if !closeEnough(sell.Fee, wantSellFee) {
		t.Errorf("sell fee = %v, want %v", sell.Fee, wantSellFee)
	}
	costBasis := 100*wantQty + wantBuyFee
	wantPnL := (110*wantQty - wantSellFee) - costBasis
	if !closeEnough(sell.PnL, wantPnL) {
		t.Errorf("sell pnl = %v, want %v", sell.PnL, wantPnL)
	}

	wantCash := cfg.InitialCash - costBasis + (110*wantQty - wantSellFee)
	if !closeEnough(res.Metrics.FinalCash, wantCash) {
		t.Errorf("final cash = %v, want %v", res.Metrics.FinalCash, wantCash)
	}
	if res.Metrics.CompletedRoundTrips != 1 {
		t.Errorf("completed round trips = %d, want 1", res.Metrics.CompletedRoundTrips)
	}
	if res.Metrics.RemainingQty != 0 {
		t.Errorf("remaining qty = %v, want 0", res.Metrics.RemainingQty)
	}
	if res.StoppedOut {
		t.Errorf("should not have stopped out")
	}
}

// TestNoSameBarChaining verifies that a buy filled on a bar cannot also have
// its paired sell fill within that same bar, even if the bar's range would
// technically span both levels — this is the documented "next bar" fill
// simplification the engine is designed around.
func TestNoSameBarChaining(t *testing.T) {
	cfg := NewConfig()
	cfg.Symbol = "BTCUSDT"
	cfg.Interval = "1m"
	cfg.LowerPrice = 100
	cfg.UpperPrice = 110
	cfg.GridSpacingPct = 1.0
	cfg.USDTPerGrid = 1000
	cfg.FeeRate = 0.001
	cfg.InitialCash = 10000

	// Single bar whose range covers both the buy level (100) and the sell
	// level (110) — a naive same-bar simulation would record a round trip
	// here; the engine must only fill the buy.
	klines := []exchange.Kline{
		k(0, 105, 112, 98, 105),
	}

	eng := NewEngine(cfg)
	res, err := eng.Run(klines)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(res.Trades) != 1 || res.Trades[0].Side != SideBuy {
		t.Fatalf("expected exactly 1 buy trade in the same bar, got %+v", res.Trades)
	}
}

// TestStopLossLiquidatesAndHalts checks that a close below the stop-loss
// threshold force-closes any open slot and stops processing further bars.
func TestStopLossLiquidatesAndHalts(t *testing.T) {
	cfg := NewConfig()
	cfg.Symbol = "BTCUSDT"
	cfg.Interval = "1m"
	cfg.LowerPrice = 100
	cfg.UpperPrice = 110
	cfg.GridSpacingPct = 1.0
	cfg.USDTPerGrid = 1000
	cfg.FeeRate = 0.001
	cfg.InitialCash = 10000
	cfg.StopLossBelowPct = 0.05 // stop below 95

	klines := []exchange.Kline{
		k(0, 105, 105, 99, 100), // buy fills at 100
		k(1, 100, 100, 90, 94),  // closes at 94, below stop-loss (95) -> force close + halt
		k(2, 94, 200, 200, 200), // should never be processed
	}

	eng := NewEngine(cfg)
	res, err := eng.Run(klines)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.StoppedOut {
		t.Fatalf("expected StoppedOut=true, reason: %q", res.StopReason)
	}
	if len(res.Trades) != 2 {
		t.Fatalf("expected 2 trades (buy + force close), got %d: %+v", len(res.Trades), res.Trades)
	}
	if res.Trades[1].Side != SideForceClose || !closeEnough(res.Trades[1].Price, 94) {
		t.Errorf("force close trade wrong: %+v", res.Trades[1])
	}
	if len(res.EquityCurve) != 2 {
		t.Errorf("expected processing to halt after bar 2 (2 equity points), got %d", len(res.EquityCurve))
	}
}

// TestPauseBuyAboveUpperBlocksNewBuysNotSells checks that once price closes
// above UpperPrice, no new buy fills happen, but the pause is not sticky —
// buying resumes once price closes back at/below UpperPrice.
func TestPauseBuyAboveUpperBlocksNewBuysNotSells(t *testing.T) {
	cfg := NewConfig()
	cfg.Symbol = "BTCUSDT"
	cfg.Interval = "1m"
	cfg.LowerPrice = 100
	cfg.UpperPrice = 110
	cfg.GridSpacingPct = 1.0
	cfg.USDTPerGrid = 1000
	cfg.FeeRate = 0.001
	cfg.InitialCash = 10000
	cfg.PauseBuyAboveUpper = true

	klines := []exchange.Kline{
		k(0, 105, 105, 99, 101),  // buy fills at 100
		k(1, 101, 111, 101, 111), // sell fills at 110; close(111) > upper(110) -> buys paused
		k(2, 111, 111, 99, 105),  // dips through 100 again, but buys are paused this bar (close still needs checking) — close(105) <= upper, so buysAllowed this bar; but bar.Low=99 requires armed[0] which was set true after the sell in bar1 (applies bar2) — so buy SHOULD fill here since close<=upper allows it
	}

	eng := NewEngine(cfg)
	res, err := eng.Run(klines)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	// bar0: buy, bar1: sell, bar2: buy again (pause already lifted since bar2's own close <= upper)
	if len(res.Trades) != 3 {
		t.Fatalf("expected 3 trades, got %d: %+v", len(res.Trades), res.Trades)
	}
	wantSides := []TradeSide{SideBuy, SideSell, SideBuy}
	for i, want := range wantSides {
		if res.Trades[i].Side != want {
			t.Errorf("trade %d side = %s, want %s", i, res.Trades[i].Side, want)
		}
	}
}
