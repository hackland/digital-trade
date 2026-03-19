// btcompare: custom_weighted strategy optimizer.
// Tests module combos × alloc sizes × dynamic sizing × native multi-TF on 90-day data.
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

type runResult struct {
	label       string
	trades      int
	returnPct   float64
	fees        float64
	winRate     float64
	maxDDPct    float64
	sharpe      float64
	sortino     float64
	profitFac   float64
	allocPct    float64
	dynamicSize bool
	multiTF     bool
}

func annualized(retPct float64, days int) float64 {
	if days <= 0 {
		return 0
	}
	return retPct / float64(days) * 365.0
}

type cwConfig struct {
	label         string
	modules       []interface{}
	buyThreshold  float64
	sellThreshold float64
	minHoldBars   int
	atrStopMult   float64
	confirmBars   int
	cooldownBars  int
	trendFilter   *bool
	trendPeriod   int
}

func mod(name string, weight float64) map[string]interface{} {
	return map[string]interface{}{"name": name, "weight": weight}
}

func boolPtr(b bool) *bool { return &b }

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

	end := time.Now().UTC()
	start90 := end.Add(-90 * 24 * time.Hour)
	symbol := "BTCUSDT"

	klineData := map[string][]exchange.Kline{}
	fmt.Println("╔════════════════════════════════════════════════════════════════════╗")
	fmt.Println("║   CUSTOM_WEIGHTED OPTIMIZER (90d, native multi-TF, dyn sizing)    ║")
	fmt.Println("╚════════════════════════════════════════════════════════════════════╝")
	fmt.Println()

	for _, intv := range []string{"1h", "4h"} {
		klines, err := store.GetKlines(ctx, symbol, intv, start90, end, 0)
		if err != nil {
			fmt.Printf("  ✗ %s: %v\n", intv, err)
			continue
		}
		klineData[intv] = klines
		if len(klines) > 0 {
			fmt.Printf("  ✓ %s: %d bars (%s ~ %s)\n", intv, len(klines),
				klines[0].OpenTime.Format("01-02 15:04"),
				klines[len(klines)-1].OpenTime.Format("01-02 15:04"))
		}
	}
	fmt.Println()

	klines1h := klineData["1h"]
	klines4h := klineData["4h"]

	if len(klines1h) < 100 {
		fmt.Fprintf(os.Stderr, "Not enough 1h data\n")
		os.Exit(1)
	}

	cash, fee := 10000.0, 0.001

	configs := []cwConfig{
		{label: "vol-heavy-cf2", confirmBars: 2, modules: []interface{}{
			mod("ema_cross", 0.15), mod("mfi", 0.25), mod("cmf", 0.20),
			mod("volume_ratio", 0.25), mod("obv_trend", 0.15),
		}},
		{label: "pure-volume", modules: []interface{}{
			mod("mfi", 0.30), mod("cmf", 0.25),
			mod("volume_ratio", 0.25), mod("obv_trend", 0.20),
		}},
		{label: "vol-heavy", modules: []interface{}{
			mod("ema_cross", 0.15), mod("mfi", 0.25), mod("cmf", 0.20),
			mod("volume_ratio", 0.25), mod("obv_trend", 0.15),
		}},
		{label: "vol-heavy+vroc", modules: []interface{}{
			mod("ema_cross", 0.10), mod("mfi", 0.20), mod("cmf", 0.20),
			mod("volume_ratio", 0.20), mod("obv_trend", 0.15), mod("vroc", 0.15),
		}},
		{label: "force+vol", modules: []interface{}{
			mod("force_index", 0.25), mod("volume_ratio", 0.25),
			mod("mfi", 0.20), mod("macd", 0.15), mod("cmf", 0.15),
		}},
		{label: "vol-cf2-loSell", confirmBars: 2, sellThreshold: -0.70, modules: []interface{}{
			mod("ema_cross", 0.15), mod("mfi", 0.25), mod("cmf", 0.20),
			mod("volume_ratio", 0.25), mod("obv_trend", 0.15),
		}},
		{label: "vol-cf2-hiSell", confirmBars: 2, sellThreshold: -0.30, modules: []interface{}{
			mod("ema_cross", 0.15), mod("mfi", 0.25), mod("cmf", 0.20),
			mod("volume_ratio", 0.25), mod("obv_trend", 0.15),
		}},
		{label: "vol-cf2-hold3", confirmBars: 2, minHoldBars: 3, modules: []interface{}{
			mod("ema_cross", 0.15), mod("mfi", 0.25), mod("cmf", 0.20),
			mod("volume_ratio", 0.25), mod("obv_trend", 0.15),
		}},
		{label: "vol-cf2-noTF", confirmBars: 2, trendFilter: boolPtr(false), modules: []interface{}{
			mod("ema_cross", 0.15), mod("mfi", 0.25), mod("cmf", 0.20),
			mod("volume_ratio", 0.25), mod("obv_trend", 0.15),
		}},
		{label: "trend-heavy", modules: []interface{}{
			mod("ema_cross", 0.30), mod("sma_trend", 0.25),
			mod("macd", 0.25), mod("bb_position", 0.20),
		}},
	}

	allocPcts := []float64{0.10, 0.30, 0.50}

	var results []runResult

	for _, cc := range configs {
		for _, allocPct := range allocPcts {
			type mode struct {
				dynamic bool
				mtf     bool
				suffix  string
			}
			modes := []mode{
				{false, false, ""},
				{true, false, "/dyn"},
			}
			if allocPct == 0.30 {
				modes = append(modes, mode{false, true, "/4hTF"})
				modes = append(modes, mode{true, true, "/dyn+4hTF"})
			}

			for _, m := range modes {
				strat := trend.NewCustomWeightedStrategy()
				initCfg := map[string]interface{}{"modules": cc.modules}
				if cc.buyThreshold != 0 {
					initCfg["buy_threshold"] = cc.buyThreshold
				}
				if cc.sellThreshold != 0 {
					initCfg["sell_threshold"] = cc.sellThreshold
				}
				if cc.minHoldBars != 0 {
					initCfg["min_hold_bars"] = cc.minHoldBars
				}
				if cc.atrStopMult != 0 {
					initCfg["atr_stop_mult"] = cc.atrStopMult
				}
				if cc.confirmBars != 0 {
					initCfg["confirm_bars"] = cc.confirmBars
				}
				if cc.cooldownBars != 0 {
					initCfg["cooldown_bars"] = cc.cooldownBars
				}
				if cc.trendFilter != nil {
					initCfg["trend_filter"] = *cc.trendFilter
				}
				if cc.trendPeriod != 0 {
					initCfg["trend_period"] = cc.trendPeriod
				}

				// Native multi-TF: enable htf_enabled in strategy config
				if m.mtf {
					initCfg["htf_enabled"] = true
					initCfg["htf_interval"] = "4h"
					initCfg["htf_period"] = 20
				}

				if err := strat.Init(initCfg); err != nil {
					fmt.Printf("  ✗ %s: init: %v\n", cc.label, err)
					continue
				}

				engineCfg := backtest.EngineConfig{
					Symbol:      symbol,
					Interval:    "1h",
					InitialCash: cash,
					FeeRate:     fee,
					AllocPct:    allocPct,
					DynamicSize: m.dynamic,
				}

				// Pass HTF klines to backtest engine
				if m.mtf && len(klines4h) > 30 {
					htfReqs := strat.HTFIndicatorRequirements()
					engineCfg.HTFKlines = klines4h
					engineCfg.HTFInterval = "4h"
					engineCfg.HTFIndReqs = htfReqs
					engineCfg.HTFHistSize = strat.HTFHistoryRequired()
				}

				engine := backtest.NewEngine(engineCfg, strat, logger.Named("bt"))

				result, err := engine.Run(ctx, klines1h)
				if err != nil {
					continue
				}
				met := result.Metrics
				label := fmt.Sprintf("%s|%.0f%%%s", cc.label, allocPct*100, m.suffix)
				results = append(results, runResult{
					label:       label,
					trades:      met.TotalTrades,
					returnPct:   met.TotalReturnPct,
					fees:        met.TotalFees,
					winRate:     met.WinRate * 100,
					maxDDPct:    met.MaxDrawdownPct,
					sharpe:      met.SharpeRatio,
					sortino:     met.SortinoRatio,
					profitFac:   met.ProfitFactor,
					allocPct:    allocPct,
					dynamicSize: m.dynamic,
					multiTF:     m.mtf,
				})
			}
		}
	}

	sort.Slice(results, func(i, j int) bool { return results[i].sharpe > results[j].sharpe })

	days := 90
	fmt.Printf("\n%d total results (90-day 1h backtest, %d bars)\n\n", len(results), len(klines1h))
	fmt.Println("┌──────────────────────────────────────┬────────┬─────────┬──────────┬─────────┬─────────┬─────────┬─────────┬──────────┐")
	fmt.Println("│ Config                               │ Trades │ Return% │ Ann.Ret% │ WinRate │ MaxDD%  │ Sharpe  │ Sortino │ ProfitF  │")
	fmt.Println("├──────────────────────────────────────┼────────┼─────────┼──────────┼─────────┼─────────┼─────────┼─────────┼──────────┤")
	for _, r := range results {
		name := r.label
		if len(name) > 36 {
			name = name[:36]
		}
		annRet := annualized(r.returnPct, days)
		fmt.Printf("│ %-36s │ %6d │ %+6.2f%% │ %+7.1f%% │ %5.1f%%  │ %5.2f%%  │ %7.2f │ %7.2f │ %8.2f │\n",
			name, r.trades, r.returnPct, annRet, r.winRate, r.maxDDPct, r.sharpe, r.sortino, r.profitFac)
	}
	fmt.Println("└──────────────────────────────────────┴────────┴─────────┴──────────┴─────────┴─────────┴─────────┴─────────┴──────────┘")

	fmt.Println()
	fmt.Println("═══════════════════════════════════════════════════════════════════")
	fmt.Println("  TOP 15 BY SHARPE RATIO")
	fmt.Println("═══════════════════════════════════════════════════════════════════")
	for i := 0; i < 15 && i < len(results); i++ {
		r := results[i]
		medal := fmt.Sprintf("%2d", i+1)
		if i == 0 {
			medal = "🥇"
		} else if i == 1 {
			medal = "🥈"
		} else if i == 2 {
			medal = "🥉"
		}
		flags := ""
		if r.dynamicSize {
			flags += " [DYN]"
		}
		if r.multiTF {
			flags += " [4hTF]"
		}
		annRet := annualized(r.returnPct, days)
		fmt.Printf("  %s %-28s Sharpe=%+.2f Ret=%+.2f%%(Ann:%+.0f%%) DD=%.2f%% Win=%.0f%% Tr=%d Fee=$%.0f%s\n",
			medal, r.label, r.sharpe, r.returnPct, annRet, r.maxDDPct, r.winRate, r.trades, r.fees, flags)
	}

	// Dimension analysis
	fmt.Println()
	fmt.Println("═══════════════════════════════════════════════════════════════════")
	fmt.Println("  DIMENSION ANALYSIS")
	fmt.Println("═══════════════════════════════════════════════════════════════════")

	fmt.Println("\n  ── By Allocation % ──")
	for _, a := range allocPcts {
		var sharpes, rets []float64
		for _, r := range results {
			if r.allocPct == a && !r.dynamicSize && !r.multiTF {
				sharpes = append(sharpes, r.sharpe)
				rets = append(rets, r.returnPct)
			}
		}
		if len(sharpes) > 0 {
			fmt.Printf("  %3.0f%%: avg_sharpe=%+.2f, avg_ret=%+.2f%%, avg_ann=%+.1f%% (%d configs)\n",
				a*100, avg(sharpes), avg(rets), annualized(avg(rets), days), len(sharpes))
		}
	}

	fmt.Println("\n  ── Dynamic vs Static Sizing (at 30% alloc) ──")
	var staticSharpes, dynSharpes []float64
	for _, r := range results {
		if r.allocPct == 0.30 && !r.multiTF {
			if r.dynamicSize {
				dynSharpes = append(dynSharpes, r.sharpe)
			} else {
				staticSharpes = append(staticSharpes, r.sharpe)
			}
		}
	}
	if len(staticSharpes) > 0 && len(dynSharpes) > 0 {
		fmt.Printf("  Static: avg_sharpe=%+.2f (%d)\n", avg(staticSharpes), len(staticSharpes))
		fmt.Printf("  Dynamic: avg_sharpe=%+.2f (%d)\n", avg(dynSharpes), len(dynSharpes))
	}

	fmt.Println("\n  ── Native Multi-TF (4h filter) vs Single-TF (at 30% alloc) ──")
	var singleSharpes, mtfSharpes []float64
	for _, r := range results {
		if r.allocPct == 0.30 && !r.dynamicSize {
			if r.multiTF {
				mtfSharpes = append(mtfSharpes, r.sharpe)
			} else {
				singleSharpes = append(singleSharpes, r.sharpe)
			}
		}
	}
	if len(singleSharpes) > 0 && len(mtfSharpes) > 0 {
		fmt.Printf("  Single-TF: avg_sharpe=%+.2f (%d)\n", avg(singleSharpes), len(singleSharpes))
		fmt.Printf("  Multi-TF(4h): avg_sharpe=%+.2f (%d)\n", avg(mtfSharpes), len(mtfSharpes))
	}

	fmt.Println()
	if len(results) > 0 {
		best := results[0]
		annRet := annualized(best.returnPct, days)
		fmt.Println("═══════════════════════════════════════════════════════════════════")
		fmt.Printf("  🏆 BEST: %s\n", best.label)
		fmt.Printf("     Sharpe=%.2f, Return=%+.2f%% (Ann: %+.1f%%), MaxDD=%.2f%%, WinRate=%.0f%%\n",
			best.sharpe, best.returnPct, annRet, best.maxDDPct, best.winRate)
		fmt.Printf("     Trades=%d, Fees=$%.0f, ProfitFactor=%.2f\n", best.trades, best.fees, best.profitFac)
		fmt.Println("═══════════════════════════════════════════════════════════════════")
	}
}

func avg(vals []float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	sum := 0.0
	for _, v := range vals {
		sum += v
	}
	return sum / float64(len(vals))
}
