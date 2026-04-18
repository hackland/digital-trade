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

// 做空策略配置方案
type shortPlan struct {
	name   string
	config map[string]interface{}
}

func main() {
	logger, _ := zap.NewProduction()
	defer logger.Sync()

	cfg, err := config.Load("configs/config.yaml")
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

	// 测试参数
	symbols := []string{"BTCUSDT", "ETHUSDT"}
	intervals := []string{"4h"} // 4h 明显优于 1h

	// 测试多个时间段
	type period struct {
		name  string
		start time.Time
		end   time.Time
	}
	now := time.Now().UTC()
	periods := []period{
		{"全年(365d)", now.Add(-365 * 24 * time.Hour), now},
		{"近半年(180d)", now.Add(-180 * 24 * time.Hour), now},
		{"近3月(90d)", now.Add(-90 * 24 * time.Hour), now},
	}

	// 做空策略方案列表 — 系统性测试关键变量
	plans := []shortPlan{
		// ==========================================
		// A 组: 无过滤器，测试原始信号质量
		// ==========================================
		{
			name: "A1_raw_rsi_macd",
			config: baseConfig(map[string]interface{}{
				"short_enabled": true, "htf_enabled": false, "trend_filter": false,
				"short_threshold": -0.25, "cover_threshold": 0.15,
				"short_confirm_bars": 2, "short_min_hold_bars": 4,
				"short_atr_stop_mult": 3.0, "short_cooldown_bars": 8,
				"modules": []interface{}{
					map[string]interface{}{"name": "rsi", "weight": 0.40},
					map[string]interface{}{"name": "macd", "weight": 0.35},
					map[string]interface{}{"name": "bb_position", "weight": 0.15},
					map[string]interface{}{"name": "mfi", "weight": 0.10},
				},
			}),
		},
		{
			name: "A2_raw_vol_bias",
			config: baseConfig(map[string]interface{}{
				"short_enabled": true, "htf_enabled": false, "trend_filter": false,
				"short_threshold": -0.25, "cover_threshold": 0.15,
				"short_confirm_bars": 2, "short_min_hold_bars": 4,
				"short_atr_stop_mult": 3.0, "short_cooldown_bars": 8,
				"modules": []interface{}{
					map[string]interface{}{"name": "mfi", "weight": 0.30},
					map[string]interface{}{"name": "cmf", "weight": 0.20},
					map[string]interface{}{"name": "rsi", "weight": 0.25},
					map[string]interface{}{"name": "macd", "weight": 0.25},
				},
			}),
		},
		{
			name: "A3_raw_ema_macd",
			config: baseConfig(map[string]interface{}{
				"short_enabled": true, "htf_enabled": false, "trend_filter": false,
				"short_threshold": -0.25, "cover_threshold": 0.15,
				"short_confirm_bars": 2, "short_min_hold_bars": 4,
				"short_atr_stop_mult": 3.0, "short_cooldown_bars": 8,
				"modules": []interface{}{
					map[string]interface{}{"name": "ema_cross", "weight": 0.40},
					map[string]interface{}{"name": "macd", "weight": 0.35},
					map[string]interface{}{"name": "rsi", "weight": 0.25},
				},
			}),
		},
		// ==========================================
		// B 组: 只用短周期 EMA 趋势过滤
		// ==========================================
		{
			name: "B1_ema20_filter",
			config: baseConfig(map[string]interface{}{
				"short_enabled": true, "htf_enabled": false,
				"trend_filter": true, "trend_period": 20,
				"short_threshold": -0.25, "cover_threshold": 0.15,
				"short_confirm_bars": 2, "short_min_hold_bars": 4,
				"short_atr_stop_mult": 3.0, "short_cooldown_bars": 8,
				"modules": []interface{}{
					map[string]interface{}{"name": "rsi", "weight": 0.35},
					map[string]interface{}{"name": "macd", "weight": 0.35},
					map[string]interface{}{"name": "bb_position", "weight": 0.15},
					map[string]interface{}{"name": "mfi", "weight": 0.15},
				},
			}),
		},
		{
			name: "B2_ema50_filter",
			config: baseConfig(map[string]interface{}{
				"short_enabled": true, "htf_enabled": false,
				"trend_filter": true, "trend_period": 50,
				"short_threshold": -0.25, "cover_threshold": 0.15,
				"short_confirm_bars": 2, "short_min_hold_bars": 4,
				"short_atr_stop_mult": 3.0, "short_cooldown_bars": 8,
				"modules": []interface{}{
					map[string]interface{}{"name": "rsi", "weight": 0.35},
					map[string]interface{}{"name": "macd", "weight": 0.35},
					map[string]interface{}{"name": "bb_position", "weight": 0.15},
					map[string]interface{}{"name": "mfi", "weight": 0.15},
				},
			}),
		},
		// ==========================================
		// C 组: 阈值和止损调优
		// ==========================================
		{
			name: "C1_strict_entry",
			config: baseConfig(map[string]interface{}{
				"short_enabled": true, "htf_enabled": false, "trend_filter": true, "trend_period": 20,
				"short_threshold": -0.40, "cover_threshold": 0.20,
				"short_confirm_bars": 3, "short_min_hold_bars": 6,
				"short_atr_stop_mult": 3.5, "short_cooldown_bars": 12,
				"modules": []interface{}{
					map[string]interface{}{"name": "rsi", "weight": 0.35},
					map[string]interface{}{"name": "macd", "weight": 0.35},
					map[string]interface{}{"name": "bb_position", "weight": 0.15},
					map[string]interface{}{"name": "mfi", "weight": 0.15},
				},
			}),
		},
		{
			name: "C2_tight_stop",
			config: baseConfig(map[string]interface{}{
				"short_enabled": true, "htf_enabled": false, "trend_filter": true, "trend_period": 20,
				"short_threshold": -0.25, "cover_threshold": 0.15,
				"short_confirm_bars": 2, "short_min_hold_bars": 3,
				"short_atr_stop_mult": 2.0, "short_cooldown_bars": 6,
				"modules": []interface{}{
					map[string]interface{}{"name": "rsi", "weight": 0.35},
					map[string]interface{}{"name": "macd", "weight": 0.35},
					map[string]interface{}{"name": "bb_position", "weight": 0.15},
					map[string]interface{}{"name": "mfi", "weight": 0.15},
				},
			}),
		},
		{
			name: "C3_wide_stop",
			config: baseConfig(map[string]interface{}{
				"short_enabled": true, "htf_enabled": false, "trend_filter": true, "trend_period": 20,
				"short_threshold": -0.30, "cover_threshold": 0.20,
				"short_confirm_bars": 2, "short_min_hold_bars": 6,
				"short_atr_stop_mult": 5.0, "short_cooldown_bars": 10,
				"modules": []interface{}{
					map[string]interface{}{"name": "rsi", "weight": 0.35},
					map[string]interface{}{"name": "macd", "weight": 0.35},
					map[string]interface{}{"name": "bb_position", "weight": 0.15},
					map[string]interface{}{"name": "mfi", "weight": 0.15},
				},
			}),
		},
		// ==========================================
		// D 组: HTF 日线过滤（只在日线熊市做空）
		// ==========================================
		{
			name: "D1_htf10_noTF",
			config: baseConfig(map[string]interface{}{
				"short_enabled": true, "trend_filter": false,
				"htf_enabled": true, "htf_interval": "1d", "htf_period": 10,
				"short_threshold": -0.25, "cover_threshold": 0.15,
				"short_confirm_bars": 2, "short_min_hold_bars": 4,
				"short_atr_stop_mult": 3.0, "short_cooldown_bars": 8,
				"modules": []interface{}{
					map[string]interface{}{"name": "rsi", "weight": 0.35},
					map[string]interface{}{"name": "macd", "weight": 0.35},
					map[string]interface{}{"name": "bb_position", "weight": 0.15},
					map[string]interface{}{"name": "mfi", "weight": 0.15},
				},
			}),
		},
		{
			name: "D2_htf5_noTF",
			config: baseConfig(map[string]interface{}{
				"short_enabled": true, "trend_filter": false,
				"htf_enabled": true, "htf_interval": "1d", "htf_period": 5,
				"short_threshold": -0.25, "cover_threshold": 0.15,
				"short_confirm_bars": 2, "short_min_hold_bars": 4,
				"short_atr_stop_mult": 3.0, "short_cooldown_bars": 8,
				"modules": []interface{}{
					map[string]interface{}{"name": "rsi", "weight": 0.35},
					map[string]interface{}{"name": "macd", "weight": 0.35},
					map[string]interface{}{"name": "bb_position", "weight": 0.15},
					map[string]interface{}{"name": "mfi", "weight": 0.15},
				},
			}),
		},
		{
			name: "D3_htf10+ema20",
			config: baseConfig(map[string]interface{}{
				"short_enabled": true,
				"htf_enabled":   true, "htf_interval": "1d", "htf_period": 10,
				"trend_filter": true, "trend_period": 20,
				"short_threshold": -0.25, "cover_threshold": 0.15,
				"short_confirm_bars": 2, "short_min_hold_bars": 4,
				"short_atr_stop_mult": 3.0, "short_cooldown_bars": 8,
				"modules": []interface{}{
					map[string]interface{}{"name": "rsi", "weight": 0.35},
					map[string]interface{}{"name": "macd", "weight": 0.35},
					map[string]interface{}{"name": "bb_position", "weight": 0.15},
					map[string]interface{}{"name": "mfi", "weight": 0.15},
				},
			}),
		},
		// ==========================================
		// E 组: 最佳组合微调
		// ==========================================
		{
			name: "E1_balanced",
			config: baseConfig(map[string]interface{}{
				"short_enabled": true, "htf_enabled": false,
				"trend_filter": true, "trend_period": 20,
				"short_threshold": -0.30, "cover_threshold": 0.20,
				"short_confirm_bars": 2, "short_min_hold_bars": 5,
				"short_atr_stop_mult": 3.5, "short_cooldown_bars": 10,
				"modules": []interface{}{
					map[string]interface{}{"name": "rsi", "weight": 0.30},
					map[string]interface{}{"name": "macd", "weight": 0.30},
					map[string]interface{}{"name": "bb_position", "weight": 0.20},
					map[string]interface{}{"name": "mfi", "weight": 0.20},
				},
			}),
		},
		{
			name: "E2_rsi_heavy",
			config: baseConfig(map[string]interface{}{
				"short_enabled": true, "htf_enabled": false,
				"trend_filter": true, "trend_period": 20,
				"short_threshold": -0.30, "cover_threshold": 0.20,
				"short_confirm_bars": 2, "short_min_hold_bars": 5,
				"short_atr_stop_mult": 3.5, "short_cooldown_bars": 10,
				"modules": []interface{}{
					map[string]interface{}{"name": "rsi", "weight": 0.45},
					map[string]interface{}{"name": "macd", "weight": 0.25},
					map[string]interface{}{"name": "bb_position", "weight": 0.15},
					map[string]interface{}{"name": "mfi", "weight": 0.15},
				},
			}),
		},
		{
			name: "E3_kdj_added",
			config: baseConfig(map[string]interface{}{
				"short_enabled": true, "htf_enabled": false,
				"trend_filter": true, "trend_period": 20,
				"short_threshold": -0.30, "cover_threshold": 0.20,
				"short_confirm_bars": 2, "short_min_hold_bars": 5,
				"short_atr_stop_mult": 3.5, "short_cooldown_bars": 10,
				"modules": []interface{}{
					map[string]interface{}{"name": "rsi", "weight": 0.25},
					map[string]interface{}{"name": "macd", "weight": 0.25},
					map[string]interface{}{"name": "kdj", "weight": 0.20},
					map[string]interface{}{"name": "bb_position", "weight": 0.15},
					map[string]interface{}{"name": "mfi", "weight": 0.15},
				},
			}),
		},
	}

	// 运行回测
	type result struct {
		plan     string
		symbol   string
		interval string
		period   string
		trades   int
		winRate  float64
		retPct   float64
		maxDD    float64
		sharpe   float64
		avgWin   float64
		avgLoss  float64
		pf       float64
	}

	var results []result

	for _, sym := range symbols {
		for _, iv := range intervals {
			for _, pd := range periods {
				fmt.Printf("\n========== %s %s %s ==========\n", sym, iv, pd.name)

				klines, err := backtest.LoadKlinesFromStore(ctx, store, sym, iv, pd.start, pd.end)
				if err != nil || len(klines) < 100 {
					fmt.Printf("  ⚠️ 数据不足: %d 条, 跳过\n", len(klines))
					continue
				}

				htfKlines, _ := backtest.LoadKlinesFromStore(ctx, store, sym, "1d", pd.start, pd.end)

				for _, plan := range plans {
					planCfg := copyMap(plan.config)
					planCfg["interval"] = iv

					strat := trend.NewCustomWeightedStrategy()
					if err := strat.Init(planCfg); err != nil {
						fmt.Printf("  ❌ %s init error: %v\n", plan.name, err)
						continue
					}

					engineCfg := backtest.EngineConfig{
						Symbol:      sym,
						Interval:    iv,
						InitialCash: 10000,
						FeeRate:     0.001,
						AllocPct:    0.9,
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
						fmt.Printf("  ❌ %s run error: %v\n", plan.name, err)
						continue
					}

					sm := res.ShortMetrics
					results = append(results, result{
						plan:     plan.name,
						symbol:   sym,
						interval: iv,
						period:   pd.name,
						trades:   sm.TotalTrades,
						winRate:  sm.WinRate * 100,
						retPct:   sm.TotalReturnPct,
						maxDD:    sm.MaxDrawdownPct,
						sharpe:   sm.SharpeRatio,
						avgWin:   sm.AvgWin,
						avgLoss:  sm.AvgLoss,
						pf:       sm.ProfitFactor,
					})
				}
			}
		}
	}

	// 只打印有交易的结果
	fmt.Println("\n\n========== 做空策略回测结果（仅显示有交易的） ==========")
	fmt.Printf("%-18s %-8s %-10s %5s %7s %8s %7s %7s %6s\n",
		"策略", "币种", "区间", "交易", "胜率%", "收益%", "MaxDD%", "PF", "盈/亏")
	fmt.Println("─────────────────────────────────────────────────────────────────────────────────────")

	hasResult := false
	for _, r := range results {
		if r.trades == 0 {
			continue
		}
		hasResult = true
		fmt.Printf("%-18s %-8s %-10s %5d %6.1f%% %+7.2f %6.2f%% %6.2f %+.0f/%.0f\n",
			r.plan, r.symbol, r.period, r.trades, r.winRate, r.retPct, r.maxDD, r.pf, r.avgWin, r.avgLoss)
	}
	if !hasResult {
		fmt.Println("（所有策略 0 交易 — 过滤器可能太严）")
	}

	// 过滤有交易的
	var active []result
	for _, r := range results {
		if r.trades > 0 {
			active = append(active, r)
		}
	}

	if len(active) > 0 {
		sort.Slice(active, func(i, j int) bool {
			return active[i].retPct > active[j].retPct
		})
		fmt.Println("\n🏆 收益率 TOP 10:")
		for i, r := range active {
			if i >= 10 {
				break
			}
			fmt.Printf("  %d. [%s] %s %s — 收益 %+.2f%%, 胜率 %.1f%%, %d 笔, PF %.2f\n",
				i+1, r.plan, r.symbol, r.period, r.retPct, r.winRate, r.trades, r.pf)
		}

		sort.Slice(active, func(i, j int) bool {
			return active[i].pf > active[j].pf
		})
		fmt.Println("\n📊 盈亏比 (PF) TOP 10:")
		for i, r := range active {
			if i >= 10 {
				break
			}
			fmt.Printf("  %d. [%s] %s %s — PF %.2f, 收益 %+.2f%%, 胜率 %.1f%%, %d 笔\n",
				i+1, r.plan, r.symbol, r.period, r.pf, r.retPct, r.winRate, r.trades)
		}
	}

	// 统计：各策略跨区间汇总
	fmt.Println("\n📋 各策略跨所有测试汇总:")
	planAgg := map[string]struct {
		totalRet float64
		count    int
	}{}
	for _, r := range results {
		if r.trades == 0 {
			continue
		}
		a := planAgg[r.plan]
		a.totalRet += r.retPct
		a.count++
		planAgg[r.plan] = a
	}
	for name, a := range planAgg {
		fmt.Printf("  [%s] %d 个测试有交易, 平均收益 %+.2f%%\n", name, a.count, a.totalRet/float64(a.count))
	}
}

// baseConfig 返回基础策略配置，做空参数由 overrides 覆盖
func baseConfig(overrides map[string]interface{}) map[string]interface{} {
	base := map[string]interface{}{
		// 做多参数（保留但不是重点）
		"buy_threshold":  0.20,
		"sell_threshold": -0.30,
		"confirm_bars":   1,
		"cooldown_bars":  12,
		"min_hold_bars":  18,
		"atr_stop_mult":  4.0,
		"atr_period":     14,
	}
	for k, v := range overrides {
		base[k] = v
	}
	return base
}

func copyMap(m map[string]interface{}) map[string]interface{} {
	cp := make(map[string]interface{}, len(m))
	for k, v := range m {
		cp[k] = v
	}
	return cp
}
