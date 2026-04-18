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

	// 当前最优配置: MACD(60%) + EMA(40%)
	bestModules := []interface{}{
		map[string]interface{}{"name": "macd", "weight": 0.60},
		map[string]interface{}{"name": "ema_cross", "weight": 0.40},
	}

	// 同时测试几组参数做对比
	type paramSet struct {
		label    string
		thresh   float64
		atr      float64
		minHold  int
		cooldown int
		modules  []interface{}
	}
	paramSets := []paramSet{
		// 当前最优
		{"最优基线", -0.25, 2.5, 8, 12, bestModules},
		// 更灵敏的阈值
		{"灵敏阈值", -0.15, 2.5, 8, 12, bestModules},
		{"灵敏阈值2", -0.20, 2.5, 8, 12, bestModules},
		// 短冷却
		{"短冷却", -0.25, 2.5, 8, 6, bestModules},
		{"短冷却+灵敏", -0.20, 2.5, 8, 6, bestModules},
		// 短持仓
		{"短持仓", -0.25, 2.5, 3, 12, bestModules},
		{"短持仓+短冷却", -0.25, 2.5, 3, 6, bestModules},
		// 更紧止损
		{"紧止损", -0.25, 2.0, 8, 12, bestModules},
		{"紧止损+短冷却", -0.25, 2.0, 8, 6, bestModules},
		// 综合优化: 灵敏+短冷却+适中持仓
		{"综合A", -0.20, 2.5, 6, 8, bestModules},
		{"综合B", -0.15, 3.0, 6, 6, bestModules},
		{"综合C", -0.20, 2.0, 6, 8, bestModules},
		{"综合D", -0.15, 2.5, 6, 6, bestModules},
		{"综合E", -0.20, 3.0, 3, 6, bestModules},
	}

	fmt.Printf("╔══════════════════════════════════════════════════════════╗\n")
	fmt.Printf("║    BTC 1h 做空深度诊断 — 参数优化对比                    ║\n")
	fmt.Printf("╚══════════════════════════════════════════════════════════╝\n\n")

	for _, ps := range paramSets {
		fmt.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
		fmt.Printf("🔧 %s: thresh=%.2f ATR=%.1f hold=%d cd=%d\n", ps.label, ps.thresh, ps.atr, ps.minHold, ps.cooldown)
		fmt.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")

		var totalRetAll float64
		var countAll int

		for _, pd := range periods {
			klines, err := backtest.LoadKlinesFromStore(ctx, store, sym, iv, pd.start, end)
			if err != nil || len(klines) < 100 {
				fmt.Printf("  %s: 数据不足\n", pd.name)
				continue
			}
			htfKlines, _ := backtest.LoadKlinesFromStore(ctx, store, sym, "1d", pd.start, end)

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
				"buy_threshold":       0.20, "sell_threshold": -0.30,
				"confirm_bars": 1, "cooldown_bars": 12, "min_hold_bars": 18,
				"atr_stop_mult": 4.0, "atr_period": 14,
				"interval": iv,
				"modules":  ps.modules,
			}

			strat := trend.NewCustomWeightedStrategy()
			if err := strat.Init(tcfg); err != nil {
				fmt.Printf("  %s: init err: %v\n", pd.name, err)
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
				fmt.Printf("  %s: run err: %v\n", pd.name, err)
				continue
			}

			sm := res.ShortMetrics
			fmt.Printf("  📅 %s: %d笔 | 胜率%.1f%% | 收益%+.2f%% | PF=%.2f | 平均赢$%.1f 亏$%.1f | 最大赢$%.1f 亏$%.1f | 手续费$%.1f\n",
				pd.name, sm.TotalTrades, sm.WinRate*100, sm.TotalReturnPct,
				sm.ProfitFactor, sm.AvgWin, sm.AvgLoss,
				sm.LargestWin, sm.LargestLoss, sm.TotalFees)

			// 打印每笔交易
			if sm.TotalTrades > 0 && sm.TotalTrades <= 50 {
				fmt.Printf("    %-4s %-16s %-10s %-10s %-9s %-8s %s\n",
					"#", "开仓时间", "开仓价", "平仓价", "盈亏$", "盈亏%", "原因")
				tradeIdx := 0
				var shortPrice float64
				var shortQty float64
				var shortTime time.Time
				for _, tr := range res.ShortTrades {
					if tr.Side == "SHORT" {
						shortPrice = tr.Price
						shortQty = tr.Quantity
						shortTime = tr.Timestamp
					} else if tr.Side == "COVER" && shortQty > 0 {
						tradeIdx++
						pnl := (shortPrice - tr.Price) * shortQty
						pnlPct := (shortPrice - tr.Price) / shortPrice * 100
						duration := tr.Timestamp.Sub(shortTime)
						hours := duration.Hours()
						fmt.Printf("    %-4d %s  $%-9.0f $%-9.0f %+8.1f %+6.2f%% [%.0fh] %s\n",
							tradeIdx,
							shortTime.Format("01-02 15:04"),
							shortPrice, tr.Price,
							pnl, pnlPct, hours, tr.Reason)
						shortQty = 0
					}
				}
			}

			totalRetAll += sm.TotalReturnPct
			countAll++

			// 分析交易时间分布
			if sm.TotalTrades > 0 {
				monthCounts := map[string]int{}
				monthPnL := map[string]float64{}
				var shortP float64
				var shortQ float64
				for _, tr := range res.ShortTrades {
					if tr.Side == "SHORT" {
						shortP = tr.Price
						shortQ = tr.Quantity
					} else if tr.Side == "COVER" && shortQ > 0 {
						month := tr.Timestamp.Format("2006-01")
						monthCounts[month]++
						monthPnL[month] += (shortP - tr.Price) * shortQ
						shortQ = 0
					}
				}
				fmt.Printf("    📊 月度分布: ")
				for m, c := range monthCounts {
					fmt.Printf("%s:%d笔(%+.0f$) ", m, c, monthPnL[m])
				}
				fmt.Println()
			}
		}

		if countAll > 0 {
			fmt.Printf("  ⚡ 平均收益: %+.2f%%\n\n", totalRetAll/float64(countAll))
		}
	}
}
