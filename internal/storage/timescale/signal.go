package timescale

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jayce/btc-trader/internal/storage"
	"github.com/jayce/btc-trader/internal/strategy"
)

// SaveSignal inserts a strategy signal record.
func (s *Store) SaveSignal(ctx context.Context, sig *strategy.Signal, wasExecuted bool) error {
	indJSON, err := json.Marshal(sig.Indicators)
	if err != nil {
		return fmt.Errorf("marshal indicators: %w", err)
	}

	query := `
		INSERT INTO signals (time, symbol, strategy_name, action, strength, reason, indicators, was_executed)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`
	_, err = s.pool.Exec(ctx, query,
		sig.Timestamp, sig.Symbol, sig.Strategy,
		sig.Action.String(), sig.Strength, sig.Reason,
		indJSON, wasExecuted,
	)
	if err != nil {
		return fmt.Errorf("insert signal: %w", err)
	}
	return nil
}

// GetSignals retrieves signals matching the filter.
func (s *Store) GetSignals(ctx context.Context, filter storage.SignalFilter) ([]storage.SignalRecord, error) {
	query := `
		SELECT time, id, symbol, strategy_name, action, strength, reason, indicators, was_executed
		FROM signals WHERE 1=1
	`
	args := []interface{}{}
	argIdx := 1

	if filter.Symbol != "" {
		query += fmt.Sprintf(" AND symbol = $%d", argIdx)
		args = append(args, filter.Symbol)
		argIdx++
	}
	if filter.StrategyName != "" {
		query += fmt.Sprintf(" AND strategy_name = $%d", argIdx)
		args = append(args, filter.StrategyName)
		argIdx++
	}
	if filter.Action != "" {
		query += fmt.Sprintf(" AND action = $%d", argIdx)
		args = append(args, filter.Action)
		argIdx++
	}
	if filter.StartTime != nil {
		query += fmt.Sprintf(" AND time >= $%d", argIdx)
		args = append(args, *filter.StartTime)
		argIdx++
	}
	if filter.EndTime != nil {
		query += fmt.Sprintf(" AND time <= $%d", argIdx)
		args = append(args, *filter.EndTime)
		argIdx++
	}

	query += " ORDER BY time DESC"

	if filter.Limit > 0 {
		query += fmt.Sprintf(" LIMIT $%d", argIdx)
		args = append(args, filter.Limit)
		argIdx++
	}
	if filter.Offset > 0 {
		query += fmt.Sprintf(" OFFSET $%d", argIdx)
		args = append(args, filter.Offset)
		argIdx++
	}

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query signals: %w", err)
	}
	defer rows.Close()

	return scanSignals(rows)
}

func scanSignals(rows pgx.Rows) ([]storage.SignalRecord, error) {
	var signals []storage.SignalRecord
	for rows.Next() {
		var sig storage.SignalRecord
		var indJSON []byte
		err := rows.Scan(
			&sig.Timestamp, &sig.ID, &sig.Symbol,
			&sig.StrategyName, &sig.Action, &sig.Strength,
			&sig.Reason, &indJSON, &sig.WasExecuted,
		)
		if err != nil {
			return nil, fmt.Errorf("scan signal: %w", err)
		}
		if indJSON != nil {
			sig.Indicators = make(map[string]float64)
			json.Unmarshal(indJSON, &sig.Indicators)
		}
		signals = append(signals, sig)
	}
	return signals, rows.Err()
}
