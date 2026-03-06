package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jayce/btc-trader/internal/backtest"
	"github.com/jayce/btc-trader/internal/config"
	"github.com/jayce/btc-trader/internal/storage/timescale"
	"github.com/jayce/btc-trader/internal/strategy"
	"github.com/jayce/btc-trader/internal/strategy/trend"
	"go.uber.org/zap"
)

func main() {
	// Flags
	configPath := flag.String("config", "", "path to config file (default: ./configs/config.yaml)")
	symbolFlag := flag.String("symbol", "BTCUSDT", "trading symbol")
	intervalFlag := flag.String("interval", "5m", "kline interval")
	strategyFlag := flag.String("strategy", "", "strategy name (overrides config)")
	daysFlag := flag.Int("days", 30, "number of days to backtest")
	startFlag := flag.String("start", "", "start date (YYYY-MM-DD), overrides -days")
	endFlag := flag.String("end", "", "end date (YYYY-MM-DD), defaults to now")
	cashFlag := flag.Float64("cash", 10000, "initial USDT balance")
	feeFlag := flag.Float64("fee", 0.001, "fee rate (e.g., 0.001 = 0.1%)")
	allocFlag := flag.Float64("alloc", 0.1, "allocation per trade (e.g., 0.1 = 10%)")

	flag.Parse()

	// Logger
	logger, _ := zap.NewProduction()
	defer logger.Sync()

	// Load config
	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load config: %v\n", err)
		os.Exit(1)
	}

	// Parse time range
	var start, end time.Time
	end = time.Now().UTC()

	if *endFlag != "" {
		end, err = time.Parse("2006-01-02", *endFlag)
		if err != nil {
			fmt.Fprintf(os.Stderr, "invalid end date: %v\n", err)
			os.Exit(1)
		}
	}

	if *startFlag != "" {
		start, err = time.Parse("2006-01-02", *startFlag)
		if err != nil {
			fmt.Fprintf(os.Stderr, "invalid start date: %v\n", err)
			os.Exit(1)
		}
	} else {
		start = end.Add(-time.Duration(*daysFlag) * 24 * time.Hour)
	}

	// Determine strategy
	stratName := cfg.Strategy.Name
	if *strategyFlag != "" {
		stratName = *strategyFlag
	}

	// Create strategy via registry
	strat, err := createStrategy(stratName, cfg.Strategy.Config)
	if err != nil {
		fmt.Fprintf(os.Stderr, "create strategy: %v\n", err)
		os.Exit(1)
	}

	// Connect to database
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle signals
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Println("\ninterrupted, stopping backtest...")
		cancel()
	}()

	store, err := timescale.New(ctx, cfg.Database, logger.Named("db"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "connect db: %v\n", err)
		os.Exit(1)
	}
	defer store.Close()

	// Load klines from database
	fmt.Printf("Loading %s %s klines from %s to %s...\n",
		*symbolFlag, *intervalFlag,
		start.Format("2006-01-02"), end.Format("2006-01-02"),
	)

	klines, err := backtest.LoadKlinesFromStore(ctx, store, *symbolFlag, *intervalFlag, start, end)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load klines: %v\n", err)
		os.Exit(1)
	}

	if len(klines) == 0 {
		fmt.Fprintf(os.Stderr, "no kline data found for %s %s in the specified range\n", *symbolFlag, *intervalFlag)
		os.Exit(1)
	}

	fmt.Printf("Loaded %d klines. Running backtest with strategy '%s'...\n\n", len(klines), strat.Name())

	// Create and run backtest engine
	engine := backtest.NewEngine(backtest.EngineConfig{
		Symbol:      *symbolFlag,
		Interval:    *intervalFlag,
		InitialCash: *cashFlag,
		FeeRate:     *feeFlag,
		AllocPct:    *allocFlag,
	}, strat, logger.Named("backtest"))

	result, err := engine.Run(ctx, klines)
	if err != nil {
		fmt.Fprintf(os.Stderr, "backtest failed: %v\n", err)
		os.Exit(1)
	}

	// Print results
	fmt.Print(result.PrintSummary())
}

func createStrategy(name string, cfg map[string]interface{}) (strategy.Strategy, error) {
	reg := strategy.NewRegistry()
	reg.Register("ema_crossover", func() strategy.Strategy { return trend.NewEMACrossStrategy() })
	reg.Register("macd_rsi", func() strategy.Strategy { return trend.NewMACDRSIStrategy() })
	reg.Register("bb_breakout", func() strategy.Strategy { return trend.NewBBBreakoutStrategy() })
	return reg.Create(name, cfg)
}
