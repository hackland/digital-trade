// tradedump3: directly compares indicator values computed from a 71-bar window
// (what the backtest engine uses, via klines[i-histSize:i+1]) vs a 140-bar
// window (what the live trader.go loop actually keeps, historySize*2), both
// ending at the exact same final bar, to check whether window length alone
// explains the composite score gap between backtest and live for the same
// candle. One-off diagnostic, not wired into the Makefile.
// Run with `go run ./cmd/tradedump3`.
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/jayce/btc-trader/internal/config"
	"github.com/jayce/btc-trader/internal/market"
	"github.com/jayce/btc-trader/internal/storage/timescale"
	"github.com/jayce/btc-trader/internal/strategy"
	"go.uber.org/zap"
)

func main() {
	cfg, err := config.Load("")
	if err != nil {
		fmt.Fprintf(os.Stderr, "load config: %v\n", err)
		os.Exit(1)
	}
	ctx := context.Background()
	store, err := timescale.New(ctx, cfg.Database, zap.NewNop())
	if err != nil {
		fmt.Fprintf(os.Stderr, "connect db: %v\n", err)
		os.Exit(1)
	}
	defer store.Close()

	symbol := "BTCUSDT"
	end := time.Now().UTC()
	start := end.Add(-30 * 24 * time.Hour)

	klines, err := store.GetKlines(ctx, symbol, "1h", start, end, 0)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load klines: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("loaded %d bars, last bar time=%s close=%.2f\n", len(klines), klines[len(klines)-1].OpenTime, klines[len(klines)-1].Close)

	// Trim to the bar that CLOSED at 21:00 (open_time hour=20) — the one live's
	// 20:59:59 buy decision was actually based on — dropping the still-forming
	// 21:00-22:00 bar that was tacked on by the time this query ran.
	for len(klines) > 0 && klines[len(klines)-1].OpenTime.Hour() != 20 {
		klines = klines[:len(klines)-1]
	}
	fmt.Printf("trimmed to last bar time=%s close=%.2f\n\n", klines[len(klines)-1].OpenTime, klines[len(klines)-1].Close)

	ic := market.NewIndicatorComputer()
	reqs := []strategy.IndicatorRequirement{
		{Name: "MACD", Params: map[string]int{"fast": 12, "slow": 26, "signal": 9}},
		{Name: "EMA", Params: map[string]int{"period": 9}},
		{Name: "EMA", Params: map[string]int{"period": 21}},
		{Name: "MFI", Params: map[string]int{"period": 14}},
	}

	for _, winSize := range []int{71, 140, 200, len(klines)} {
		if winSize > len(klines) {
			winSize = len(klines)
		}
		window := klines[len(klines)-winSize:]
		ind := ic.ComputeAll(window, reqs)
		fmt.Printf("window=%4d bars (from %s) -> MACD.Hist=%.4f EMA9=%.2f EMA21=%.2f MFI14=%.2f\n",
			winSize, window[0].OpenTime.Format("01-02 15:04"), ind.MACD.Histogram, ind.EMA[9], ind.EMA[21], ind.MFI[14])
	}
}
