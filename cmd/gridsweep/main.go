// gridsweep: compare grid-trading parameter combinations (spacing % x
// USDT-per-grid) over the same historical window, so you can see how spacing
// trades off fee drag vs. trade frequency, and how per-grid sizing affects
// absolute profit vs. capital utilization. One-off research tool, not wired
// into the Makefile. Run with `go run ./cmd/gridsweep`.
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/jayce/btc-trader/internal/config"
	"github.com/jayce/btc-trader/internal/grid"
	"github.com/jayce/btc-trader/internal/storage/timescale"
	"go.uber.org/zap"
)

func main() {
	logger, _ := zap.NewDevelopment()
	defer logger.Sync()

	cfg, err := config.Load("")
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

	symbol := "BTCUSDT"
	interval := "1m"
	end := time.Now().UTC()
	days := 14
	start := end.Add(-time.Duration(days) * 24 * time.Hour)

	klines, err := store.GetKlines(ctx, symbol, interval, start, end, 0)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load klines: %v\n", err)
		os.Exit(1)
	}
	if len(klines) == 0 {
		fmt.Fprintln(os.Stderr, "no klines loaded")
		os.Exit(1)
	}

	// Same range used in the earlier manual run, so results are comparable.
	lower, upper := 76264.0, 82300.0
	cash := 10000.0
	feeRate := 0.001
	stopLossBelowPct := 0.05

	spacings := []float64{0.005, 0.01, 0.015, 0.02, 0.03}
	perGridAmounts := []float64{200, 500}

	fmt.Printf("symbol=%s interval=%s window=%dd range=[%.0f,%.0f] fee=%.3f%% stop_loss_below=%.1f%%\n\n",
		symbol, interval, days, lower, upper, feeRate*100, stopLossBelowPct*100)

	fmt.Printf("%-8s %-12s %8s %8s %8s %10s %10s %8s %10s %10s\n",
		"spacing", "usdt/grid", "trades", "trips", "maxConc", "realizedPnL", "totalFees", "return%", "capDeploy", "returnOnCap%")

	for _, sp := range spacings {
		for _, amt := range perGridAmounts {
			cfg := grid.NewConfig()
			cfg.Symbol = symbol
			cfg.Interval = interval
			cfg.UpperPrice = upper
			cfg.LowerPrice = lower
			cfg.GridSpacingPct = sp
			cfg.USDTPerGrid = amt
			cfg.FeeRate = feeRate
			cfg.InitialCash = cash
			cfg.StopLossBelowPct = stopLossBelowPct

			eng := grid.NewEngine(cfg)
			res, err := eng.Run(klines)
			if err != nil {
				fmt.Printf("  error for spacing=%.3f amt=%.0f: %v\n", sp, amt, err)
				continue
			}

			capDeployed := float64(res.Metrics.MaxConcurrentHoldings) * amt
			returnOnCap := 0.0
			if capDeployed > 0 {
				returnOnCap = res.Metrics.RealizedPnL / capDeployed * 100
			}

			stopFlag := ""
			if res.StoppedOut {
				stopFlag = " [STOPPED]"
			}

			fmt.Printf("%-8s %-12s %8d %8d %8d %10.2f %10.2f %8.3f %10.2f %10.2f%s\n",
				fmt.Sprintf("%.1f%%", sp*100), fmt.Sprintf("$%.0f", amt),
				len(res.Trades), res.Metrics.CompletedRoundTrips, res.Metrics.MaxConcurrentHoldings,
				res.Metrics.RealizedPnL, res.Metrics.TotalFees, res.Metrics.TotalReturnPct,
				capDeployed, returnOnCap, stopFlag,
			)
		}
	}
}
