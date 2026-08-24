// tradedump2: reproduces the dashboard "Run Backtest" handler's HTF-loading
// behavior (old buggy vs new buffered) for a 30-day window, to verify the fix
// in internal/web/handler/backtest.go actually resolves htf_dist showing 0%
// for early trades in short backtest windows. One-off diagnostic, not wired
// into the Makefile. Run with `go run ./cmd/tradedump2`.
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/jayce/btc-trader/internal/backtest"
	"github.com/jayce/btc-trader/internal/config"
	"github.com/jayce/btc-trader/internal/storage/timescale"
	"github.com/jayce/btc-trader/internal/strategy/trend"
	"go.uber.org/zap"
)

func mod(name string, weight float64) map[string]interface{} {
	return map[string]interface{}{"name": name, "weight": weight}
}

func runOnce(ctx context.Context, store *timescale.Store, logger *zap.Logger, symbol string, start, end time.Time, htfStart time.Time, label string) {
	strat := trend.NewCustomWeightedStrategy()
	initCfg := map[string]interface{}{
		"modules":        []interface{}{mod("ema_cross", 0.4), mod("macd", 0.4), mod("mfi", 0.2)},
		"buy_threshold":  0.20,
		"sell_threshold": -0.25,
		"confirm_bars":   1,
		"cooldown_bars":  12,
		"min_hold_bars":  18,
		"atr_stop_mult":  3.0,
		"ema_cross_min":  0.15,
		"trend_filter":   false,
		"htf_enabled":    true,
		"htf_interval":   "1d",
		"htf_period":     10,
	}
	if err := strat.Init(initCfg); err != nil {
		fmt.Fprintf(os.Stderr, "init: %v\n", err)
		os.Exit(1)
	}

	klines, err := store.GetKlines(ctx, symbol, "1h", start, end, 0)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load klines: %v\n", err)
		os.Exit(1)
	}
	htfKlines, err := store.GetKlines(ctx, symbol, "1d", htfStart, end, 0)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load htf klines: %v\n", err)
		os.Exit(1)
	}

	engineCfg := backtest.EngineConfig{
		Symbol: symbol, Interval: "1h", InitialCash: 10000, FeeRate: 0.001, AllocPct: 0.1,
		HTFKlines: htfKlines, HTFInterval: "1d",
		HTFIndReqs:  strat.HTFIndicatorRequirements(),
		HTFHistSize: strat.HTFHistoryRequired(),
	}
	engine := backtest.NewEngine(engineCfg, strat, logger)
	result, err := engine.Run(ctx, klines)
	if err != nil {
		fmt.Fprintf(os.Stderr, "run: %v\n", err)
		os.Exit(1)
	}

	zeroCount, nonZeroCount, buyCount := 0, 0, 0
	for _, t := range result.Trades {
		if t.Side != "BUY" {
			continue
		}
		buyCount++
		if len(t.Reason) > 0 {
			if containsZeroPct(t.Reason) {
				zeroCount++
			} else {
				nonZeroCount++
			}
		}
	}
	fmt.Printf("=== %s === htf klines loaded: %d (from %s) | buys: %d, htf=0.0%%: %d, htf!=0: %d\n",
		label, len(htfKlines), htfStart.Format("2006-01-02"), buyCount, zeroCount, nonZeroCount)
}

func containsZeroPct(s string) bool {
	return len(s) > 0 && (contains(s, "(0.0%)") || contains(s, "(-0.0%)"))
}
func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func main() {
	logger := zap.NewNop()
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

	// OLD (buggy) behavior: htf query uses the same start as the main window.
	runOnce(ctx, store, logger, symbol, start, end, start, "OLD (no buffer, matches dashboard bug)")

	// NEW (fixed) behavior: htf query buffered back by HTFHistSize*2+5 daily bars.
	htfHistSize := 20 // period(10)+10
	htfStart := start.Add(-time.Duration(htfHistSize*2+5) * 24 * time.Hour)
	runOnce(ctx, store, logger, symbol, start, end, htfStart, "NEW (buffered, matches fix)")
}
