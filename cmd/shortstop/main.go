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

	modules := []interface{}{
		map[string]interface{}{"name": "macd", "weight": 0.40},
		map[string]interface{}{"name": "rsi", "weight": 0.40},
		map[string]interface{}{"name": "bb_position", "weight": 0.20},
	}

	// 基于前面最优参数: htf=5, confirm=1
	// 排列组合: ATR倍数 × 最短持仓 × 冷却 × 阈值
	atrMults := []float64{1.5, 2.0, 2.5, 3.0, 3.5, 4.0, 5.0}
	minHolds := []int{2, 3, 4, 6}
	cooldowns := []int{4, 6, 8, 12}
	thresholds := []float64{-0.10, -0.15, -0.20}

	type testCase struct {
		sym, iv, pd string
		thresh      float64
		atr         float64
		minHold     int
		cooldown    int
		trades      int
		winRate     float64
		retPct      float64
		pf          float64
	}
	var results []testCase

	type symIv struct {
		sym string
		iv  string
	}
	combos := []symIv{
		{"BTCUSDT", "4h"},
		{"ETHUSDT", "4h"},
		{"ETHUSDT", "1h"},
	}

	for _, si := range combos {
		for _, pd := range periods {
			klines, err := backtest.LoadKlinesFromStore(ctx, store, si.sym, si.iv, pd.start, end)
			if err != nil || len(klines) < 50 {
				continue
			}
			htfKlines, _ := backtest.LoadKlinesFromStore(ctx, store, si.sym, "1d", pd.start, end)

			fmt.Printf("测试 %s %s %s (%d K线)...\n", si.sym, si.iv, pd.name, len(klines))

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
								"interval": si.iv,
								"modules":  modules,
							}

							strat := trend.NewCustomWeightedStrategy()
							if err := strat.Init(tcfg); err != nil {
								continue
							}

							engineCfg := backtest.EngineConfig{
								Symbol: si.sym, Interval: si.iv,
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
							results = append(results, testCase{
								sym: si.sym, iv: si.iv, pd: pd.name,
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

	// 按品种+周期分组，找每组 TOP 10
	for _, si := range combos {
		fmt.Printf("\n\n==================== %s %s TOP 20 (跨所有时间段加权) ====================\n", si.sym, si.iv)

		// 聚合: 同一参数组合跨3个时间段的平均收益
		type paramKey struct {
			thresh   float64
			atr      float64
			minHold  int
			cooldown int
		}
		type aggResult struct {
			key      paramKey
			totalRet float64
			totalPF  float64
			totalWR  float64
			count    int
			minRet   float64
			details  string
		}
		agg := map[paramKey]*aggResult{}

		for _, r := range results {
			if r.sym != si.sym || r.iv != si.iv {
				continue
			}
			pk := paramKey{r.thresh, r.atr, r.minHold, r.cooldown}
			a, ok := agg[pk]
			if !ok {
				a = &aggResult{key: pk, minRet: 999}
				agg[pk] = a
			}
			a.totalRet += r.retPct
			a.totalPF += r.pf
			a.totalWR += r.winRate
			a.count++
			if r.retPct < a.minRet {
				a.minRet = r.retPct
			}
			a.details += fmt.Sprintf(" [%s: %+.1f%%/%d笔]", r.pd, r.retPct, r.trades)
		}

		// 转切片排序
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
		for _, a := range agg {
			if a.count >= 2 { // 至少跨 2 个时间段
				list = append(list, ranked{
					key: a.key, avgRet: a.totalRet / float64(a.count),
					avgPF: a.totalPF / float64(a.count), avgWR: a.totalWR / float64(a.count),
					minRet: a.minRet, count: a.count, detail: a.details,
				})
			}
		}

		// 按平均收益排序
		for i := 0; i < len(list); i++ {
			for j := i + 1; j < len(list); j++ {
				if list[j].avgRet > list[i].avgRet {
					list[i], list[j] = list[j], list[i]
				}
			}
		}

		fmt.Printf("%-6s %-4s %-4s %-3s | %7s %6s %6s %8s | 明细\n",
			"阈值", "ATR", "持仓", "冷却", "平均收益", "平均PF", "胜率%", "最差区间")
		fmt.Println("──────────────────────────────────────────────────────────────────────────────")

		limit := 20
		if len(list) < limit {
			limit = len(list)
		}
		for i := 0; i < limit; i++ {
			r := list[i]
			fmt.Printf("%+.2f  %-4.1f %-4d %-3d | %+6.2f%% %5.2f  %5.1f%% %+7.2f%% |%s\n",
				r.key.thresh, r.key.atr, r.key.minHold, r.key.cooldown,
				r.avgRet, r.avgPF, r.avgWR, r.minRet, r.detail)
		}

		// 稳定性排名 (最差区间收益最高)
		for i := 0; i < len(list); i++ {
			for j := i + 1; j < len(list); j++ {
				if list[j].minRet > list[i].minRet {
					list[i], list[j] = list[j], list[i]
				}
			}
		}
		fmt.Printf("\n📊 稳定性 TOP 10（最差区间收益最高）:\n")
		limit = 10
		if len(list) < limit {
			limit = len(list)
		}
		for i := 0; i < limit; i++ {
			r := list[i]
			fmt.Printf("  %d. thresh=%+.2f ATR=%.1f hold=%d cd=%d | 最差%+.2f%% 平均%+.2f%% PF=%.2f\n",
				i+1, r.key.thresh, r.key.atr, r.key.minHold, r.key.cooldown,
				r.minRet, r.avgRet, r.avgPF)
		}
	}
}
