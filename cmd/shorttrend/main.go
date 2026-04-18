package main

import (
	"context"
	"fmt"
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
	type period struct {
		name  string
		start time.Time
	}
	periods := []period{
		{"90d", end.Add(-90 * 24 * time.Hour)},
		{"180d", end.Add(-180 * 24 * time.Hour)},
		{"365d", end.Add(-365 * 24 * time.Hour)},
	}

	// ==========================================
	// 趋势类做空模块组合
	// ==========================================
	type modulePlan struct {
		name    string
		modules []interface{}
	}
	modulePlans := []modulePlan{
		// 纯趋势
		{"EMA+MACD", []interface{}{
			map[string]interface{}{"name": "ema_cross", "weight": 0.50},
			map[string]interface{}{"name": "macd", "weight": 0.50},
		}},
		{"EMA+MACD+SMA", []interface{}{
			map[string]interface{}{"name": "ema_cross", "weight": 0.40},
			map[string]interface{}{"name": "macd", "weight": 0.35},
			map[string]interface{}{"name": "sma_trend", "weight": 0.25},
		}},
		{"EMA主导+MACD", []interface{}{
			map[string]interface{}{"name": "ema_cross", "weight": 0.60},
			map[string]interface{}{"name": "macd", "weight": 0.40},
		}},
		{"MACD主导+EMA", []interface{}{
			map[string]interface{}{"name": "macd", "weight": 0.60},
			map[string]interface{}{"name": "ema_cross", "weight": 0.40},
		}},
		// 趋势 + 少量量能确认
		{"EMA+MACD+OBV", []interface{}{
			map[string]interface{}{"name": "ema_cross", "weight": 0.40},
			map[string]interface{}{"name": "macd", "weight": 0.35},
			map[string]interface{}{"name": "obv_trend", "weight": 0.25},
		}},
		{"EMA+MACD+ForceIdx", []interface{}{
			map[string]interface{}{"name": "ema_cross", "weight": 0.40},
			map[string]interface{}{"name": "macd", "weight": 0.35},
			map[string]interface{}{"name": "force_index", "weight": 0.25},
		}},
		// 对比: 之前的震荡类组合
		{"旧:MACD+RSI+BB", []interface{}{
			map[string]interface{}{"name": "macd", "weight": 0.40},
			map[string]interface{}{"name": "rsi", "weight": 0.40},
			map[string]interface{}{"name": "bb_position", "weight": 0.20},
		}},
	}

	// 参数组合
	thresholds := []float64{-0.10, -0.15, -0.20, -0.25, -0.30}
	atrMults := []float64{2.5, 3.0, 3.5, 4.0}
	minHolds := []int{3, 6, 8}
	cooldowns := []int{6, 8, 12}

	type result struct {
		modName  string
		pd       string
		thresh   float64
		atr      float64
		minHold  int
		cooldown int
		trades   int
		winRate  float64
		retPct   float64
		pf       float64
	}

	symbols := []string{"BTCUSDT", "ETHUSDT"}

	for _, sym := range symbols {
		fmt.Printf("\n\n╔══════════════════════════════════════════╗\n")
		fmt.Printf("║        %s 1h 趋势做空回测             ║\n", sym)
		fmt.Printf("╚══════════════════════════════════════════╝\n")

		var allResults []result

		for _, pd := range periods {
			klines, err := backtest.LoadKlinesFromStore(ctx, store, sym, "1h", pd.start, end)
			if err != nil || len(klines) < 100 {
				fmt.Printf("  %s 数据不足, 跳过\n", pd.name)
				continue
			}
			htfKlines, _ := backtest.LoadKlinesFromStore(ctx, store, sym, "1d", pd.start, end)
			fmt.Printf("  测试 %s (%d K线)...\n", pd.name, len(klines))

			for _, mp := range modulePlans {
				for _, th := range thresholds {
					for _, am := range atrMults {
						for _, mh := range minHolds {
							for _, cd := range cooldowns {
								tcfg := map[string]interface{}{
									"short_enabled": true,
									"htf_enabled":   true, "htf_interval": "1d", "htf_period": 5,
									"trend_filter":        false,
									"short_threshold":     th,
									"cover_threshold":     -th * 0.6,
									"short_confirm_bars":  1,
									"short_min_hold_bars": mh,
									"short_atr_stop_mult": am,
									"short_cooldown_bars": cd,
									"buy_threshold":       0.20, "sell_threshold": -0.30,
									"confirm_bars": 1, "cooldown_bars": 12, "min_hold_bars": 18,
									"atr_stop_mult": 4.0, "atr_period": 14,
									"interval": "1h",
									"modules":  mp.modules,
								}

								strat := trend.NewCustomWeightedStrategy()
								if err := strat.Init(tcfg); err != nil {
									continue
								}

								engineCfg := backtest.EngineConfig{
									Symbol: sym, Interval: "1h",
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
								if err != nil || res.ShortMetrics.TotalTrades == 0 {
									continue
								}
								sm := res.ShortMetrics
								allResults = append(allResults, result{
									modName: mp.name, pd: pd.name,
									thresh: th, atr: am, minHold: mh, cooldown: cd,
									trades: sm.TotalTrades, winRate: sm.WinRate * 100,
									retPct: sm.TotalReturnPct, pf: sm.ProfitFactor,
								})
							}
						}
					}
				}
			}
		}

		// 聚合: 同一 (模块+参数) 跨时间段
		type paramKey struct {
			modName  string
			thresh   float64
			atr      float64
			minHold  int
			cooldown int
		}
		type agg struct {
			totalRet float64
			totalPF  float64
			totalWR  float64
			count    int
			minRet   float64
			details  string
		}
		aggMap := map[paramKey]*agg{}

		for _, r := range allResults {
			pk := paramKey{r.modName, r.thresh, r.atr, r.minHold, r.cooldown}
			a, ok := aggMap[pk]
			if !ok {
				a = &agg{minRet: 999}
				aggMap[pk] = a
			}
			a.totalRet += r.retPct
			a.totalPF += r.pf
			a.totalWR += r.winRate
			a.count++
			if r.retPct < a.minRet {
				a.minRet = r.retPct
			}
			a.details += fmt.Sprintf("[%s:%+.1f%%] ", r.pd, r.retPct)
		}

		type ranked struct {
			key    paramKey
			avgRet float64
			avgPF  float64
			avgWR  float64
			minRet float64
			count  int
			detail string
		}
		var list []ranked
		for pk, a := range aggMap {
			if a.count >= 3 { // 必须跨 3 个时间段都有交易
				list = append(list, ranked{
					key: pk, avgRet: a.totalRet / float64(a.count),
					avgPF:  a.totalPF / float64(a.count),
					avgWR:  a.totalWR / float64(a.count),
					minRet: a.minRet, count: a.count, detail: a.details,
				})
			}
		}

		// 按平均收益排序
		sort.Slice(list, func(i, j int) bool { return list[i].avgRet > list[j].avgRet })

		fmt.Printf("\n🏆 收益 TOP 15:\n")
		fmt.Printf("%-16s %-6s %-4s %-4s %-3s | %7s %6s %6s %8s\n",
			"模块", "阈值", "ATR", "持仓", "冷却", "平均收益", "平均PF", "胜率%", "最差")
		fmt.Println("──────────────────────────────────────────────────────────────────────────")
		limit := 15
		if len(list) < limit {
			limit = len(list)
		}
		for i := 0; i < limit; i++ {
			r := list[i]
			fmt.Printf("%-16s %+.2f  %-4.1f %-4d %-3d | %+6.2f%% %5.2f  %5.1f%% %+7.2f%%\n",
				r.key.modName, r.key.thresh, r.key.atr, r.key.minHold, r.key.cooldown,
				r.avgRet, r.avgPF, r.avgWR, r.minRet)
		}

		// 稳定性排名 (最差 > 0 的优先)
		sort.Slice(list, func(i, j int) bool { return list[i].minRet > list[j].minRet })
		fmt.Printf("\n📊 稳定性 TOP 15（最差区间收益最高）:\n")
		fmt.Printf("%-16s %-6s %-4s %-4s %-3s | %7s %6s %6s %8s | 明细\n",
			"模块", "阈值", "ATR", "持仓", "冷却", "平均收益", "平均PF", "胜率%", "最差")
		fmt.Println("──────────────────────────────────────────────────────────────────────────────────────────")
		limit = 15
		if len(list) < limit {
			limit = len(list)
		}
		for i := 0; i < limit; i++ {
			r := list[i]
			fmt.Printf("%-16s %+.2f  %-4.1f %-4d %-3d | %+6.2f%% %5.2f  %5.1f%% %+7.2f%% | %s\n",
				r.key.modName, r.key.thresh, r.key.atr, r.key.minHold, r.key.cooldown,
				r.avgRet, r.avgPF, r.avgWR, r.minRet, r.detail)
		}

		// 按模块组合汇总
		fmt.Printf("\n📋 各模块组合平均表现:\n")
		modAgg := map[string]struct {
			ret   float64
			count int
		}{}
		for _, r := range list {
			a := modAgg[r.key.modName]
			a.ret += r.avgRet
			a.count++
			modAgg[r.key.modName] = a
		}
		type modRank struct {
			name   string
			avgRet float64
		}
		var modList []modRank
		for name, a := range modAgg {
			modList = append(modList, modRank{name, a.ret / float64(a.count)})
		}
		sort.Slice(modList, func(i, j int) bool { return modList[i].avgRet > modList[j].avgRet })
		for _, m := range modList {
			fmt.Printf("  %-20s 平均收益 %+.2f%%\n", m.name, m.avgRet)
		}
	}
}
