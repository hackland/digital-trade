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
	start := end.Add(-365 * 24 * time.Hour)

	klines, err := backtest.LoadKlinesFromStore(ctx, store, sym, iv, start, end)
	if err != nil || len(klines) < 100 {
		fmt.Fprintf(os.Stderr, "数据不足\n")
		os.Exit(1)
	}
	htfKlines, _ := backtest.LoadKlinesFromStore(ctx, store, sym, "1d", start, end)

	modules := []interface{}{
		map[string]interface{}{"name": "macd", "weight": 0.60},
		map[string]interface{}{"name": "ema_cross", "weight": 0.40},
	}

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
		"buy_threshold":       0.20, "sell_threshold": -0.30,
		"confirm_bars": 1, "cooldown_bars": 12, "min_hold_bars": 18,
		"atr_stop_mult": 4.0, "atr_period": 14,
		"interval": iv,
		"modules":  modules,
	}

	strat := trend.NewCustomWeightedStrategy()
	if err := strat.Init(tcfg); err != nil {
		fmt.Fprintf(os.Stderr, "init: %v\n", err)
		os.Exit(1)
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
		fmt.Fprintf(os.Stderr, "run: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("╔══════════════════════════════════════════════════════════════════╗\n")
	fmt.Printf("║  BTC 1h 365d 做空亏损分析 — 每笔交易详解                      ║\n")
	fmt.Printf("╚══════════════════════════════════════════════════════════════════╝\n\n")

	// 配对交易
	type roundTrip struct {
		shortTime  time.Time
		coverTime  time.Time
		shortPrice float64
		coverPrice float64
		qty        float64
		pnl        float64
		pnlPct     float64
		duration   time.Duration
		reason     string
		month      string
	}
	var trades []roundTrip
	var shortP, shortQ float64
	var shortT time.Time

	for _, tr := range res.ShortTrades {
		if tr.Side == "SHORT" {
			shortP = tr.Price
			shortQ = tr.Quantity
			shortT = tr.Timestamp
		} else if tr.Side == "COVER" && shortQ > 0 {
			pnl := (shortP - tr.Price) * shortQ
			pnlPct := (shortP - tr.Price) / shortP * 100
			trades = append(trades, roundTrip{
				shortTime: shortT, coverTime: tr.Timestamp,
				shortPrice: shortP, coverPrice: tr.Price,
				qty: shortQ, pnl: pnl, pnlPct: pnlPct,
				duration: tr.Timestamp.Sub(shortT),
				reason:   tr.Reason,
				month:    shortT.Format("2006-01"),
			})
			shortQ = 0
		}
	}

	// === 1. 按月分析 ===
	type monthStat struct {
		month       string
		trades      int
		wins        int
		totalPnL    float64
		avgPnL      float64
		avgDuration float64 // hours
		stopCount   int     // ATR stop 触发次数
		coverCount  int     // score cover 次数
		avgWinPct   float64
		avgLossPct  float64
	}
	monthMap := map[string]*monthStat{}
	months := []string{}

	for _, t := range trades {
		ms, ok := monthMap[t.month]
		if !ok {
			ms = &monthStat{month: t.month}
			monthMap[t.month] = ms
			months = append(months, t.month)
		}
		ms.trades++
		ms.totalPnL += t.pnl
		ms.avgDuration += t.duration.Hours()
		if t.pnl > 0 {
			ms.wins++
			ms.avgWinPct += t.pnlPct
		} else {
			ms.avgLossPct += t.pnlPct
		}
		if len(t.reason) > 10 && t.reason[:10] == "Short ATR " {
			ms.stopCount++
		} else {
			ms.coverCount++
		}
	}

	sort.Strings(months)
	fmt.Printf("📊 月度统计:\n")
	fmt.Printf("%-8s %4s %4s %6s %8s %6s %5s %5s %7s %7s\n",
		"月份", "笔数", "赢", "胜率%", "盈亏$", "均PnL$", "止损", "平仓", "赢均%", "亏均%")
	fmt.Println("──────────────────────────────────────────────────────────────────────────────")
	for _, m := range months {
		ms := monthMap[m]
		ms.avgPnL = ms.totalPnL / float64(ms.trades)
		ms.avgDuration = ms.avgDuration / float64(ms.trades)
		wr := float64(ms.wins) / float64(ms.trades) * 100
		avgWP := 0.0
		avgLP := 0.0
		if ms.wins > 0 {
			avgWP = ms.avgWinPct / float64(ms.wins)
		}
		lossTrades := ms.trades - ms.wins
		if lossTrades > 0 {
			avgLP = ms.avgLossPct / float64(lossTrades)
		}
		marker := ""
		if ms.totalPnL < -100 {
			marker = " ❌"
		} else if ms.totalPnL < 100 {
			marker = " ⚠️"
		} else {
			marker = " ✅"
		}
		fmt.Printf("%-8s %4d %4d %5.1f%% %+7.0f$ %+5.0f$ %5d %5d %+6.2f%% %+6.2f%%%s\n",
			m, ms.trades, ms.wins, wr, ms.totalPnL, ms.avgPnL,
			ms.stopCount, ms.coverCount, avgWP, avgLP, marker)
	}

	// === 2. 亏损月每笔交易 ===
	fmt.Printf("\n\n🔍 亏损月 & 薄利月 逐笔详情:\n")
	for _, m := range months {
		ms := monthMap[m]
		if ms.totalPnL > 200 {
			continue
		}
		fmt.Printf("\n  ── %s (总PnL: %+.0f$, %d笔, 胜率%.0f%%) ──\n", m, ms.totalPnL, ms.trades, float64(ms.wins)/float64(ms.trades)*100)
		for i, t := range trades {
			if t.month != m {
				continue
			}
			marker := "  "
			if t.pnl < -50 {
				marker = "❌"
			} else if t.pnl < 0 {
				marker = "⚠️"
			} else {
				marker = "✅"
			}
			fmt.Printf("    %s #%-3d %s→%s [%2.0fh] $%-8.0f→$%-8.0f PnL:%+7.1f$ (%+5.2f%%) %s\n",
				marker, i+1,
				t.shortTime.Format("01-02 15:04"),
				t.coverTime.Format("01-02 15:04"),
				t.duration.Hours(),
				t.shortPrice, t.coverPrice,
				t.pnl, t.pnlPct, t.reason)
		}
	}

	// === 3. 亏损交易特征分析 ===
	fmt.Printf("\n\n📈 盈亏交易特征对比:\n")
	var winDurs, lossDurs []float64
	var winPcts, lossPcts []float64
	var stopWins, stopLosses, coverWins, coverLosses int

	for _, t := range trades {
		isStop := len(t.reason) > 10 && t.reason[:10] == "Short ATR "
		if t.pnl > 0 {
			winDurs = append(winDurs, t.duration.Hours())
			winPcts = append(winPcts, t.pnlPct)
			if isStop {
				stopWins++
			} else {
				coverWins++
			}
		} else {
			lossDurs = append(lossDurs, t.duration.Hours())
			lossPcts = append(lossPcts, math.Abs(t.pnlPct))
			if isStop {
				stopLosses++
			} else {
				coverLosses++
			}
		}
	}

	avgWinDur := mean(winDurs)
	avgLossDur := mean(lossDurs)
	avgWinPct := mean(winPcts)
	avgLossPct := mean(lossPcts)

	fmt.Printf("  盈利交易: %d笔, 平均持仓%.1fh, 平均盈利%+.2f%%\n", len(winPcts), avgWinDur, avgWinPct)
	fmt.Printf("  亏损交易: %d笔, 平均持仓%.1fh, 平均亏损-%.2f%%\n", len(lossPcts), avgLossDur, avgLossPct)
	fmt.Printf("  盈亏比: %.2f (平均盈利/平均亏损)\n", avgWinPct/avgLossPct)
	fmt.Printf("\n  平仓方式 vs 盈亏:\n")
	fmt.Printf("    ATR止损: %d赢 / %d亏 (胜率%.1f%%)\n", stopWins, stopLosses,
		float64(stopWins)/float64(stopWins+stopLosses)*100)
	fmt.Printf("    信号平仓: %d赢 / %d亏 (胜率%.1f%%)\n", coverWins, coverLosses,
		float64(coverWins)/float64(coverWins+coverLosses)*100)

	// === 4. 连续亏损分析 ===
	fmt.Printf("\n📉 连续亏损序列:\n")
	streak := 0
	maxStreak := 0
	streakPnL := 0.0
	for _, t := range trades {
		if t.pnl < 0 {
			streak++
			streakPnL += t.pnl
			if streak > maxStreak {
				maxStreak = streak
			}
		} else {
			if streak >= 3 {
				fmt.Printf("  连亏%d笔, 累计PnL: %+.0f$ (结束于%s)\n", streak, streakPnL, t.shortTime.Format("01-02"))
			}
			streak = 0
			streakPnL = 0
		}
	}
	fmt.Printf("  最大连续亏损: %d笔\n", maxStreak)

	// === 5. 价格位置分析 ===
	fmt.Printf("\n📊 开空价格区间 vs 盈亏:\n")
	type priceRange struct {
		label        string
		lo, hi       float64
		wins, losses int
		pnl          float64
	}
	ranges := []priceRange{
		{"<60K", 0, 60000, 0, 0, 0},
		{"60-70K", 60000, 70000, 0, 0, 0},
		{"70-80K", 70000, 80000, 0, 0, 0},
		{"80-90K", 80000, 90000, 0, 0, 0},
		{"90-100K", 90000, 100000, 0, 0, 0},
		{">100K", 100000, 999999, 0, 0, 0},
	}
	for _, t := range trades {
		for i := range ranges {
			if t.shortPrice >= ranges[i].lo && t.shortPrice < ranges[i].hi {
				ranges[i].pnl += t.pnl
				if t.pnl > 0 {
					ranges[i].wins++
				} else {
					ranges[i].losses++
				}
				break
			}
		}
	}
	for _, r := range ranges {
		total := r.wins + r.losses
		if total == 0 {
			continue
		}
		fmt.Printf("  %-10s %3d笔 (胜率%5.1f%%) PnL: %+8.0f$\n",
			r.label, total, float64(r.wins)/float64(total)*100, r.pnl)
	}

	// === 6. BTC价格走势 vs 做空收益关联 ===
	fmt.Printf("\n📊 月度BTC价格变化 vs 做空收益:\n")
	// 用日线算每月的价格变化
	monthPrices := map[string]struct{ open, close float64 }{}
	for _, k := range htfKlines {
		m := k.OpenTime.Format("2006-01")
		mp, ok := monthPrices[m]
		if !ok {
			mp.open = k.Open
		}
		mp.close = k.Close
		monthPrices[m] = mp
	}
	fmt.Printf("  %-8s %8s %8s %7s | %8s %4s\n", "月份", "开盘", "收盘", "涨跌%", "做空PnL", "笔数")
	fmt.Println("  ─────────────────────────────────────────────────────")
	for _, m := range months {
		mp := monthPrices[m]
		ms := monthMap[m]
		chg := 0.0
		if mp.open > 0 {
			chg = (mp.close - mp.open) / mp.open * 100
		}
		correlation := ""
		if chg > 3 && ms.totalPnL < 0 {
			correlation = " ← 涨时做空亏损"
		} else if chg < -3 && ms.totalPnL > 500 {
			correlation = " ← 跌时做空大赚"
		}
		fmt.Printf("  %-8s $%6.0f→$%6.0f %+5.1f%% | %+7.0f$ %4d%s\n",
			m, mp.open, mp.close, chg, ms.totalPnL, ms.trades, correlation)
	}
}

func mean(vals []float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	sum := 0.0
	for _, v := range vals {
		sum += v
	}
	return sum / float64(len(vals))
}
