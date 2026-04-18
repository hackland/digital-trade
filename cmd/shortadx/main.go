package main

import (
	"context"
	"fmt"
	"math"
	"os"
	"sort"
	"time"

	"github.com/jayce/btc-trader/internal/backtest"
	"github.com/jayce/btc-trader/internal/config"
	"github.com/jayce/btc-trader/internal/storage/timescale"
	"github.com/jayce/btc-trader/internal/strategy/trend"
	"go.uber.org/zap"
)

func main() {
	logger, _ := zap.NewProduction()
	cfg, err := config.Load("configs/config.yaml")
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		os.Exit(1)
	}
	ctx := context.Background()
	store, err := timescale.New(ctx, cfg.Database, logger.Named("db"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "db: %v\n", err)
		os.Exit(1)
	}
	defer store.Close()

	end := time.Now().UTC()
	sym := "BTCUSDT"
	iv := "1h"

	type period struct {
		name  string
		start time.Time
	}
	periods := []period{
		{"90d", end.Add(-90 * 24 * time.Hour)},
		{"180d", end.Add(-180 * 24 * time.Hour)},
		{"365d", end.Add(-365 * 24 * time.Hour)},
	}

	modules := []interface{}{
		map[string]interface{}{"name": "macd", "weight": 0.60},
		map[string]interface{}{"name": "ema_cross", "weight": 0.40},
	}

	type paramSet struct {
		label  string
		adxOn  bool
		adxMin float64
		adxDI  bool
		adxPer int
	}

	paramSets := []paramSet{
		{"基线(无ADX)", false, 0, false, 14},
		// ADX 阈值测试
		{"ADX>15", true, 15, false, 14},
		{"ADX>20", true, 20, false, 14},
		{"ADX>25", true, 25, false, 14},
		{"ADX>30", true, 30, false, 14},
		// ADX + DI方向
		{"ADX>15+DI", true, 15, true, 14},
		{"ADX>20+DI", true, 20, true, 14},
		{"ADX>25+DI", true, 25, true, 14},
		{"ADX>30+DI", true, 30, true, 14},
		// 不同周期
		{"ADX>20+DI p10", true, 20, true, 10},
		{"ADX>20+DI p20", true, 20, true, 20},
		{"ADX>25+DI p10", true, 25, true, 10},
		{"ADX>25+DI p20", true, 25, true, 20},
		// 只要求DI方向不要求ADX强度
		{"仅DI方向", true, 0, true, 14},
		{"仅DI p10", true, 0, true, 10},
	}

	fmt.Printf("╔══════════════════════════════════════════════════════════════╗\n")
	fmt.Printf("║  BTC 1h 做空 ADX趋势强度过滤 回测                        ║\n")
	fmt.Printf("╚══════════════════════════════════════════════════════════════╝\n\n")

	type result struct {
		label  string
		pd     string
		trades int
		retPct float64
		pf     float64
		wr     float64
	}
	var allResults []result

	for _, pd := range periods {
		klines, err := backtest.LoadKlinesFromStore(ctx, store, sym, iv, pd.start, end)
		if err != nil || len(klines) < 100 {
			continue
		}
		htfKlines, _ := backtest.LoadKlinesFromStore(ctx, store, sym, "1d", pd.start, end)

		for _, ps := range paramSets {
			tcfg := map[string]interface{}{
				"short_enabled": true,
				"htf_enabled":   true, "htf_interval": "1d", "htf_period": 5,
				"trend_filter":        false,
				"short_threshold":     -0.25,
				"cover_threshold":     0.15,
				"short_confirm_bars":  1,
				"short_min_hold_bars": 8,
				"short_atr_stop_mult": 2.5,
				"short_cooldown_bars": 12,
				// ADX params
				"short_adx_enabled":   ps.adxOn,
				"short_adx_period":    ps.adxPer,
				"short_adx_min":       ps.adxMin,
				"short_adx_di_filter": ps.adxDI,
				// Long params
				"buy_threshold": 0.20, "sell_threshold": -0.30,
				"confirm_bars": 1, "cooldown_bars": 12, "min_hold_bars": 18,
				"atr_stop_mult": 4.0, "atr_period": 14,
				"interval": iv,
				"modules":  modules,
			}

			strat := trend.NewCustomWeightedStrategy()
			if err := strat.Init(tcfg); err != nil {
				continue
			}

			engineCfg := backtest.EngineConfig{
				Symbol: sym, Interval: iv,
				InitialCash: 10000, FeeRate: 0.001, AllocPct: 0.9,
			}
			if len(htfKlines) > 0 {
				engineCfg.HTFKlines = htfKlines
				engineCfg.HTFInterval = "1d"
				engineCfg.HTFIndReqs = strat.HTFIndicatorRequirements()
				engineCfg.HTFHistSize = strat.HTFHistoryRequired()
			}

			engine := backtest.NewEngine(engineCfg, strat, logger.Named("bt"))
			res, err := engine.Run(ctx, klines)
			if err != nil {
				continue
			}

			sm := res.ShortMetrics
			allResults = append(allResults, result{
				label: ps.label, pd: pd.name,
				trades: sm.TotalTrades, retPct: sm.TotalReturnPct,
				pf: sm.ProfitFactor, wr: sm.WinRate * 100,
			})
		}
	}

	// 聚合
	type agg struct {
		totalRet  float64
		count     int
		minRet    float64
		maxRet    float64
		t90, t365 int
		details   string
	}
	aggMap := map[string]*agg{}
	for _, r := range allResults {
		a, ok := aggMap[r.label]
		if !ok {
			a = &agg{minRet: 999, maxRet: -999}
			aggMap[r.label] = a
		}
		a.totalRet += r.retPct
		a.count++
		if r.retPct < a.minRet {
			a.minRet = r.retPct
		}
		if r.retPct > a.maxRet {
			a.maxRet = r.retPct
		}
		if r.pd == "90d" {
			a.t90 = r.trades
		}
		if r.pd == "365d" {
			a.t365 = r.trades
		}
		a.details += fmt.Sprintf("[%s:%+.1f%%/%d笔] ", r.pd, r.retPct, r.trades)
	}

	type ranked struct {
		label     string
		avgRet    float64
		minRet    float64
		spread    float64
		t90, t365 int
		detail    string
	}
	var list []ranked
	for label, a := range aggMap {
		if a.count < 3 {
			continue
		}
		list = append(list, ranked{
			label: label, avgRet: a.totalRet / float64(a.count),
			minRet: a.minRet, spread: a.maxRet - a.minRet,
			t90: a.t90, t365: a.t365, detail: a.details,
		})
	}

	sort.Slice(list, func(i, j int) bool { return list[i].avgRet > list[j].avgRet })

	fmt.Printf("🏆 收益排名:\n")
	fmt.Printf("%-18s | %7s %7s %5s | 90d笔 365d笔 | 明细\n",
		"配置", "平均收益", "最差", "差幅")
	fmt.Println("─────────────────────────────────────────────────────────────────────────────────")
	for _, r := range list {
		fmt.Printf("%-18s | %+6.2f%% %+6.2f%% %4.1f%% | %-5d %-6d | %s\n",
			r.label, r.avgRet, r.minRet, r.spread, r.t90, r.t365, r.detail)
	}

	// 最优配置的逐月详情
	fmt.Printf("\n📅 最优配置逐月 PnL (365d):\n")
	bestLabels := []string{"基线(无ADX)", "ADX>20+DI", "ADX>25+DI", "仅DI方向"}
	for _, bl := range bestLabels {
		for _, ps := range paramSets {
			if ps.label != bl {
				continue
			}
			klines, _ := backtest.LoadKlinesFromStore(ctx, store, sym, iv, end.Add(-365*24*time.Hour), end)
			htfKlines, _ := backtest.LoadKlinesFromStore(ctx, store, sym, "1d", end.Add(-365*24*time.Hour), end)
			tcfg := map[string]interface{}{
				"short_enabled": true,
				"htf_enabled":   true, "htf_interval": "1d", "htf_period": 5,
				"trend_filter":    false,
				"short_threshold": -0.25, "cover_threshold": 0.15,
				"short_confirm_bars": 1, "short_min_hold_bars": 8,
				"short_atr_stop_mult": 2.5, "short_cooldown_bars": 12,
				"short_adx_enabled": ps.adxOn, "short_adx_period": ps.adxPer,
				"short_adx_min": ps.adxMin, "short_adx_di_filter": ps.adxDI,
				"buy_threshold": 0.20, "sell_threshold": -0.30,
				"confirm_bars": 1, "cooldown_bars": 12, "min_hold_bars": 18,
				"atr_stop_mult": 4.0, "atr_period": 14,
				"interval": iv, "modules": modules,
			}
			strat := trend.NewCustomWeightedStrategy()
			strat.Init(tcfg)
			engineCfg := backtest.EngineConfig{
				Symbol: sym, Interval: iv,
				InitialCash: 10000, FeeRate: 0.001, AllocPct: 0.9,
			}
			if len(htfKlines) > 0 {
				engineCfg.HTFKlines = htfKlines
				engineCfg.HTFInterval = "1d"
				engineCfg.HTFIndReqs = strat.HTFIndicatorRequirements()
				engineCfg.HTFHistSize = strat.HTFHistoryRequired()
			}
			engine := backtest.NewEngine(engineCfg, strat, logger.Named("bt"))
			res, _ := engine.Run(ctx, klines)

			monthPnL := map[string]float64{}
			monthTrades := map[string]int{}
			var sp, sq float64
			for _, tr := range res.ShortTrades {
				if tr.Side == "SHORT" {
					sp = tr.Price
					sq = tr.Quantity
				}
				if tr.Side == "COVER" && sq > 0 {
					m := tr.Timestamp.Format("2006-01")
					monthPnL[m] += (sp - tr.Price) * sq
					monthTrades[m]++
					sq = 0
				}
			}
			months := make([]string, 0)
			for m := range monthPnL {
				months = append(months, m)
			}
			sort.Strings(months)

			fmt.Printf("  【%s】365d: %+.2f%%, %d笔\n", bl, res.ShortMetrics.TotalReturnPct, res.ShortMetrics.TotalTrades)
			lossMonths := 0
			for _, m := range months {
				marker := "✅"
				if monthPnL[m] < -50 {
					marker = "❌"
					lossMonths++
				} else if monthPnL[m] < 100 {
					marker = "⚠️"
				}
				fmt.Printf("    %s %s %3d笔 %+7.0f$\n", marker, m, monthTrades[m], monthPnL[m])
			}
			fmt.Printf("    亏损月: %d/%d (%.0f%%)\n\n", lossMonths, len(months), float64(lossMonths)/math.Max(1, float64(len(months)))*100)
		}
	}
}
