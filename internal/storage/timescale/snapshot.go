package timescale

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jayce/btc-trader/internal/storage"
)

// SaveSnapshot inserts an account snapshot.
func (s *Store) SaveSnapshot(ctx context.Context, snap *storage.AccountSnapshot) error {
	posJSON, err := json.Marshal(snap.Positions)
	if err != nil {
		return fmt.Errorf("marshal positions: %w", err)
	}

	query := `
		INSERT INTO account_snapshots (time, total_equity, free_cash, position_value, unrealized_pnl, realized_pnl, daily_pnl, drawdown_pct, positions)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`
	_, err = s.pool.Exec(ctx, query,
		snap.Timestamp, snap.TotalEquity, snap.FreeCash,
		snap.PositionValue, snap.UnrealizedPnL, snap.RealizedPnL,
		snap.DailyPnL, snap.DrawdownPct, posJSON,
	)
	if err != nil {
		return fmt.Errorf("insert snapshot: %w", err)
	}
	return nil
}

// GetSnapshots retrieves account snapshots within a time range.
// The interval parameter controls time bucketing (e.g., "5 minutes", "1 hour").
func (s *Store) GetSnapshots(ctx context.Context, start, end time.Time, interval string) ([]storage.AccountSnapshot, error) {
	// Use time_bucket for downsampling if interval is provided
	var query string
	if interval != "" {
		query = fmt.Sprintf(`
			SELECT time_bucket('%s', time) AS bucket,
				LAST(total_equity, time), LAST(free_cash, time),
				LAST(position_value, time), LAST(unrealized_pnl, time),
				LAST(realized_pnl, time), LAST(daily_pnl, time),
				LAST(drawdown_pct, time), LAST(positions, time)
			FROM account_snapshots
			WHERE time >= $1 AND time <= $2
			GROUP BY bucket
			ORDER BY bucket ASC
		`, interval)
	} else {
		query = `
			SELECT time, total_equity, free_cash, position_value, unrealized_pnl, realized_pnl, daily_pnl, drawdown_pct, positions
			FROM account_snapshots
			WHERE time >= $1 AND time <= $2
			ORDER BY time ASC
		`
	}

	rows, err := s.pool.Query(ctx, query, start, end)
	if err != nil {
		return nil, fmt.Errorf("query snapshots: %w", err)
	}
	defer rows.Close()

	return scanSnapshots(rows)
}

// GetLatestSnapshot returns the most recent account snapshot.
func (s *Store) GetLatestSnapshot(ctx context.Context) (*storage.AccountSnapshot, error) {
	query := `
		SELECT time, total_equity, free_cash, position_value, unrealized_pnl, realized_pnl, daily_pnl, drawdown_pct, positions
		FROM account_snapshots
		ORDER BY time DESC
		LIMIT 1
	`
	var snap storage.AccountSnapshot
	var posJSON []byte

	err := s.pool.QueryRow(ctx, query).Scan(
		&snap.Timestamp, &snap.TotalEquity, &snap.FreeCash,
		&snap.PositionValue, &snap.UnrealizedPnL, &snap.RealizedPnL,
		&snap.DailyPnL, &snap.DrawdownPct, &posJSON,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("query latest snapshot: %w", err)
	}

	if posJSON != nil {
		snap.Positions = make(map[string]float64)
		if err := json.Unmarshal(posJSON, &snap.Positions); err != nil {
			return nil, fmt.Errorf("unmarshal positions: %w", err)
		}
	}

	return &snap, nil
}

func scanSnapshots(rows pgx.Rows) ([]storage.AccountSnapshot, error) {
	var snaps []storage.AccountSnapshot
	for rows.Next() {
		var snap storage.AccountSnapshot
		var posJSON []byte
		err := rows.Scan(
			&snap.Timestamp, &snap.TotalEquity, &snap.FreeCash,
			&snap.PositionValue, &snap.UnrealizedPnL, &snap.RealizedPnL,
			&snap.DailyPnL, &snap.DrawdownPct, &posJSON,
		)
		if err != nil {
			return nil, fmt.Errorf("scan snapshot: %w", err)
		}
		if posJSON != nil {
			snap.Positions = make(map[string]float64)
			json.Unmarshal(posJSON, &snap.Positions)
		}
		snaps = append(snaps, snap)
	}
	return snaps, rows.Err()
}
