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
		label    string
		atrMult  float64 // 0 = 禁用ATR止损
		thresh   float64
		cover    float64
		minHold  int
		cooldown int
		activPct float64
	}

	paramSets := []paramSet{
		// 基线
		{"基线 ATR=2.5", 2.5, -0.25, 0.15, 8, 12, 0},

		// 禁用ATR止损，纯信号出场
		{"无ATR止损", 0, -0.25, 0.15, 8, 12, 0},
		{"无ATR cover=0.10", 0, -0.25, 0.10, 8, 12, 0},
		{"无ATR cover=0.05", 0, -0.25, 0.05, 8, 12, 0},
		{"无ATR cover=0.20", 0, -0.25, 0.20, 8, 12, 0},

		// ATR很宽 (不易触发，只做保底)
		{"ATR=5.0", 5.0, -0.25, 0.15, 8, 12, 0},
		{"ATR=6.0", 6.0, -0.25, 0.15, 8, 12, 0},
		{"ATR=8.0", 8.0, -0.25, 0.15, 8, 12, 0},

		// ATR激活门槛 (浮盈后才启用)
		{"ATR=2.5 激活2%", 2.5, -0.25, 0.15, 8, 12, 2.0},
		{"ATR=2.5 激活3%", 2.5, -0.25, 0.15, 8, 12, 3.0},
		{"ATR=2.5 激活5%", 2.5, -0.25, 0.15, 8, 12, 5.0},

		// 无ATR + 参数调整
		{"无ATR th-0.20", 0, -0.20, 0.15, 8, 12, 0},
		{"无ATR th-0.30", 0, -0.30, 0.15, 8, 12, 0},
		{"无ATR hold=12", 0, -0.25, 0.15, 12, 12, 0},
		{"无ATR hold=6", 0, -0.25, 0.15, 6, 12, 0},
		{"无ATR cd=8", 0, -0.25, 0.15, 8, 8, 0},

		// 最优组合猜测
		{"无ATR th-0.25 cv=0.10 h=10", 0, -0.25, 0.10, 10, 12, 0},
		{"ATR=6 cv=0.10", 6.0, -0.25, 0.10, 8, 12, 0},
		{"ATR=5 激活3%", 5.0, -0.25, 0.15, 8, 12, 3.0},
	}

	fmt.Printf("╔══════════════════════════════════════════════════════════════╗\n")
	fmt.Printf("║  BTC 1h 做空出场方式优化 — ATR止损 vs 纯信号平仓          ║\n")
	fmt.Printf("╚══════════════════════════════════════════════════════════════╝\n\n")

	type result struct {
		label  string
		pd     string
		trades int
		retPct float64
		pf     float64
		wr     float64
		fees   float64
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
				"trend_filter":                false,
				"short_threshold":             ps.thresh,
				"cover_threshold":             ps.cover,
				"short_confirm_bars":          1,
				"short_min_hold_bars":         ps.minHold,
				"short_atr_stop_mult":         ps.atrMult,
				"short_cooldown_bars":         ps.cooldown,
				"short_atr_stop_activate_pct": ps.activPct,
				"buy_threshold":               0.20, "sell_threshold": -0.30,
				"confirm_bars": 1, "cooldown_bars": 12, "min_hold_bars": 18,
				"atr_stop_mult": 4.0, "atr_period": 14,
				"interval": iv, "modules": modules,
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
				fees: sm.TotalFees,
			})
		}
	}

	// 聚合
	type agg struct {
		totalRet       float64
		count          int
		minRet, maxRet float64
		t90, t365      int
		details        string
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
		label                  string
		avgRet, minRet, spread float64
		t90, t365              int
		detail                 string
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
	fmt.Printf("%-25s | %7s %7s %5s | 90d笔 365d笔 | 明细\n",
		"配置", "平均收益", "最差", "差幅")
	fmt.Println("──────────────────────────────────────────────────────────────────────────────────────")
	for _, r := range list {
		fmt.Printf("%-25s | %+6.2f%% %+6.2f%% %4.1f%% | %-5d %-6d | %s\n",
			r.label, r.avgRet, r.minRet, r.spread, r.t90, r.t365, r.detail)
	}

	// 最优对比的逐月
	fmt.Printf("\n📅 逐月对比 (365d):\n")
	bestLabels := []string{"基线 ATR=2.5", "无ATR止损", "无ATR cover=0.10", "ATR=6.0", "ATR=2.5 激活3%"}
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
				"short_threshold": ps.thresh, "cover_threshold": ps.cover,
				"short_confirm_bars": 1, "short_min_hold_bars": ps.minHold,
				"short_atr_stop_mult": ps.atrMult, "short_cooldown_bars": ps.cooldown,
				"short_atr_stop_activate_pct": ps.activPct,
				"buy_threshold":               0.20, "sell_threshold": -0.30,
				"confirm_bars": 1, "cooldown_bars": 12, "min_hold_bars": 18,
				"atr_stop_mult": 4.0, "atr_period": 14,
				"interval": iv, "modules": modules,
			}
			strat := trend.NewCustomWeightedStrategy()
			strat.Init(tcfg)
			engineCfg := backtest.EngineConfig{
				Symbol: sym, Interval: iv, InitialCash: 10000, FeeRate: 0.001, AllocPct: 0.9,
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
			lossM := 0
			for _, m := range months {
				mk := "✅"
				if monthPnL[m] < -50 {
					mk = "❌"
					lossM++
				} else if monthPnL[m] < 100 {
					mk = "⚠️"
				}
				fmt.Printf("    %s %s %3d笔 %+7.0f$\n", mk, m, monthTrades[m], monthPnL[m])
			}
			fmt.Printf("    亏损月: %d/%d (%.0f%%)\n\n", lossM, len(months), float64(lossM)/math.Max(1, float64(len(months)))*100)
		}
	}
}
