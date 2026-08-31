package timescale

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jayce/btc-trader/internal/storage"
)

// SaveVirtualTrade inserts a virtual (alert-only) trade leg.
func (s *Store) SaveVirtualTrade(ctx context.Context, trade *storage.VirtualTradeRecord) error {
	query := `
		INSERT INTO virtual_trades (time, symbol, side, price, quantity, fee, pnl, equity_after, strategy_name, reason)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`
	_, err := s.pool.Exec(ctx, query,
		trade.Timestamp, trade.Symbol, trade.Side, trade.Price, trade.Quantity,
		trade.Fee, trade.PnL, trade.EquityAfter, trade.StrategyName, trade.Reason,
	)
	if err != nil {
		return fmt.Errorf("insert virtual trade: %w", err)
	}
	return nil
}

// GetVirtualTrades retrieves virtual trades matching the filter.
func (s *Store) GetVirtualTrades(ctx context.Context, filter storage.VirtualTradeFilter) ([]storage.VirtualTradeRecord, error) {
	query := `
		SELECT time, id, symbol, side, price, quantity, fee, pnl, equity_after, strategy_name, reason
		FROM virtual_trades
		WHERE 1=1
	`
	args := []interface{}{}
	argIdx := 1

	if filter.Symbol != "" {
		query += fmt.Sprintf(" AND symbol = $%d", argIdx)
		args = append(args, filter.Symbol)
		argIdx++
	}
	if filter.Side != "" {
		query += fmt.Sprintf(" AND side = $%d", argIdx)
		args = append(args, filter.Side)
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
		return nil, fmt.Errorf("query virtual trades: %w", err)
	}
	defer rows.Close()

	return scanVirtualTrades(rows)
}

// GetLatestVirtualTrade returns the single most recent virtual trade leg for
// a symbol+side (e.g. side="SHORT" for the short-tracking ledger, or "BUY"
// for the signal_only-blocked long ledger). Used on startup to recover
// in-flight virtual position state. Returns nil, nil if none exist.
func (s *Store) GetLatestVirtualTrade(ctx context.Context, symbol, side string) (*storage.VirtualTradeRecord, error) {
	query := `
		SELECT time, id, symbol, side, price, quantity, fee, pnl, equity_after, strategy_name, reason
		FROM virtual_trades
		WHERE symbol = $1 AND side = $2
		ORDER BY time DESC
		LIMIT 1
	`
	rows, err := s.pool.Query(ctx, query, symbol, side)
	if err != nil {
		return nil, fmt.Errorf("query latest virtual trade: %w", err)
	}
	defer rows.Close()

	trades, err := scanVirtualTrades(rows)
	if err != nil {
		return nil, err
	}
	if len(trades) == 0 {
		return nil, nil
	}
	return &trades[0], nil
}

func scanVirtualTrades(rows pgx.Rows) ([]storage.VirtualTradeRecord, error) {
	var trades []storage.VirtualTradeRecord
	for rows.Next() {
		var t storage.VirtualTradeRecord
		err := rows.Scan(
			&t.Timestamp, &t.ID, &t.Symbol, &t.Side, &t.Price, &t.Quantity,
			&t.Fee, &t.PnL, &t.EquityAfter, &t.StrategyName, &t.Reason,
		)
		if err != nil {
			return nil, fmt.Errorf("scan virtual trade: %w", err)
		}
		trades = append(trades, t)
	}
	return trades, rows.Err()
}
