// tradedump: dump individual trades for a given custom_weighted config so we
// can see concretely which exits were hard-stops, how soon after entry, and
// what happened to price right after each buy. hard_stop_pct is read from
// argv[1] so the same config can be re-run with different values for
// comparison. One-off diagnostic, not wired into the Makefile.
// Run with `go run ./cmd/tradedump <hard_stop_pct>`.
package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
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

func main() {
	hardStopPct := 1.5
	if len(os.Args) > 1 {
		v, err := strconv.ParseFloat(os.Args[1], 64)
		if err == nil {
			hardStopPct = v
		}
	}

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
	mainInterval := "1h"
	end := time.Now().UTC()
	// Matches the dashboard's "180D" window preset.
	start := end.Add(-180 * 24 * time.Hour)

	klinesMain, err := store.GetKlines(ctx, symbol, mainInterval, start, end, 0)
	if err != nil || len(klinesMain) < 100 {
		fmt.Fprintf(os.Stderr, "load klines: %v\n", err)
		os.Exit(1)
	}
	// Drop the still-forming trailing bar (its close keeps moving) so the last
	// evaluated bar matches the one live's most recent CLOSED-candle decision
	// was based on, not whatever partial bar happened to be in the DB at fetch time.
	for len(klinesMain) > 0 && klinesMain[len(klinesMain)-1].OpenTime.Hour() != 20 {
		klinesMain = klinesMain[:len(klinesMain)-1]
	}
	klinesHTF, err := store.GetKlines(ctx, symbol, "1d", start.Add(-30*24*time.Hour), end, 0)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load htf: %v\n", err)
		os.Exit(1)
	}

	strat := trend.NewCustomWeightedStrategy()
	initCfg := map[string]interface{}{
		// Matches the dashboard screenshot: macd40+ema40+mfi20.
		"modules": []interface{}{
			mod("macd", 0.4), mod("ema_cross", 0.4), mod("mfi", 0.2),
		},
		"buy_threshold":           0.20,
		"sell_threshold":          -0.25,
		"confirm_bars":            1,
		"cooldown_bars":           12,
		"min_hold_bars":           18,
		"atr_stop_mult":           3.0,
		"atr_activate_profit_pct": 0.0,
		"hard_stop_pct":           hardStopPct,
		"ema_cross_min":           0.15,
		"trend_filter":            false,
		"trend_period":            50,
		"htf_enabled":             true,
		"htf_interval":            "1d",
		"htf_period":              10,
	}
	if err := strat.Init(initCfg); err != nil {
		fmt.Fprintf(os.Stderr, "init: %v\n", err)
		os.Exit(1)
	}

	engineCfg := backtest.EngineConfig{
		Symbol: symbol, Interval: mainInterval, InitialCash: 10000, FeeRate: 0.001, AllocPct: 1.0,
		HTFKlines: klinesHTF, HTFInterval: "1d",
		HTFIndReqs:  strat.HTFIndicatorRequirements(),
		HTFHistSize: strat.HTFHistoryRequired(),
	}
	engine := backtest.NewEngine(engineCfg, strat, logger)
	result, err := engine.Run(ctx, klinesMain)
	if err != nil {
		fmt.Fprintf(os.Stderr, "run: %v\n", err)
		os.Exit(1)
	}

	if diag := strat.GetDiagnostics(symbol); diag != nil {
		fmt.Printf("=== last evaluated bar diagnostics ===\n")
		fmt.Printf("timestamp=%s action=%s composite=%.4f buy_threshold=%.4f\n", diag.Timestamp, diag.Action, diag.CompositeScore, diag.BuyThreshold)
		fmt.Printf("has_position=%v cooldown_count=%d confirm_count=%d\n", diag.HasPosition, diag.CooldownCount, diag.ConfirmCount)
		fmt.Printf("htf_enabled=%v htf_bullish=%v htf_ema_dist_pct=%.4f\n", diag.HTFEnabled, diag.HTFBullish, diag.HTFEMADist)
		fmt.Printf("module_scores=%v\n", diag.ModuleScores)
		fmt.Printf("hold_reason=%s\n\n", diag.HoldReason)
	}

	fmt.Printf("=== hard_stop_pct=%.2f ===\n", hardStopPct)
	fmt.Printf("total_return_pct=%.4f max_dd_pct=%.4f trades=%d\n\n", result.Metrics.TotalReturnPct, result.Metrics.MaxDrawdownPct, result.Metrics.TotalTrades)

	fmt.Printf("%-17s %-4s %10s %8s %10s  %s\n", "time", "side", "price", "pnl", "", "reason")
	var buyTime time.Time
	var buyPrice float64
	for _, t := range result.Trades {
		if t.Side == "BUY" {
			buyTime = t.Timestamp
			buyPrice = t.Price
			fmt.Printf("%-17s %-4s %10.2f  %s\n", t.Timestamp.Format("2006-01-02 15:04"), t.Side, t.Price, t.Reason)
		} else {
			bars := int(t.Timestamp.Sub(buyTime).Hours())
			chgPct := (t.Price - buyPrice) / buyPrice * 100
			fmt.Printf("%-17s %-4s %10.2f %8.2f %6dh chg=%6.2f%%  %s\n",
				t.Timestamp.Format("2006-01-02 15:04"), t.Side, t.Price, t.PnL, bars, chgPct, t.Reason)
		}
	}
}
