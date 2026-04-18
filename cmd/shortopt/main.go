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
		label       string
		thresh      float64
		atr         float64
		minHold     int
		cooldown    int
		activatePct float64 // ATR止损激活门槛
		minScore    float64 // 最小信号强度
		volMin      float64 // 最小波动率%
	}

	paramSets := []paramSet{
		// 基线
		{"基线(原)", -0.25, 2.5, 8, 12, 0, 0, 0},

		// === 优化1: ATR止损激活门槛 ===
		// 只有浮盈达到X%才启用trailing stop,避免刚入场就被止损
		{"ATR激活0.5%", -0.25, 2.5, 8, 12, 0.5, 0, 0},
		{"ATR激活1.0%", -0.25, 2.5, 8, 12, 1.0, 0, 0},
		{"ATR激活1.5%", -0.25, 2.5, 8, 12, 1.5, 0, 0},
		{"ATR激活2.0%", -0.25, 2.5, 8, 12, 2.0, 0, 0},

		// === 优化2: 最小信号强度 ===
		// 只有信号绝对值足够强才开空
		{"信号>0.30", -0.25, 2.5, 8, 12, 0, 0.30, 0},
		{"信号>0.35", -0.25, 2.5, 8, 12, 0, 0.35, 0},
		{"信号>0.40", -0.25, 2.5, 8, 12, 0, 0.40, 0},

		// === 优化3: 波动率过滤 ===
		// ATR/price < X% 时不开空(过滤横盘低波动)
		{"波动>0.3%", -0.25, 2.5, 8, 12, 0, 0, 0.30},
		{"波动>0.5%", -0.25, 2.5, 8, 12, 0, 0, 0.50},
		{"波动>0.7%", -0.25, 2.5, 8, 12, 0, 0, 0.70},
		{"波动>1.0%", -0.25, 2.5, 8, 12, 0, 0, 1.00},

		// === 组合优化 ===
		{"激活1+信号0.30", -0.25, 2.5, 8, 12, 1.0, 0.30, 0},
		{"激活1+波动0.5", -0.25, 2.5, 8, 12, 1.0, 0, 0.50},
		{"信号0.30+波动0.5", -0.25, 2.5, 8, 12, 0, 0.30, 0.50},
		{"三合一A", -0.25, 2.5, 8, 12, 1.0, 0.30, 0.50},
		{"三合一B", -0.25, 2.5, 8, 12, 0.5, 0.30, 0.30},
		{"三合一C", -0.25, 2.5, 8, 12, 1.5, 0.35, 0.50},

		// === 组合 + 参数微调 ===
		{"激活1+阈-0.20", -0.20, 2.5, 8, 12, 1.0, 0, 0},
		{"激活1+阈-0.30", -0.30, 2.5, 8, 12, 1.0, 0, 0},
		{"激活1+ATR3.0", -0.25, 3.0, 8, 12, 1.0, 0, 0},
		{"激活1+ATR2.0", -0.25, 2.0, 8, 12, 1.0, 0, 0},
		{"激活1+cd8", -0.25, 2.5, 8, 8, 1.0, 0, 0},
		{"激活1+hold6", -0.25, 2.5, 6, 12, 1.0, 0, 0},

		// 最有希望的组合
		{"全优A", -0.25, 2.5, 8, 12, 1.0, 0.30, 0.30},
		{"全优B", -0.25, 3.0, 8, 12, 1.0, 0.30, 0.30},
		{"全优C", -0.20, 2.5, 6, 8, 1.0, 0.25, 0.30},
		{"全优D", -0.25, 2.5, 6, 8, 1.5, 0.30, 0.50},
		{"全优E", -0.25, 2.0, 8, 12, 1.0, 0.30, 0.30},
	}

	fmt.Printf("╔══════════════════════════════════════════════════════════════╗\n")
	fmt.Printf("║  BTC 1h 做空优化 — ATR激活+信号过滤+波动率门槛           ║\n")
	fmt.Printf("╚══════════════════════════════════════════════════════════════╝\n\n")

	type result struct {
		label   string
		pd      string
		trades  int
		winRate float64
		retPct  float64
		pf      float64
		fees    float64
		stopW   int
		stopL   int
		sigW    int
		sigL    int
	}
	var allResults []result

	for _, pd := range periods {
		klines, err := backtest.LoadKlinesFromStore(ctx, store, sym, iv, pd.start, end)
		if err != nil || len(klines) < 100 {
			continue
		}
		htfKlines, _ := backtest.LoadKlinesFromStore(ctx, store, sym, "1d", pd.start, end)
		fmt.Printf("  加载 %s: %d根K线\n", pd.name, len(klines))

		for _, ps := range paramSets {
			tcfg := map[string]interface{}{
				"short_enabled": true,
				"htf_enabled":   true, "htf_interval": "1d", "htf_period": 5,
				"trend_filter":        false,
				"short_threshold":     ps.thresh,
				"cover_threshold":     -ps.thresh * 0.6,
				"short_confirm_bars":  1,
				"short_min_hold_bars": ps.minHold,
				"short_atr_stop_mult": ps.atr,
				"short_cooldown_bars": ps.cooldown,
				// New optimization params
				"short_atr_stop_activate_pct": ps.activatePct,
				"short_min_score_abs":         ps.minScore,
				"short_atr_volatility_min":    ps.volMin,
				// Long params (unused but needed)
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
			if sm.TotalTrades == 0 {
				continue
			}

			// 统计止损 vs 信号平仓
			var stopWins, stopLosses, sigWins, sigLosses int
			var shortP, shortQ float64
			for _, tr := range res.ShortTrades {
				if tr.Side == "SHORT" {
					shortP = tr.Price
					shortQ = tr.Quantity
				} else if tr.Side == "COVER" && shortQ > 0 {
					pnl := (shortP - tr.Price) * shortQ
					isStop := len(tr.Reason) > 10 && tr.Reason[:10] == "Short ATR "
					if pnl > 0 {
						if isStop {
							stopWins++
						} else {
							sigWins++
						}
					} else {
						if isStop {
							stopLosses++
						} else {
							sigLosses++
						}
					}
					shortQ = 0
				}
			}

			allResults = append(allResults, result{
				label: ps.label, pd: pd.name,
				trades: sm.TotalTrades, winRate: sm.WinRate * 100,
				retPct: sm.TotalReturnPct, pf: sm.ProfitFactor,
				fees:  sm.TotalFees,
				stopW: stopWins, stopL: stopLosses,
				sigW: sigWins, sigL: sigLosses,
			})
		}
	}

	// 聚合跨时间段
	type paramKey struct {
		label string
	}
	type agg struct {
		totalRet                                     float64
		totalPF                                      float64
		totalWR                                      float64
		totalFees                                    float64
		count                                        int
		minRet                                       float64
		maxRet                                       float64
		t90                                          int
		t365                                         int
		totalStopW, totalStopL, totalSigW, totalSigL int
		details                                      string
	}
	aggMap := map[string]*agg{}

	for _, r := range allResults {
		a, ok := aggMap[r.label]
		if !ok {
			a = &agg{minRet: 999, maxRet: -999}
			aggMap[r.label] = a
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
		a.totalStopW += r.stopW
		a.totalStopL += r.stopL
		a.totalSigW += r.sigW
		a.totalSigL += r.sigL
		if r.pd == "90d" {
			a.t90 = r.trades
		}
		if r.pd == "365d" {
			a.t365 = r.trades
		}
		a.details += fmt.Sprintf("[%s:%+.1f%%/%d笔] ", r.pd, r.retPct, r.trades)
	}

	type ranked struct {
		label  string
		avgRet float64
		avgPF  float64
		avgWR  float64
		minRet float64
		maxRet float64
		spread float64
		t90    int
		t365   int
		stopWR float64
		sigWR  float64
		detail string
	}
	var list []ranked
	for label, a := range aggMap {
		if a.count < 3 {
			continue
		}
		avg := a.totalRet / float64(a.count)
		stopTotal := a.totalStopW + a.totalStopL
		sigTotal := a.totalSigW + a.totalSigL
		swr := 0.0
		if stopTotal > 0 {
			swr = float64(a.totalStopW) / float64(stopTotal) * 100
		}
		sgr := 0.0
		if sigTotal > 0 {
			sgr = float64(a.totalSigW) / float64(sigTotal) * 100
		}
		list = append(list, ranked{
			label: label, avgRet: avg,
			avgPF:  a.totalPF / float64(a.count),
			avgWR:  a.totalWR / float64(a.count),
			minRet: a.minRet, maxRet: a.maxRet,
			spread: a.maxRet - a.minRet,
			t90:    a.t90, t365: a.t365,
			stopWR: swr, sigWR: sgr,
			detail: a.details,
		})
	}

	// 按年化收益排序 (365d收益 ≈ 年化)
	sort.Slice(list, func(i, j int) bool { return list[i].avgRet > list[j].avgRet })

	fmt.Printf("\n🏆 收益排名:\n")
	fmt.Printf("%-18s | %7s %5s %5s | %7s %7s %5s | %6s %6s | 明细\n",
		"配置", "平均收益", "PF", "胜率%", "最优", "最差", "差幅", "止损WR", "信号WR")
	fmt.Println("────────────────────────────────────────────────────────────────────────────────────────────────────")
	for _, r := range list {
		marker := "  "
		if r.avgRet > 30 {
			marker = "🔥"
		} else if r.avgRet > 20 {
			marker = "✅"
		} else if r.avgRet > 10 {
			marker = "  "
		}
		fmt.Printf("%s%-18s | %+6.2f%% %4.2f  %4.1f%% | %+6.2f%% %+6.2f%% %4.1f%% | %5.1f%% %5.1f%% | %s\n",
			marker, r.label,
			r.avgRet, r.avgPF, r.avgWR,
			r.maxRet, r.minRet, r.spread,
			r.stopWR, r.sigWR, r.detail)
	}

	// 稳定性排名（365d收益 > 年化50%目标）
	fmt.Printf("\n📊 365d收益排名 (年化50%%目标):\n")
	// 按365d收益排序
	type ret365 struct {
		label  string
		ret    float64
		detail string
	}
	var r365 []ret365
	for _, r := range allResults {
		if r.pd == "365d" {
			r365 = append(r365, ret365{r.label, r.retPct, ""})
		}
	}
	sort.Slice(r365, func(i, j int) bool { return r365[i].ret > r365[j].ret })
	for _, r := range r365 {
		marker := "❌"
		if r.ret >= 50 {
			marker = "🔥"
		} else if r.ret >= 30 {
			marker = "✅"
		} else if r.ret >= 20 {
			marker = "⚠️"
		}
		fmt.Printf("  %s %-18s 365d收益: %+.2f%%\n", marker, r.label, r.ret)
	}

	// 额外: 计算最优配置的每月详情
	fmt.Printf("\n\n📅 最有希望配置的365d逐月PnL:\n")
	bestLabels := []string{"基线(原)", "ATR激活1.0%", "ATR激活1.5%", "全优A", "全优C"}
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
				"short_threshold": ps.thresh, "cover_threshold": -ps.thresh * 0.6,
				"short_confirm_bars": 1, "short_min_hold_bars": ps.minHold,
				"short_atr_stop_mult": ps.atr, "short_cooldown_bars": ps.cooldown,
				"short_atr_stop_activate_pct": ps.activatePct,
				"short_min_score_abs":         ps.minScore,
				"short_atr_volatility_min":    ps.volMin,
				"buy_threshold":               0.20, "sell_threshold": -0.30,
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

			fmt.Printf("  【%s】365d: %+.2f%%\n", bl, res.ShortMetrics.TotalReturnPct)
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
