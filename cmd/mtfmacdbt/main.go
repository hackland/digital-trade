// mtfmacdbt: 回测 4h/1h/15m 三周期 MACD 共振信号（用 Binance 公共行情，无需 API Key）。
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/jayce/btc-trader/internal/exchange/binance"
	"github.com/jayce/btc-trader/internal/mtfmacd"
	"go.uber.org/zap"
)

func main() {
	symbol := flag.String("symbol", "BTCUSDT", "symbol")
	days := flag.Int("days", 180, "backtest days")
	exitMode := flag.Bool("exit", false, "run 1h-histogram-fade exit study instead of fixed-horizon stats")
	rev := flag.String("rev", "cross", "15m reversal: cross | turn | turn_pre")
	exp4 := flag.Bool("exp4h", false, "require 4h histogram expanding")
	min1 := flag.Float64("min1h", 0, "min |1h DIF|/price in percent")
	stopMode := flag.Bool("stop", false, "run structural stop-loss study (15m swing low/high)")
	macdStop := flag.Bool("macdstop", false, "stop requires price break + DIF new extreme")
	div := flag.Bool("div", false, "require 15m MACD divergence")
	divMode := flag.String("divmode", "regular", "regular | near | hidden | dif_up")
	longOnly := flag.Bool("long", false, "long side only")
	revExit := flag.Bool("revexit", false, "1h-fade take profit + 15m reversal-failure stop study")
	exp1 := flag.Bool("exp1h", false, "require 1h histogram expanding")
	fee := flag.Float64("fee", 0.1, "fee % per side (exit study)")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	params := mtfmacd.DefaultParams()
	params.Reversal = *rev
	params.Expanding4h, params.Min1hDifPct = *exp4, *min1
	params.Divergence, params.DivMode = *div, *divMode
	params.LongOnly = *longOnly
	params.Expanding1h = *exp1
	client := binance.NewClient("", "", false, zap.NewNop())
	if *revExit {
		var rules []mtfmacd.RevExitRule
		add := func(name string, r mtfmacd.RevExitRule) {
			r.Name, r.PeakRatio, r.HardStopPct = name, -1, 3 // take profit = 1h histogram colour flip
			r.TPInProfitOnly = true
			rules = append(rules, r)
		}
		for _, mp := range []float64{0, 0.5, 1.0, 1.5} {
			add(fmt.Sprintf("盈利>%.1f%%才止盈 | 无结构止损", mp), mtfmacd.RevExitRule{StopMode: "none", MinProfitPct: mp})
			for _, b := range []float64{1.0, 1.5} {
				add(fmt.Sprintf("盈利>%.1f%%才止盈 | 15m低点-%.1f%%+1h同意", mp, b), mtfmacd.RevExitRule{StopMode: "leg", Buffer: b, Stop1hAgree: true, MinProfitPct: mp})
			}
			add(fmt.Sprintf("盈利>%.1f%%才止盈 | 1h前低-0.5%%+1h同意", mp), mtfmacd.RevExitRule{StopMode: "pivot1h", Buffer: 0.5, Stop1hAgree: true, MinProfitPct: mp})
		}
		stats, n, err := mtfmacd.RevExitStudy(ctx, client, *symbol, *days, params, rules, *fee, 4)
		if err != nil {
			fmt.Fprintln(os.Stderr, "revexit study:", err)
			os.Exit(1)
		}
		fmt.Printf("%s %d天 信号%d个 反转=%s 手续费单边%.2f%% 硬止损3%% 最长48h 止盈=1h柱翻色\n", *symbol, *days, n, *rev, *fee)
		for _, st := range stats {
			fmt.Printf("%-30s %-4s 笔%3d 胜率%5.1f%% 均%6.3f%% PF%5.2f 累计%7.1f%% 止损占%3.0f%% 止盈占%3.0f%% 持仓%4.1fh\n",
				st.Rule, st.Group, st.Trades, st.WinRate, st.AvgRet, st.PF, st.SumRet, st.StopRate, st.TPRate, st.AvgHoldH)
		}
		return
	}
	if *stopMode {
		var rules []mtfmacd.StopRule
		if *macdStop {
			rules = append(rules, mtfmacd.StopRule{Name: "【对照】价格止损 leg缓冲1.0%", Stop: mtfmacd.StopLeg, MaxBars: 96, Buffer: 1.0, PivotLen: 3})
			for _, st := range []string{mtfmacd.StopLeg, mtfmacd.StopPivot} {
				for _, buf := range []float64{0, 0.3} {
					for _, hard := range []float64{0, 2, 3} {
						name := fmt.Sprintf("MACD确认 %s 缓冲%.1f%%", st, buf)
						if hard > 0 {
							name += fmt.Sprintf(" +硬止损%.0f%%", hard)
						} else {
							name += " 无硬止损"
						}
						rules = append(rules, mtfmacd.StopRule{Name: name, Stop: st, MaxBars: 96, Buffer: buf, PivotLen: 3, MACDConfirm: true, HardStopPct: hard})
					}
				}
			}
		} else {
			for _, st := range []string{mtfmacd.StopPivot, mtfmacd.StopLeg} {
				for _, buf := range []float64{0.8, 1.0, 1.5, 2.0} {
					rules = append(rules, mtfmacd.StopRule{Name: fmt.Sprintf("%s 缓冲%.1f%%", st, buf), Stop: st, MaxBars: 96, Buffer: buf, PivotLen: 3})
				}
				for _, k := range []float64{0.5, 1, 1.5, 2} {
					rules = append(rules, mtfmacd.StopRule{Name: fmt.Sprintf("%s 0.1%%+%.1fATR", st, k), Stop: st, MaxBars: 96, Buffer: 0.1, ATRMult: k, PivotLen: 3})
				}
			}
		}
		stats, n, err := mtfmacd.StopStudy(ctx, client, *symbol, *days, params, rules, *fee, 4)
		if err != nil {
			fmt.Fprintln(os.Stderr, "stop study:", err)
			os.Exit(1)
		}
		fmt.Printf("%s %d天 信号%d个 反转=%s 手续费单边%.2f%% 最长持仓24h\n", *symbol, *days, n, *rev, *fee)
		for _, st := range stats {
			fmt.Printf("%-22s %-4s 笔%3d 跳过%2d 胜率%5.1f%% 均%6.3f%% 均R%6.2f PF%5.2f 累计%7.1f%% 平均止损距离%5.2f%% 止损占%3.0f%% 止盈占%3.0f%% 持仓%4.1fh\n",
				st.Rule, st.Group, st.Trades, st.Skipped, st.WinRate, st.AvgRet, st.AvgR, st.PF, st.SumRet, st.AvgRisk, st.StopRate, st.TPRate, st.AvgHoldH)
		}
		return
	}
	if *exitMode {
		var rules []mtfmacd.ExitRule
		for _, sl := range []float64{0, 1, 1.5, 2} {
			for _, ratio := range []float64{0.6, 0.4, 0.2, 0} {
				name := fmt.Sprintf("1h柱回落至峰值%.0f%%", ratio*100)
				if ratio == 0 {
					name = "1h柱回落至0轴"
				}
				if sl > 0 {
					name += fmt.Sprintf(" +止损%.1f%%", sl)
				}
				rules = append(rules, mtfmacd.ExitRule{Name: name, PeakRatio: ratio, StopLossPct: sl})
			}
		}
		stats, n, err := mtfmacd.ExitStudy(ctx, client, *symbol, *days, params, rules, *fee)
		if err != nil {
			fmt.Fprintln(os.Stderr, "exit study:", err)
			os.Exit(1)
		}
		fmt.Printf("%s %d天 信号%d个 手续费单边%.2f%% (收益已扣费)\n", *symbol, *days, n, *fee)
		for _, st := range stats {
			fmt.Printf("%-26s %-4s 笔%3d 胜率%5.1f%% 均%6.2f%% 盈%5.2f 亏%6.2f 盈亏比PF%5.2f 累计%7.1f%% 持仓%5.1fh 止损占%3.0f%% 最差%6.2f%%\n",
				st.Rule, st.Group, st.Trades, st.WinRate, st.AvgRet, st.AvgWin, st.AvgLoss, st.ProfitFact, st.SumRet, st.AvgHoldH, st.StopRate, st.MaxDD)
		}
		return
	}
	res, err := mtfmacd.Backtest(ctx, client, *symbol, *days, params)
	if err != nil {
		fmt.Fprintln(os.Stderr, "backtest:", err)
		os.Exit(1)
	}
	fmt.Printf("%s %d天 15m K线%d根 信号%d个 (收益为信号方向，未计手续费)\n", res.Symbol, res.Days, res.Bars15m, len(res.Signals))
	for _, b := range res.Buckets {
		fmt.Printf("\n[%s] 信号数=%d 平均最大浮盈=%.2f%% 平均最大浮亏=%.2f%%\n", b.Name, b.Count, b.AvgMFE, b.AvgMAE)
		for _, h := range res.Horizons {
			s := b.ByHorizon[h]
			fmt.Printf("  %4dm后: 样本%3d 胜率%5.1f%% 均收益%6.3f%% 均盈%6.3f%% 均亏%7.3f%% 累计%7.2f%%\n", h*15, s.Samples, s.WinRate, s.AvgRet, s.AvgWin, s.AvgLoss, s.SumRet)
		}
	}
}
