package timescale

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jayce/btc-trader/internal/storage"
)

// SaveTrade inserts a trade record.
func (s *Store) SaveTrade(ctx context.Context, trade *storage.TradeRecord) error {
	query := `
		INSERT INTO trades (time, exchange_id, order_id, symbol, side, price, quantity, fee, fee_asset, strategy_name, realized_pnl)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
	`
	_, err := s.pool.Exec(ctx, query,
		trade.Timestamp, trade.ExchangeID, trade.OrderID,
		trade.Symbol, trade.Side, trade.Price, trade.Quantity,
		trade.Fee, trade.FeeAsset, trade.StrategyName, trade.RealizedPnL,
	)
	if err != nil {
		return fmt.Errorf("insert trade: %w", err)
	}
	return nil
}

// GetTrades retrieves trades matching the filter.
func (s *Store) GetTrades(ctx context.Context, filter storage.TradeFilter) ([]storage.TradeRecord, error) {
	query := `
		SELECT time, id, exchange_id, order_id, symbol, side, price, quantity, fee, fee_asset, strategy_name, realized_pnl
		FROM trades
		WHERE 1=1
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
		return nil, fmt.Errorf("query trades: %w", err)
	}
	defer rows.Close()

	return scanTrades(rows)
}

// GetTradesByDateRange retrieves trades for a symbol within a date range.
func (s *Store) GetTradesByDateRange(ctx context.Context, symbol string, start, end time.Time) ([]storage.TradeRecord, error) {
	return s.GetTrades(ctx, storage.TradeFilter{
		Symbol:    symbol,
		StartTime: &start,
		EndTime:   &end,
	})
}

func scanTrades(rows pgx.Rows) ([]storage.TradeRecord, error) {
	var trades []storage.TradeRecord
	for rows.Next() {
		var t storage.TradeRecord
		err := rows.Scan(
			&t.Timestamp, &t.ID, &t.ExchangeID, &t.OrderID,
			&t.Symbol, &t.Side, &t.Price, &t.Quantity,
			&t.Fee, &t.FeeAsset, &t.StrategyName, &t.RealizedPnL,
		)
		if err != nil {
			return nil, fmt.Errorf("scan trade: %w", err)
		}
		trades = append(trades, t)
	}
	return trades, rows.Err()
}
