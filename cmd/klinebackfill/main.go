package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/jayce/btc-trader/internal/config"
	"github.com/jayce/btc-trader/internal/exchange"
	"github.com/jayce/btc-trader/internal/exchange/binance"
	"github.com/jayce/btc-trader/internal/storage/timescale"
	"go.uber.org/zap"
)

const binanceLimit = 1000

func intervalDuration(interval string) time.Duration {
	switch interval {
	case "1m":
		return time.Minute
	case "3m":
		return 3 * time.Minute
	case "5m":
		return 5 * time.Minute
	case "15m":
		return 15 * time.Minute
	case "30m":
		return 30 * time.Minute
	case "1h":
		return time.Hour
	case "2h":
		return 2 * time.Hour
	case "4h":
		return 4 * time.Hour
	case "6h":
		return 6 * time.Hour
	case "8h":
		return 8 * time.Hour
	case "12h":
		return 12 * time.Hour
	case "1d":
		return 24 * time.Hour
	case "3d":
		return 3 * 24 * time.Hour
	case "1w":
		return 7 * 24 * time.Hour
	default:
		return 5 * time.Minute
	}
}

func main() {
	configPath := flag.String("config", "", "path to config file")
	symbol := flag.String("symbol", "BTCUSDT", "trading symbol")
	interval := flag.String("interval", "5m", "kline interval (1m,5m,15m,1h,4h,1d)")
	days := flag.Int("days", 365, "number of days to backfill")

	flag.Parse()

	logger, _ := zap.NewProduction()
	defer logger.Sync()

	cfg, err := config.Load(*configPath)
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

	client := binance.NewClient(cfg.Exchange.APIKey, cfg.Exchange.SecretKey, cfg.App.Testnet, logger.Named("binance"))

	end := time.Now().UTC()
	start := end.Add(-time.Duration(*days) * 24 * time.Hour)

	fmt.Printf("Backfilling %s %s from %s to %s...\n",
		*symbol, *interval,
		start.Format("2006-01-02"), end.Format("2006-01-02"),
	)

	var total int
	cur := start

	for cur.Before(end) {
		req := exchange.KlineRequest{
			Symbol:    *symbol,
			Interval:  *interval,
			StartTime: &cur,
			EndTime:   &end,
			Limit:     binanceLimit,
		}

		klines, err := client.GetKlines(ctx, req)
		if err != nil {
			fmt.Fprintf(os.Stderr, "fetch klines: %v\n", err)
			os.Exit(1)
		}

		if len(klines) == 0 {
			break
		}

		if err := store.SaveKlines(ctx, klines); err != nil {
			fmt.Fprintf(os.Stderr, "save klines: %v\n", err)
			os.Exit(1)
		}

		total += len(klines)
		cur = klines[len(klines)-1].OpenTime.Add(intervalDuration(*interval))

		fmt.Printf("  saved %d (total %d), next from %s\n", len(klines), total, cur.Format("2006-01-02 15:04"))

		if len(klines) < binanceLimit {
			break
		}

		time.Sleep(200 * time.Millisecond)
	}

	fmt.Printf("\nDone. Total %d klines saved.\n", total)
}
