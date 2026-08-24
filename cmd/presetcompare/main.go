// presetcompare: one-off comparison of the dashboard's built-in signal presets
// (保守/标准/激进) using the current default module weights (MACD 40% + EMA 40% + MFI 20%).
// Not wired into the Makefile; run directly with `go run ./cmd/presetcompare`.
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/jayce/btc-trader/internal/backtest"
	"github.com/jayce/btc-trader/internal/config"
	"github.com/jayce/btc-trader/internal/exchange"
	"github.com/jayce/btc-trader/internal/storage/timescale"
	"github.com/jayce/btc-trader/internal/strategy/trend"
	"go.uber.org/zap"
)

func mod(name string, weight float64) map[string]interface{} {
	return map[string]interface{}{"name": name, "weight": weight}
}

func baseModules() []interface{} {
	return []interface{}{
		mod("ema_cross", 0.4),
		mod("macd", 0.4),
		mod("mfi", 0.2),
	}
}

type presetCfg struct {
	label         string
	buyThreshold  float64
	sellThreshold float64
	confirmBars   int
	cooldownBars  int
	minHoldBars   int
	atrStopMult   float64
	htfEnabled    bool
	htfInterval   string
	htfPeriod     int
}

func main() {
	logger, _ := zap.NewDevelopment()
	defer logger.Sync()

	cfg, err := config.Load("")
	if err != nil {
		fmt.Fprintf(os.Stderr, "load config: %v\n", err)
		os.Exit(1)
	}

	ctx := context.Background()
	store, err := timescale.New(ctx, cfg.Database, logger.Named("db"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "connect db: %v\n", err)
		os.Exit(1)
	}
	defer store.Close()

	symbol := "BTCUSDT"
	end := time.Now().UTC()

	windows := []struct {
		label string
		days  int
	}{
		{"365d", 365},
		{"180d", 180},
		{"90d", 90},
	}

	presets := []presetCfg{
		{label: "保守 conservative", buyThreshold: 0.30, sellThreshold: -0.30, confirmBars: 2, cooldownBars: 24, minHoldBars: 12, atrStopMult: 3.5, htfEnabled: true, htfInterval: "1d", htfPeriod: 10},
		{label: "标准 standard", buyThreshold: 0.20, sellThreshold: -0.30, confirmBars: 1, cooldownBars: 12, minHoldBars: 6, atrStopMult: 3.0, htfEnabled: true, htfInterval: "1d", htfPeriod: 10},
		{label: "激进 aggressive", buyThreshold: 0.10, sellThreshold: -0.15, confirmBars: 1, cooldownBars: 2, minHoldBars: 3, atrStopMult: 2.0, htfEnabled: true, htfInterval: "1d", htfPeriod: 10},
	}

	cash, fee, allocPct := 10000.0, 0.001, 0.5

	klines1h, err := store.GetKlines(ctx, symbol, "1h", end.Add(-400*24*time.Hour), end, 0)
	if err != nil || len(klines1h) < 100 {
		fmt.Fprintf(os.Stderr, "load 1h klines: %v (n=%d)\n", err, len(klines1h))
		os.Exit(1)
	}
	klines1d, err := store.GetKlines(ctx, symbol, "1d", end.Add(-400*24*time.Hour), end, 0)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load 1d klines: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("%-20s %-8s %8s %8s %8s %8s %8s %8s\n", "preset", "window", "return%", "maxDD%", "winRate", "trades", "sharpe", "PF")

	for _, w := range windows {
		start := end.Add(-time.Duration(w.days) * 24 * time.Hour)
		sub1h := sliceSince(klines1h, start)
		sub1d := sliceSince(klines1d, start.Add(-30*24*time.Hour)) // extra buffer for HTF EMA warmup

		for _, p := range presets {
			strat := trend.NewCustomWeightedStrategy()
			initCfg := map[string]interface{}{
				"modules":        baseModules(),
				"buy_threshold":  p.buyThreshold,
				"sell_threshold": p.sellThreshold,
				"confirm_bars":   p.confirmBars,
				"cooldown_bars":  p.cooldownBars,
				"min_hold_bars":  p.minHoldBars,
				"atr_stop_mult":  p.atrStopMult,
				"htf_enabled":    p.htfEnabled,
				"htf_interval":   p.htfInterval,
				"htf_period":     p.htfPeriod,
			}
			if err := strat.Init(initCfg); err != nil {
				fmt.Printf("  init error for %s: %v\n", p.label, err)
				continue
			}

			engineCfg := backtest.EngineConfig{
				Symbol:      symbol,
				Interval:    "1h",
				InitialCash: cash,
				FeeRate:     fee,
				AllocPct:    allocPct,
			}
			if p.htfEnabled && len(sub1d) > 30 {
				engineCfg.HTFKlines = sub1d
				engineCfg.HTFInterval = p.htfInterval
				engineCfg.HTFIndReqs = strat.HTFIndicatorRequirements()
				engineCfg.HTFHistSize = strat.HTFHistoryRequired()
			}

			engine := backtest.NewEngine(engineCfg, strat, logger.Named("bt"))
			result, err := engine.Run(ctx, sub1h)
			if err != nil {
				fmt.Printf("  run error for %s: %v\n", p.label, err)
				continue
			}
			met := result.Metrics
			fmt.Printf("%-20s %-8s %8.2f %8.2f %7.1f%% %8d %8.2f %8.2f\n",
				p.label, w.label, met.TotalReturnPct, met.MaxDrawdownPct, met.WinRate*100, met.TotalTrades, met.SharpeRatio, met.ProfitFactor)
		}
		fmt.Println()
	}
}

func sliceSince(klines []exchange.Kline, start time.Time) []exchange.Kline {
	for i, k := range klines {
		if !k.OpenTime.Before(start) {
			return klines[i:]
		}
	}
	return klines
}
