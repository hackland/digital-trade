// gridcompare4h3: module-combo search for the 4h low-frequency custom_weighted
// strategy, holding the walk-forward-validated signal-control region fixed
// (confirm=1, cooldown=6, hold=12, atr=3.0) and sweeping which indicator
// modules to weight, plus a light threshold check around each combo.
// Validated walk-forward (in-sample search, out-of-sample confirm), same as
// gridcompare4h2. One-off research tool, not wired into the Makefile.
// Run with `go run ./cmd/gridcompare4h3`.
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

type paramSet struct {
	comboLabel                             string
	modules                                []interface{}
	buyThreshold, sellThreshold            float64
	confirmBars, cooldownBars, minHoldBars int
	atrStopMult                            float64
	trendFilter                            bool
	htfInterval                            string
	htfPeriod                              int
}

type metrics struct {
	trades              int
	returnPct, maxDDPct float64
	sharpe, pf, wr      float64
}

func runBacktest(ctx context.Context, logger *zap.Logger, symbol, interval string, klines, htfKlines []exchange.Kline, htfInterval string, p paramSet, cash, fee, allocPct float64) (metrics, error) {
	strat := trend.NewCustomWeightedStrategy()
	initCfg := map[string]interface{}{
		"modules":                 p.modules,
		"buy_threshold":           p.buyThreshold,
		"sell_threshold":          p.sellThreshold,
		"confirm_bars":            p.confirmBars,
		"cooldown_bars":           p.cooldownBars,
		"min_hold_bars":           p.minHoldBars,
		"atr_stop_mult":           p.atrStopMult,
		"atr_activate_profit_pct": 0.0,
		"hard_stop_pct":           1.5,
		"ema_cross_min":           0.15,
		"trend_filter":            false,
		"trend_period":            50,
		"htf_enabled":             true,
		"htf_interval":            p.htfInterval,
		"htf_period":              p.htfPeriod,
	}
	if err := strat.Init(initCfg); err != nil {
		return metrics{}, err
	}
	engineCfg := backtest.EngineConfig{
		Symbol: symbol, Interval: interval, InitialCash: cash, FeeRate: fee, AllocPct: allocPct,
	}
	if len(htfKlines) > 30 {
		engineCfg.HTFKlines = htfKlines
		engineCfg.HTFInterval = htfInterval
		engineCfg.HTFIndReqs = strat.HTFIndicatorRequirements()
		engineCfg.HTFHistSize = strat.HTFHistoryRequired()
	}
	engine := backtest.NewEngine(engineCfg, strat, logger)
	result, err := engine.Run(ctx, klines)
	if err != nil {
		return metrics{}, err
	}
	met := result.Metrics
	return metrics{
		trades: met.TotalTrades, returnPct: met.TotalReturnPct, maxDDPct: met.MaxDrawdownPct,
		sharpe: met.SharpeRatio, pf: met.ProfitFactor, wr: met.WinRate * 100,
	}, nil
}

func sliceRange(klines []exchange.Kline, start, end time.Time) []exchange.Kline {
	var out []exchange.Kline
	for _, k := range klines {
		if !k.OpenTime.Before(start) && k.OpenTime.Before(end) {
			out = append(out, k)
		}
	}
	return out
}

func sliceSince(klines []exchange.Kline, start time.Time) []exchange.Kline {
	for i, k := range klines {
		if !k.OpenTime.Before(start) {
			return klines[i:]
		}
	}
	return klines
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
	mainInterval := "4h"
	htfInterval := "1d"
	end := time.Now().UTC()
	fullStart := end.Add(-365 * 24 * time.Hour)
	splitAt := fullStart.Add(255 * 24 * time.Hour)

	klineMain, err := store.GetKlines(ctx, symbol, mainInterval, fullStart, end, 0)
	if err != nil || len(klineMain) < 100 {
		fmt.Fprintf(os.Stderr, "load %s klines: %v (n=%d)\n", mainInterval, err, len(klineMain))
		os.Exit(1)
	}
	klineHTF, err := store.GetKlines(ctx, symbol, htfInterval, fullStart, end, 0)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load %s klines: %v\n", htfInterval, err)
		os.Exit(1)
	}

	inSample := sliceRange(klineMain, fullStart, splitAt)
	outSample := sliceRange(klineMain, splitAt, end)
	htfInSample := sliceSince(klineHTF, fullStart.Add(-90*24*time.Hour))
	htfOutSample := sliceSince(klineHTF, splitAt.Add(-90*24*time.Hour))

	fmt.Printf("in-sample:  %s ~ %s (%d bars)\n", inSample[0].OpenTime.Format("2006-01-02"), inSample[len(inSample)-1].OpenTime.Format("2006-01-02"), len(inSample))
	fmt.Printf("out-sample: %s ~ %s (%d bars)\n\n", outSample[0].OpenTime.Format("2006-01-02"), outSample[len(outSample)-1].OpenTime.Format("2006-01-02"), len(outSample))

	// htf_period sweep: is "大周期EMA=10" (10-day EMA on the daily HTF filter) a
	// reasonable choice, or would a different lookback do better? Module combo
	// and all other signal params held at the already-validated recommendation.
	winningModules := []interface{}{mod("macd", 0.45), mod("ema_cross", 0.35), mod("mfi", 0.2)}

	buyThresholds := []float64{0.20}
	sellThresholds := []float64{-0.25}
	confirmBars, cooldownBars, minHoldBars, atrStopMult := 1, 6, 12, 3.0

	cash, fee, allocPct := 10000.0, 0.001, 0.5

	var candidates []paramSet
	for _, htfP := range []int{5, 8, 10, 15, 20, 30, 40, 50} {
		for _, buy := range buyThresholds {
			for _, sell := range sellThresholds {
				candidates = append(candidates, paramSet{
					comboLabel: fmt.Sprintf("htf_period=%d (macd45+ema35+mfi20)", htfP), modules: winningModules,
					buyThreshold: buy, sellThreshold: sell,
					confirmBars: confirmBars, cooldownBars: cooldownBars, minHoldBars: minHoldBars,
					atrStopMult: atrStopMult, htfInterval: "1d", htfPeriod: htfP,
				})
			}
		}
	}
	fmt.Printf("testing %d module-combo x threshold candidates in-sample...\n", len(candidates))

	type scored struct {
		p  paramSet
		in metrics
	}
	var inSampleResults []scored
	minInSampleTrades := 10
	for _, p := range candidates {
		met, err := runBacktest(ctx, logger, symbol, mainInterval, inSample, htfInSample, htfInterval, p, cash, fee, allocPct)
		if err != nil {
			fmt.Printf("  init/run error for %s: %v\n", p.comboLabel, err)
			continue
		}
		if met.trades < minInSampleTrades {
			continue
		}
		inSampleResults = append(inSampleResults, scored{p, met})
	}
	sort.Slice(inSampleResults, func(i, j int) bool { return inSampleResults[i].in.sharpe > inSampleResults[j].in.sharpe })

	topN := 8
	if len(inSampleResults) < topN {
		topN = len(inSampleResults)
	}
	fmt.Printf("top %d in-sample candidates (min %d trades) -> validating out-of-sample:\n\n", topN, minInSampleTrades)

	type validated struct {
		p       paramSet
		in, out metrics
	}
	var results []validated
	for i := 0; i < topN; i++ {
		p := inSampleResults[i].p
		outMet, err := runBacktest(ctx, logger, symbol, mainInterval, outSample, htfOutSample, htfInterval, p, cash, fee, allocPct)
		if err != nil {
			continue
		}
		results = append(results, validated{p, inSampleResults[i].in, outMet})
	}

	sort.Slice(results, func(i, j int) bool {
		si := results[i].in.sharpe + results[i].out.sharpe
		sj := results[j].in.sharpe + results[j].out.sharpe
		return si > sj
	})

	fmt.Printf("%-30s | %7s %6s %6s %5s %6s %3s | %7s %6s %6s %5s %6s %3s\n",
		"combo",
		"IS-ret%", "IS-dd%", "IS-shp", "IS-pf", "IS-wr%", "IS-n",
		"OS-ret%", "OS-dd%", "OS-shp", "OS-pf", "OS-wr%", "OS-n")
	for _, r := range results {
		fmt.Printf("%-30s | %7.2f %6.2f %6.2f %5.2f %6.1f %3d | %7.2f %6.2f %6.2f %5.2f %6.1f %3d\n",
			r.p.comboLabel,
			r.in.returnPct, r.in.maxDDPct, r.in.sharpe, r.in.pf, r.in.wr, r.in.trades,
			r.out.returnPct, r.out.maxDDPct, r.out.sharpe, r.out.pf, r.out.wr, r.out.trades)
	}
}
