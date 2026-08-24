// gridcompare: compare custom_weighted module combos across main intervals (1h/4h)
// using the dashboard's "标准" signal-control params as a fixed control.
// One-off research tool, not wired into the Makefile. Run with `go run ./cmd/gridcompare`.
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

type combo struct {
	label   string
	modules []interface{}
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

	combos := []combo{
		{"baseline(ema40+macd40+mfi20)", []interface{}{mod("ema_cross", 0.4), mod("macd", 0.4), mod("mfi", 0.2)}},
		{"macd-heavy(macd60+ema20+mfi20)", []interface{}{mod("macd", 0.6), mod("ema_cross", 0.2), mod("mfi", 0.2)}},
		{"trend+rsi(ema30+macd30+rsi20+mfi20)", []interface{}{mod("ema_cross", 0.3), mod("macd", 0.3), mod("rsi", 0.2), mod("mfi", 0.2)}},
		{"vol-augmented(ema25+macd25+mfi20+cmf15+volr15)", []interface{}{mod("ema_cross", 0.25), mod("macd", 0.25), mod("mfi", 0.2), mod("cmf", 0.15), mod("volume_ratio", 0.15)}},
		{"trend-only(ema35+macd35+sma30)", []interface{}{mod("ema_cross", 0.35), mod("macd", 0.35), mod("sma_trend", 0.3)}},
		{"macd+kdj+mfi(macd40+kdj30+mfi30)", []interface{}{mod("macd", 0.4), mod("kdj", 0.3), mod("mfi", 0.3)}},
	}

	// "标准" signal-control params, held fixed as the control across all combos/intervals.
	buyThreshold, sellThreshold := 0.20, -0.30
	confirmBars, cooldownBars, minHoldBars := 1, 12, 6
	atrStopMult := 3.0
	htfInterval, htfPeriod := "1d", 10

	windows := []struct {
		label string
		days  int
	}{
		{"365d", 365},
		{"180d", 180},
	}

	intervals := []string{"1h", "4h"}

	cash, fee, allocPct := 10000.0, 0.001, 0.5

	klineCache := map[string][]exchange.Kline{}
	for _, intv := range append(append([]string{}, intervals...), htfInterval) {
		if _, ok := klineCache[intv]; ok {
			continue
		}
		ks, err := store.GetKlines(ctx, symbol, intv, end.Add(-400*24*time.Hour), end, 0)
		if err != nil {
			fmt.Fprintf(os.Stderr, "load %s klines: %v\n", intv, err)
			os.Exit(1)
		}
		klineCache[intv] = ks
	}

	fmt.Printf("%-45s %-6s %-6s %8s %8s %8s %8s %8s %8s\n",
		"combo", "intv", "window", "return%", "maxDD%", "winRate", "trades", "sharpe", "PF")

	for _, w := range windows {
		start := end.Add(-time.Duration(w.days) * 24 * time.Hour)
		for _, intv := range intervals {
			subMain := sliceSince(klineCache[intv], start)
			subHTF := sliceSince(klineCache[htfInterval], start.Add(-30*24*time.Hour))

			for _, c := range combos {
				strat := trend.NewCustomWeightedStrategy()
				initCfg := map[string]interface{}{
					"modules":        c.modules,
					"buy_threshold":  buyThreshold,
					"sell_threshold": sellThreshold,
					"confirm_bars":   confirmBars,
					"cooldown_bars":  cooldownBars,
					"min_hold_bars":  minHoldBars,
					"atr_stop_mult":  atrStopMult,
					"htf_enabled":    true,
					"htf_interval":   htfInterval,
					"htf_period":     htfPeriod,
				}
				if err := strat.Init(initCfg); err != nil {
					fmt.Printf("  init error for %s: %v\n", c.label, err)
					continue
				}

				engineCfg := backtest.EngineConfig{
					Symbol:      symbol,
					Interval:    intv,
					InitialCash: cash,
					FeeRate:     fee,
					AllocPct:    allocPct,
				}
				if len(subHTF) > 30 {
					engineCfg.HTFKlines = subHTF
					engineCfg.HTFInterval = htfInterval
					engineCfg.HTFIndReqs = strat.HTFIndicatorRequirements()
					engineCfg.HTFHistSize = strat.HTFHistoryRequired()
				}

				engine := backtest.NewEngine(engineCfg, strat, logger.Named("bt"))
				result, err := engine.Run(ctx, subMain)
				if err != nil {
					fmt.Printf("  run error for %s/%s: %v\n", c.label, intv, err)
					continue
				}
				met := result.Metrics
				fmt.Printf("%-45s %-6s %-6s %8.2f %8.2f %7.1f%% %8d %8.2f %8.2f\n",
					c.label, intv, w.label, met.TotalReturnPct, met.MaxDrawdownPct, met.WinRate*100, met.TotalTrades, met.SharpeRatio, met.ProfitFactor)
			}
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
