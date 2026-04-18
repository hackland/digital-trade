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

	// 用户配置: MACD 40%, RSI 40%, BB 20%, HTF 开
	// 排列组合关键变量
	thresholds := []float64{-0.10, -0.15, -0.20, -0.25, -0.30}
	htfPeriods := []int{5, 10}
	confirmBars := []int{1, 2}

	modules := []interface{}{
		map[string]interface{}{"name": "macd", "weight": 0.40},
		map[string]interface{}{"name": "rsi", "weight": 0.40},
		map[string]interface{}{"name": "bb_position", "weight": 0.20},
	}

	symbols := []string{"BTCUSDT", "ETHUSDT"}
	intervals := []string{"1h", "4h"}

	fmt.Printf("%-8s %-3s %-4s  thresh  htf  cfm | %5s %6s %8s %6s\n",
		"币种", "周期", "区间", "交易", "胜率%", "收益%", "PF")
	fmt.Println("────────────────────────────────────────────────────────────────────")

	for _, sym := range symbols {
		for _, iv := range intervals {
			for _, pd := range periods {
				klines, err := backtest.LoadKlinesFromStore(ctx, store, sym, iv, pd.start, end)
				if err != nil || len(klines) < 50 {
					continue
				}
				htfKlines, _ := backtest.LoadKlinesFromStore(ctx, store, sym, "1d", pd.start, end)

				for _, th := range thresholds {
					for _, hp := range htfPeriods {
						for _, cb := range confirmBars {
							tcfg := map[string]interface{}{
								"short_enabled": true,
								"htf_enabled":   true, "htf_interval": "1d", "htf_period": hp,
								"trend_filter":        false,
								"short_threshold":     th,
								"cover_threshold":     -th * 0.6,
								"short_confirm_bars":  cb,
								"short_min_hold_bars": 3,
								"short_atr_stop_mult": 3.0,
								"short_cooldown_bars": 6,
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
							if err != nil {
								continue
							}
							sm := res.ShortMetrics
							if sm.TotalTrades > 0 {
								fmt.Printf("%-8s %-3s %-4s %+.2f  %3d  %3d | %5d %5.1f%% %+7.2f %6.2f\n",
									sym, iv, pd.name, th, hp, cb, sm.TotalTrades, sm.WinRate*100, sm.TotalReturnPct, sm.ProfitFactor)
							}
						}
					}
				}
			}
		}
	}
}
