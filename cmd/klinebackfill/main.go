package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
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
	interval := flag.String("interval", "", "kline interval (comma-separated, e.g. 15m,1h,4h,1d,1w)")
	days := flag.Int("days", 730, "number of days to backfill")
	startDate := flag.String("start", "", "start date (YYYY-MM-DD), overrides -days")

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
	var start time.Time
	if *startDate != "" {
		start, err = time.Parse("2006-01-02", *startDate)
		if err != nil {
			fmt.Fprintf(os.Stderr, "invalid start date: %v\n", err)
			os.Exit(1)
		}
	} else {
		start = end.Add(-time.Duration(*days) * 24 * time.Hour)
	}

	// Determine intervals to backfill
	var intervals []string
	if *interval != "" {
		intervals = strings.Split(*interval, ",")
	} else {
		// Default: all commonly needed intervals
		intervals = []string{"15m", "1h", "4h", "1d", "1w"}
	}

	fmt.Printf("=== Kline Backfill ===\n")
	fmt.Printf("Symbol:    %s\n", *symbol)
	fmt.Printf("Range:     %s to %s\n", start.Format("2006-01-02"), end.Format("2006-01-02"))
	fmt.Printf("Intervals: %s\n\n", strings.Join(intervals, ", "))

	for _, ivl := range intervals {
		ivl = strings.TrimSpace(ivl)
		if ivl == "" {
			continue
		}

		// Check existing data coverage
		earliest, _ := store.GetEarliestKline(ctx, *symbol, ivl)
		latest, _ := store.GetLatestKline(ctx, *symbol, ivl)

		if earliest != nil && latest != nil {
			fmt.Printf("[%s] DB: %s to %s\n", ivl,
				earliest.OpenTime.Format("2006-01-02 15:04"),
				latest.OpenTime.Format("2006-01-02 15:04"))
		} else {
			fmt.Printf("[%s] DB: empty\n", ivl)
		}

		// Always fetch from the requested start date
		// The DB upsert (ON CONFLICT DO UPDATE) handles dedup
		fetchStart := start
		fmt.Printf("[%s] Fetching from %s (upsert mode)\n", ivl, fetchStart.Format("2006-01-02"))

		total, err := backfillInterval(ctx, client, store, *symbol, ivl, fetchStart, end)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[%s] ERROR: %v\n", ivl, err)
			// Continue with next interval instead of exiting
			continue
		}
		fmt.Printf("[%s] Done. %d klines saved.\n\n", ivl, total)
	}

	fmt.Println("=== All intervals complete ===")
}

func backfillInterval(
	ctx context.Context,
	client *binance.Client,
	store *timescale.Store,
	symbol, interval string,
	start, end time.Time,
) (int, error) {
	var total int
	cur := start
	batchSize := 500 // Save to DB every 500 klines for memory efficiency

	for cur.Before(end) {
		curCopy := cur
		endCopy := end
		req := exchange.KlineRequest{
			Symbol:    symbol,
			Interval:  interval,
			StartTime: &curCopy,
			EndTime:   &endCopy,
			Limit:     binanceLimit,
		}

		klines, err := client.GetKlines(ctx, req)
		if err != nil {
			return total, fmt.Errorf("fetch klines at %s: %w", cur.Format("2006-01-02 15:04"), err)
		}

		if len(klines) == 0 {
			break
		}

		// Save in batches
		for i := 0; i < len(klines); i += batchSize {
			end := i + batchSize
			if end > len(klines) {
				end = len(klines)
			}
			if err := store.SaveKlines(ctx, klines[i:end]); err != nil {
				return total, fmt.Errorf("save klines: %w", err)
			}
		}

		total += len(klines)
		lastTime := klines[len(klines)-1].OpenTime
		newCur := lastTime.Add(intervalDuration(interval))

		fmt.Printf("  [%s] +%d (total %d) → %s\n",
			interval, len(klines), total, lastTime.Format("2006-01-02 15:04"))

		if !newCur.After(cur) {
			// No progress, break to avoid infinite loop
			break
		}
		cur = newCur

		if len(klines) < binanceLimit {
			break
		}

		// Rate limiting: Binance allows 1200 req/min, be conservative
		time.Sleep(250 * time.Millisecond)
	}

	return total, nil
}
