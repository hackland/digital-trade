package timescale

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jayce/btc-trader/internal/config"
	"github.com/jayce/btc-trader/internal/storage"
	"go.uber.org/zap"
)

// Store implements storage.Store using TimescaleDB.
type Store struct {
	pool   *pgxpool.Pool
	logger *zap.Logger
}

// New creates a new TimescaleDB store.
func New(ctx context.Context, cfg config.DatabaseConfig, logger *zap.Logger) (*Store, error) {
	poolCfg, err := pgxpool.ParseConfig(cfg.DSN)
	if err != nil {
		return nil, fmt.Errorf("parse dsn: %w", err)
	}

	poolCfg.MaxConns = int32(cfg.MaxOpenConns)
	poolCfg.MinConns = int32(cfg.MaxIdleConns)
	poolCfg.MaxConnLifetime = cfg.ConnMaxLifetime

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping db: %w", err)
	}

	logger.Info("connected to TimescaleDB")

	return &Store{
		pool:   pool,
		logger: logger,
	}, nil
}

// Close closes the database connection pool.
func (s *Store) Close() error {
	s.pool.Close()
	return nil
}

// Migrate runs all SQL migration files in order.
func (s *Store) Migrate(ctx context.Context) error {
	// Ensure TimescaleDB extension is enabled
	_, err := s.pool.Exec(ctx, "CREATE EXTENSION IF NOT EXISTS timescaledb")
	if err != nil {
		return fmt.Errorf("enable timescaledb extension: %w", err)
	}

	// Read and execute migration files in order using the embed.FS from storage package
	migrations := []string{
		"migrations/001_create_klines.up.sql",
		"migrations/002_create_orders.up.sql",
		"migrations/003_create_trades.up.sql",
		"migrations/004_create_snapshots.up.sql",
		"migrations/005_create_signals.up.sql",
	}

	for _, path := range migrations {
		data, err := storage.MigrationsFS.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", path, err)
		}

		_, err = s.pool.Exec(ctx, string(data))
		if err != nil {
			return fmt.Errorf("execute migration %s: %w", path, err)
		}

		s.logger.Info("migration applied", zap.String("file", path))
	}

	return nil
}

// Pool returns the underlying connection pool for use by sub-repositories.
func (s *Store) Pool() *pgxpool.Pool {
	return s.pool
}

// Suppress unused import warning
var _ = time.Now
