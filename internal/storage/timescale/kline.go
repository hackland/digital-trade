package timescale

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jayce/btc-trader/internal/exchange"
	"go.uber.org/zap"
)

// SaveKlines upserts klines — on conflict (same time+symbol+interval), update the row.
func (s *Store) SaveKlines(ctx context.Context, klines []exchange.Kline) error {
	if len(klines) == 0 {
		return nil
	}

	batch := &pgx.Batch{}
	for _, k := range klines {
		batch.Queue(`
			INSERT INTO klines (time, symbol, interval, open, high, low, close, volume, quote_volume, trades, is_final)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
			ON CONFLICT (symbol, interval, time) DO UPDATE SET
				open = EXCLUDED.open, high = EXCLUDED.high, low = EXCLUDED.low,
				close = EXCLUDED.close, volume = EXCLUDED.volume, quote_volume = EXCLUDED.quote_volume,
				trades = EXCLUDED.trades, is_final = EXCLUDED.is_final
		`, k.OpenTime, k.Symbol, k.Interval,
			k.Open, k.High, k.Low, k.Close,
			k.Volume, k.QuoteVolume, k.Trades, k.IsFinal,
		)
	}

	br := s.pool.SendBatch(ctx, batch)
	defer br.Close()

	for range klines {
		if _, err := br.Exec(); err != nil {
			return fmt.Errorf("upsert kline: %w", err)
		}
	}

	s.logger.Debug("saved klines", zap.Int("count", len(klines)))
	return nil
}

// GetKlines retrieves klines for a symbol/interval within a time range.
// Returns the most recent `limit` distinct records ordered by time ascending (for charting).
func (s *Store) GetKlines(ctx context.Context, symbol, interval string, start, end time.Time, limit int) ([]exchange.Kline, error) {
	args := []interface{}{symbol, interval, start, end}

	var query string
	if limit > 0 {
		// Subquery: pick the latest N distinct rows (DESC), then re-sort ASC for chart display
		query = `
			SELECT time, symbol, interval, open, high, low, close, volume, quote_volume, trades, is_final
			FROM (
				SELECT DISTINCT ON (time) time, symbol, interval, open, high, low, close, volume, quote_volume, trades, is_final
				FROM klines
				WHERE symbol = $1 AND interval = $2 AND time >= $3 AND time <= $4
				ORDER BY time DESC
				LIMIT $5
			) sub
			ORDER BY time ASC
		`
		args = append(args, limit)
	} else {
		query = `
			SELECT DISTINCT ON (time) time, symbol, interval, open, high, low, close, volume, quote_volume, trades, is_final
			FROM klines
			WHERE symbol = $1 AND interval = $2 AND time >= $3 AND time <= $4
			ORDER BY time ASC
		`
	}

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query klines: %w", err)
	}
	defer rows.Close()

	var klines []exchange.Kline
	for rows.Next() {
		var k exchange.Kline
		err := rows.Scan(
			&k.OpenTime, &k.Symbol, &k.Interval,
			&k.Open, &k.High, &k.Low, &k.Close,
			&k.Volume, &k.QuoteVolume, &k.Trades, &k.IsFinal,
		)
		if err != nil {
			return nil, fmt.Errorf("scan kline: %w", err)
		}
		klines = append(klines, k)
	}

	return klines, rows.Err()
}

// GetEarliestKline returns the oldest kline for a symbol/interval.
func (s *Store) GetEarliestKline(ctx context.Context, symbol, interval string) (*exchange.Kline, error) {
	query := `
		SELECT time, symbol, interval, open, high, low, close, volume, quote_volume, trades, is_final
		FROM klines
		WHERE symbol = $1 AND interval = $2
		ORDER BY time ASC
		LIMIT 1
	`

	var k exchange.Kline
	err := s.pool.QueryRow(ctx, query, symbol, interval).Scan(
		&k.OpenTime, &k.Symbol, &k.Interval,
		&k.Open, &k.High, &k.Low, &k.Close,
		&k.Volume, &k.QuoteVolume, &k.Trades, &k.IsFinal,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("query earliest kline: %w", err)
	}

	return &k, nil
}

// GetLatestKline returns the most recent kline for a symbol/interval.
func (s *Store) GetLatestKline(ctx context.Context, symbol, interval string) (*exchange.Kline, error) {
	query := `
		SELECT time, symbol, interval, open, high, low, close, volume, quote_volume, trades, is_final
		FROM klines
		WHERE symbol = $1 AND interval = $2
		ORDER BY time DESC
		LIMIT 1
	`

	var k exchange.Kline
	err := s.pool.QueryRow(ctx, query, symbol, interval).Scan(
		&k.OpenTime, &k.Symbol, &k.Interval,
		&k.Open, &k.High, &k.Low, &k.Close,
		&k.Volume, &k.QuoteVolume, &k.Trades, &k.IsFinal,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("query latest kline: %w", err)
	}

	return &k, nil
}
