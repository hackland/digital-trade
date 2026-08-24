// gridcompare4h2: walk-forward parameter search for a LOW-FREQUENCY 4h
// custom_weighted strategy on BTCUSDT. Splits the available ~365d of history
// into an in-sample period (parameter search) and an out-of-sample period
// (validation), to avoid picking a config that only looks good because it was
// curve-fit to the full backtest window.
// One-off research tool, not wired into the Makefile. Run with `go run ./cmd/gridcompare4h2`.
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
		"htf_interval":            htfInterval,
		"htf_period":              10,
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

	// 70/30 walk-forward split: optimize on the first ~255 days, validate on the last ~110.
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
	htfInSample := sliceSince(klineHTF, fullStart.Add(-30*24*time.Hour))
	htfOutSample := sliceSince(klineHTF, splitAt.Add(-30*24*time.Hour))

	fmt.Printf("in-sample:  %s ~ %s (%d bars)\n", inSample[0].OpenTime.Format("2006-01-02"), inSample[len(inSample)-1].OpenTime.Format("2006-01-02"), len(inSample))
	fmt.Printf("out-sample: %s ~ %s (%d bars)\n\n", outSample[0].OpenTime.Format("2006-01-02"), outSample[len(outSample)-1].OpenTime.Format("2006-01-02"), len(outSample))

	combos := []combo{
		{"baseline(ema40+macd40+mfi20)", []interface{}{mod("ema_cross", 0.4), mod("macd", 0.4), mod("mfi", 0.2)}},
		{"trend+rsi(ema30+macd30+rsi20+mfi20)", []interface{}{mod("ema_cross", 0.3), mod("macd", 0.3), mod("rsi", 0.2), mod("mfi", 0.2)}},
		{"macd-heavy(macd60+ema20+mfi20)", []interface{}{mod("macd", 0.6), mod("ema_cross", 0.2), mod("mfi", 0.2)}},
	}
	buyThresholds := []float64{0.15, 0.20, 0.25, 0.30}
	sellThresholds := []float64{-0.15, -0.20, -0.25, -0.30}
	confirmBarsOpts := []int{1, 2}
	cooldownBarsOpts := []int{4, 6, 8, 12}
	minHoldBarsOpts := []int{4, 6, 8, 12}
	atrStopMultOpts := []float64{2.5, 3.0, 3.5, 4.0}

	cash, fee, allocPct := 10000.0, 0.001, 0.5

	var candidates []paramSet
	for _, c := range combos {
		for _, buy := range buyThresholds {
			for _, sell := range sellThresholds {
				for _, confirm := range confirmBarsOpts {
					for _, cooldown := range cooldownBarsOpts {
						for _, hold := range minHoldBarsOpts {
							for _, atr := range atrStopMultOpts {
								candidates = append(candidates, paramSet{
									comboLabel: c.label, modules: c.modules,
									buyThreshold: buy, sellThreshold: sell,
									confirmBars: confirm, cooldownBars: cooldown, minHoldBars: hold,
									atrStopMult: atr,
								})
							}
						}
					}
				}
			}
		}
	}
	fmt.Printf("testing %d parameter combos in-sample...\n", len(candidates))

	type scored struct {
		p  paramSet
		in metrics
	}
	var inSampleResults []scored
	minInSampleTrades := 15
	for _, p := range candidates {
		met, err := runBacktest(ctx, logger, symbol, mainInterval, inSample, htfInSample, htfInterval, p, cash, fee, allocPct)
		if err != nil || met.trades < minInSampleTrades {
			continue
		}
		inSampleResults = append(inSampleResults, scored{p, met})
	}
	sort.Slice(inSampleResults, func(i, j int) bool { return inSampleResults[i].in.sharpe > inSampleResults[j].in.sharpe })

	topN := 20
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

	// Rank by combined score: reward configs that hold up out-of-sample too (not just in-sample).
	sort.Slice(results, func(i, j int) bool {
		si := results[i].in.sharpe + results[i].out.sharpe
		sj := results[j].in.sharpe + results[j].out.sharpe
		return si > sj
	})

	fmt.Printf("%-38s %5s %5s %2s %3s %4s %4s | %7s %6s %6s %5s %3s | %7s %6s %6s %5s %3s\n",
		"combo", "buy", "sell", "cf", "cd", "hold", "atr",
		"IS-ret%", "IS-dd%", "IS-shp", "IS-pf", "IS-n",
		"OS-ret%", "OS-dd%", "OS-shp", "OS-pf", "OS-n")
	for _, r := range results {
		fmt.Printf("%-38s %5.2f %5.2f %2d %3d %4d %4.1f | %7.2f %6.2f %6.2f %5.2f %3d | %7.2f %6.2f %6.2f %5.2f %3d\n",
			r.p.comboLabel, r.p.buyThreshold, r.p.sellThreshold, r.p.confirmBars, r.p.cooldownBars, r.p.minHoldBars, r.p.atrStopMult,
			r.in.returnPct, r.in.maxDDPct, r.in.sharpe, r.in.pf, r.in.trades,
			r.out.returnPct, r.out.maxDDPct, r.out.sharpe, r.out.pf, r.out.trades)
	}
}
