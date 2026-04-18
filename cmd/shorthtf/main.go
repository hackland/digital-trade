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

	// 核心测试: HTF 周期 × 做空参数
	htfPeriods := []int{5, 10, 15, 20, 30}
	thresholds := []float64{-0.20, -0.25, -0.30}
	atrMults := []float64{2.0, 2.5, 3.0}

	fmt.Printf("╔══════════════════════════════════════════════════════════╗\n")
	fmt.Printf("║  BTC 1h HTF周期优化 — 核心问题: 牛市月份过滤          ║\n")
	fmt.Printf("╚══════════════════════════════════════════════════════════╝\n\n")

	type result struct {
		htfP    int
		thresh  float64
		atr     float64
		pd      string
		trades  int
		retPct  float64
		pf      float64
		winRate float64
		fees    float64
		months  string
	}

	var allResults []result

	for _, pd := range periods {
		klines, err := backtest.LoadKlinesFromStore(ctx, store, sym, iv, pd.start, end)
		if err != nil || len(klines) < 100 {
			continue
		}
		htfKlines, _ := backtest.LoadKlinesFromStore(ctx, store, sym, "1d", pd.start, end)
		fmt.Printf("  加载 %s: %d根K线, %d根日线\n", pd.name, len(klines), len(htfKlines))

		for _, hp := range htfPeriods {
			for _, th := range thresholds {
				for _, am := range atrMults {
					tcfg := map[string]interface{}{
						"short_enabled": true,
						"htf_enabled":   true, "htf_interval": "1d", "htf_period": hp,
						"trend_filter":        false,
						"short_threshold":     th,
						"cover_threshold":     -th * 0.6,
						"short_confirm_bars":  1,
						"short_min_hold_bars": 8,
						"short_atr_stop_mult": am,
						"short_cooldown_bars": 12,
						"buy_threshold":       0.20, "sell_threshold": -0.30,
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
					if err != nil || res.ShortMetrics.TotalTrades == 0 {
						continue
					}

					sm := res.ShortMetrics

					// 月度分布
					monthPnL := map[string]float64{}
					var shortP, shortQ float64
					for _, tr := range res.ShortTrades {
						if tr.Side == "SHORT" {
							shortP = tr.Price
							shortQ = tr.Quantity
						} else if tr.Side == "COVER" && shortQ > 0 {
							month := tr.Timestamp.Format("2006-01")
							monthPnL[month] += (shortP - tr.Price) * shortQ
							shortQ = 0
						}
					}
					// 统计亏损月数
					lossingMonths := 0
					profitMonths := 0
					for _, pnl := range monthPnL {
						if pnl < 0 {
							lossingMonths++
						} else if pnl > 50 {
							profitMonths++
						}
					}
					monthStr := fmt.Sprintf("%d盈/%d亏/%d总月", profitMonths, lossingMonths, len(monthPnL))

					allResults = append(allResults, result{
						htfP: hp, thresh: th, atr: am, pd: pd.name,
						trades: sm.TotalTrades, retPct: sm.TotalReturnPct,
						pf: sm.ProfitFactor, winRate: sm.WinRate * 100,
						fees: sm.TotalFees, months: monthStr,
					})
				}
			}
		}
	}

	// 聚合跨时间段
	type paramKey struct {
		htfP   int
		thresh float64
		atr    float64
	}
	type agg struct {
		totalRet   float64
		totalPF    float64
		totalWR    float64
		totalFees  float64
		count      int
		minRet     float64
		maxRet     float64
		details    string
		trades90d  int
		trades365d int
	}
	aggMap := map[paramKey]*agg{}

	for _, r := range allResults {
		pk := paramKey{r.htfP, r.thresh, r.atr}
		a, ok := aggMap[pk]
		if !ok {
			a = &agg{minRet: 999, maxRet: -999}
			aggMap[pk] = a
		}
		a.totalRet += r.retPct
		a.totalPF += r.pf
		a.totalWR += r.winRate
		a.totalFees += r.fees
		a.count++
		if r.retPct < a.minRet {
			a.minRet = r.retPct
		}
		if r.retPct > a.maxRet {
			a.maxRet = r.retPct
		}
		a.details += fmt.Sprintf("[%s:%+.1f%%/%d笔/$%.0f费/%s] ", r.pd, r.retPct, r.trades, r.fees, r.months)
		if r.pd == "90d" {
			a.trades90d = r.trades
		}
		if r.pd == "365d" {
			a.trades365d = r.trades
		}
	}

	type ranked struct {
		key    paramKey
		avgRet float64
		avgPF  float64
		avgWR  float64
		minRet float64
		maxRet float64
		spread float64 // maxRet - minRet, 越小越稳定
		count  int
		detail string
		t90    int
		t365   int
	}
	var list []ranked
	for pk, a := range aggMap {
		if a.count >= 3 {
			avg := a.totalRet / float64(a.count)
			list = append(list, ranked{
				key: pk, avgRet: avg,
				avgPF:  a.totalPF / float64(a.count),
				avgWR:  a.totalWR / float64(a.count),
				minRet: a.minRet, maxRet: a.maxRet,
				spread: a.maxRet - a.minRet,
				count:  a.count, detail: a.details,
				t90: a.trades90d, t365: a.trades365d,
			})
		}
	}

	// 按 365d 收益稳定性排序 (spread 越小越好，同时平均收益要正)
	// 先按平均收益
	sort.Slice(list, func(i, j int) bool { return list[i].avgRet > list[j].avgRet })

	fmt.Printf("\n🏆 收益 TOP 20 (按平均收益):\n")
	fmt.Printf("%-4s %-6s %-4s | %7s %6s %5s | %8s %8s %6s | 90d笔 365d笔 | 明细\n",
		"HTF", "阈值", "ATR", "平均收益", "平均PF", "胜率%", "最优", "最差", "差幅")
	fmt.Println("─────────────────────────────────────────────────────────────────────────────────────────────────")
	limit := 20
	if len(list) < limit {
		limit = len(list)
	}
	for i := 0; i < limit; i++ {
		r := list[i]
		fmt.Printf("%-4d %+.2f  %-4.1f | %+6.2f%% %5.2f  %5.1f | %+7.2f%% %+7.2f%% %5.1f%% | %-5d %-6d | %s\n",
			r.key.htfP, r.key.thresh, r.key.atr,
			r.avgRet, r.avgPF, r.avgWR,
			r.maxRet, r.minRet, r.spread,
			r.t90, r.t365, r.detail)
	}

	// 按稳定性排序 (差幅小 + 平均收益正)
	sort.Slice(list, func(i, j int) bool {
		// 只要平均收益>5%的，按差幅排序
		if list[i].avgRet > 5 && list[j].avgRet > 5 {
			return list[i].spread < list[j].spread
		}
		return list[i].avgRet > list[j].avgRet
	})

	fmt.Printf("\n📊 稳定性排名 (差幅最小，平均收益>5%%):\n")
	fmt.Printf("%-4s %-6s %-4s | %7s %6s %5s | %8s %8s %6s | 90d笔 365d笔\n",
		"HTF", "阈值", "ATR", "平均收益", "平均PF", "胜率%", "最优", "最差", "差幅")
	fmt.Println("─────────────────────────────────────────────────────────────────────────────────")
	limit = 20
	if len(list) < limit {
		limit = len(list)
	}
	for i := 0; i < limit; i++ {
		r := list[i]
		fmt.Printf("%-4d %+.2f  %-4.1f | %+6.2f%% %5.2f  %5.1f | %+7.2f%% %+7.2f%% %5.1f%% | %-5d %-6d\n",
			r.key.htfP, r.key.thresh, r.key.atr,
			r.avgRet, r.avgPF, r.avgWR,
			r.maxRet, r.minRet, r.spread,
			r.t90, r.t365)
	}

	// 按 HTF 周期汇总
	fmt.Printf("\n📋 各 HTF 周期平均表现:\n")
	htfAgg := map[int]struct {
		ret    float64
		spread float64
		count  int
	}{}
	for _, r := range list {
		a := htfAgg[r.key.htfP]
		a.ret += r.avgRet
		a.spread += r.spread
		a.count++
		htfAgg[r.key.htfP] = a
	}
	for _, hp := range htfPeriods {
		a := htfAgg[hp]
		if a.count > 0 {
			fmt.Printf("  HTF=%2d 日EMA: 平均收益 %+.2f%%, 平均差幅 %.1f%%, (%d组)\n",
				hp, a.ret/float64(a.count), a.spread/float64(a.count), a.count)
		}
	}
}
