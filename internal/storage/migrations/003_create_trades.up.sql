CREATE TABLE IF NOT EXISTS trades (
    time          TIMESTAMPTZ      NOT NULL,
    id            BIGSERIAL,
    exchange_id   BIGINT,
    order_id      BIGINT,
    symbol        TEXT             NOT NULL,
    side          TEXT             NOT NULL,
    price         DOUBLE PRECISION NOT NULL,
    quantity      DOUBLE PRECISION NOT NULL,
    fee           DOUBLE PRECISION,
    fee_asset     TEXT,
    strategy_name TEXT,
    realized_pnl  DOUBLE PRECISION
);

SELECT create_hypertable('trades', 'time', if_not_exists => TRUE);

CREATE INDEX IF NOT EXISTS idx_trades_symbol_time ON trades (symbol, time DESC);
CREATE INDEX IF NOT EXISTS idx_trades_strategy ON trades (strategy_name, time DESC);
