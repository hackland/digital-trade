// gridcompare4h: parameter + module sweep for a LOW-FREQUENCY 4h custom_weighted
// strategy on BTCUSDT. Prints the top results by Sharpe ratio per window.
// One-off research tool, not wired into the Makefile. Run with `go run ./cmd/gridcompare4h`.
package main

import (
	"context"
	"fmt"
	"os"
	"sort"
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

type run struct {
	comboLabel                             string
	buyThreshold, sellThreshold            float64
	confirmBars, cooldownBars, minHoldBars int
	atrStopMult                            float64
	window                                 string
	trades                                 int
	returnPct, maxDDPct, sharpe, pf, wr    float64
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
	mainInterval := "4h"
	htfInterval, htfPeriod := "1d", 10

	combos := []combo{
		{"baseline(ema40+macd40+mfi20)", []interface{}{mod("ema_cross", 0.4), mod("macd", 0.4), mod("mfi", 0.2)}},
		{"trend+rsi(ema30+macd30+rsi20+mfi20)", []interface{}{mod("ema_cross", 0.3), mod("macd", 0.3), mod("rsi", 0.2), mod("mfi", 0.2)}},
		{"macd-heavy(macd60+ema20+mfi20)", []interface{}{mod("macd", 0.6), mod("ema_cross", 0.2), mod("mfi", 0.2)}},
	}

	buyThresholds := []float64{0.15, 0.20, 0.25}
	sellThresholds := []float64{-0.20, -0.30}
	confirmBarsOpts := []int{1, 2}
	cooldownBarsOpts := []int{2, 4, 8}
	minHoldBarsOpts := []int{2, 4, 8}
	atrStopMult := 3.0

	windows := []struct {
		label string
		days  int
	}{
		{"365d", 365},
		{"180d", 180},
	}

	cash, fee, allocPct := 10000.0, 0.001, 0.5

	klineMain, err := store.GetKlines(ctx, symbol, mainInterval, end.Add(-400*24*time.Hour), end, 0)
	if err != nil || len(klineMain) < 100 {
		fmt.Fprintf(os.Stderr, "load %s klines: %v (n=%d)\n", mainInterval, err, len(klineMain))
		os.Exit(1)
	}
	klineHTF, err := store.GetKlines(ctx, symbol, htfInterval, end.Add(-400*24*time.Hour), end, 0)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load %s klines: %v\n", htfInterval, err)
		os.Exit(1)
	}

	var allRuns []run

	for _, w := range windows {
		start := end.Add(-time.Duration(w.days) * 24 * time.Hour)
		subMain := sliceSince(klineMain, start)
		subHTF := sliceSince(klineHTF, start.Add(-30*24*time.Hour))

		for _, c := range combos {
			for _, buy := range buyThresholds {
				for _, sell := range sellThresholds {
					for _, confirm := range confirmBarsOpts {
						for _, cooldown := range cooldownBarsOpts {
							for _, hold := range minHoldBarsOpts {
								strat := trend.NewCustomWeightedStrategy()
								initCfg := map[string]interface{}{
									"modules":        c.modules,
									"buy_threshold":  buy,
									"sell_threshold": sell,
									"confirm_bars":   confirm,
									"cooldown_bars":  cooldown,
									"min_hold_bars":  hold,
									"atr_stop_mult":  atrStopMult,
									"htf_enabled":    true,
									"htf_interval":   htfInterval,
									"htf_period":     htfPeriod,
								}
								if err := strat.Init(initCfg); err != nil {
									continue
								}

								engineCfg := backtest.EngineConfig{
									Symbol:      symbol,
									Interval:    mainInterval,
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
									continue
								}
								met := result.Metrics
								allRuns = append(allRuns, run{
									comboLabel: c.label, buyThreshold: buy, sellThreshold: sell,
									confirmBars: confirm, cooldownBars: cooldown, minHoldBars: hold,
									atrStopMult: atrStopMult, window: w.label,
									trades: met.TotalTrades, returnPct: met.TotalReturnPct,
									maxDDPct: met.MaxDrawdownPct, sharpe: met.SharpeRatio,
									pf: met.ProfitFactor, wr: met.WinRate * 100,
								})
							}
						}
					}
				}
			}
		}
	}

	minTrades := map[string]int{"365d": 8, "180d": 4}

	for _, w := range windows {
		var filtered []run
		for _, r := range allRuns {
			if r.window == w.label && r.trades >= minTrades[w.label] {
				filtered = append(filtered, r)
			}
		}
		sort.Slice(filtered, func(i, j int) bool { return filtered[i].sharpe > filtered[j].sharpe })

		fmt.Printf("\n=== Top 15 by Sharpe, window=%s (min trades=%d) ===\n", w.label, minTrades[w.label])
		fmt.Printf("%-38s %6s %6s %3s %4s %4s %8s %8s %7s %7s %6s\n",
			"combo", "buy", "sell", "cf", "cd", "hold", "return%", "maxDD%", "sharpe", "PF", "trades")
		n := 15
		if len(filtered) < n {
			n = len(filtered)
		}
		for i := 0; i < n; i++ {
			r := filtered[i]
			fmt.Printf("%-38s %6.2f %6.2f %3d %4d %4d %8.2f %8.2f %7.2f %7.2f %6d\n",
				r.comboLabel, r.buyThreshold, r.sellThreshold, r.confirmBars, r.cooldownBars, r.minHoldBars,
				r.returnPct, r.maxDDPct, r.sharpe, r.pf, r.trades)
		}
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
