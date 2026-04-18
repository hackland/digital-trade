package timescale

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jayce/btc-trader/internal/storage"
)

// SaveOrder inserts a new order record.
func (s *Store) SaveOrder(ctx context.Context, order *storage.OrderRecord) error {
	query := `
		INSERT INTO orders (exchange_id, client_order_id, symbol, side, type, status, price, quantity, filled_qty, avg_price, stop_price, strategy_name, signal_reason, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
		RETURNING id
	`
	err := s.pool.QueryRow(ctx, query,
		order.ExchangeID, order.ClientOrderID, order.Symbol,
		order.Side, order.Type, order.Status,
		order.Price, order.Quantity, order.FilledQty,
		order.AvgPrice, order.StopPrice,
		order.StrategyName, order.SignalReason,
		order.CreatedAt, order.UpdatedAt,
	).Scan(&order.ID)
	if err != nil {
		return fmt.Errorf("insert order: %w", err)
	}
	return nil
}

// UpdateOrder updates an existing order record.
// Matches by exchange_id (Binance order ID) when ExchangeID is set,
// otherwise falls back to DB primary key.
func (s *Store) UpdateOrder(ctx context.Context, order *storage.OrderRecord) error {
	query := `
		UPDATE orders
		SET status = $1, filled_qty = $2, avg_price = $3, updated_at = $4
		WHERE exchange_id = $5
	`
	matchID := order.ExchangeID
	if matchID == 0 {
		query = `
			UPDATE orders
			SET status = $1, filled_qty = $2, avg_price = $3, updated_at = $4
			WHERE id = $5
		`
		matchID = order.ID
	}
	_, err := s.pool.Exec(ctx, query,
		order.Status, order.FilledQty, order.AvgPrice, order.UpdatedAt, matchID,
	)
	if err != nil {
		return fmt.Errorf("update order: %w", err)
	}
	return nil
}

// GetOrder retrieves an order by ID.
func (s *Store) GetOrder(ctx context.Context, orderID int64) (*storage.OrderRecord, error) {
	query := `
		SELECT id, exchange_id, client_order_id, symbol, side, type, status, price, quantity, filled_qty, avg_price, stop_price, strategy_name, signal_reason, created_at, updated_at
		FROM orders WHERE id = $1
	`
	var o storage.OrderRecord
	err := s.pool.QueryRow(ctx, query, orderID).Scan(
		&o.ID, &o.ExchangeID, &o.ClientOrderID, &o.Symbol,
		&o.Side, &o.Type, &o.Status,
		&o.Price, &o.Quantity, &o.FilledQty,
		&o.AvgPrice, &o.StopPrice,
		&o.StrategyName, &o.SignalReason,
		&o.CreatedAt, &o.UpdatedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("query order: %w", err)
	}
	return &o, nil
}

// GetOpenOrders retrieves all open orders for a symbol.
func (s *Store) GetOpenOrders(ctx context.Context, symbol string) ([]storage.OrderRecord, error) {
	return s.GetOrders(ctx, storage.OrderFilter{
		Symbol: symbol,
		Status: "NEW",
	})
}

// GetOrders retrieves orders matching the filter.
func (s *Store) GetOrders(ctx context.Context, filter storage.OrderFilter) ([]storage.OrderRecord, error) {
	query := `
		SELECT id, exchange_id, client_order_id, symbol, side, type, status, price, quantity, filled_qty, avg_price, stop_price, strategy_name, signal_reason, created_at, updated_at
		FROM orders WHERE 1=1
	`
	args := []interface{}{}
	argIdx := 1

	if filter.Symbol != "" {
		query += fmt.Sprintf(" AND symbol = $%d", argIdx)
		args = append(args, filter.Symbol)
		argIdx++
	}
	if filter.Status != "" {
		query += fmt.Sprintf(" AND status = $%d", argIdx)
		args = append(args, filter.Status)
		argIdx++
	}
	if filter.StartTime != nil {
		query += fmt.Sprintf(" AND created_at >= $%d", argIdx)
		args = append(args, *filter.StartTime)
		argIdx++
	}
	if filter.EndTime != nil {
		query += fmt.Sprintf(" AND created_at <= $%d", argIdx)
		args = append(args, *filter.EndTime)
		argIdx++
	}

	query += " ORDER BY created_at DESC"

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
		return nil, fmt.Errorf("query orders: %w", err)
	}
	defer rows.Close()

	var orders []storage.OrderRecord
	for rows.Next() {
		var o storage.OrderRecord
		err := rows.Scan(
			&o.ID, &o.ExchangeID, &o.ClientOrderID, &o.Symbol,
			&o.Side, &o.Type, &o.Status,
			&o.Price, &o.Quantity, &o.FilledQty,
			&o.AvgPrice, &o.StopPrice,
			&o.StrategyName, &o.SignalReason,
			&o.CreatedAt, &o.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan order: %w", err)
		}
		orders = append(orders, o)
	}
	return orders, rows.Err()
}
