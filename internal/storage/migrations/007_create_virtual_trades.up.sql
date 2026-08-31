CREATE TABLE IF NOT EXISTS virtual_trades (
    time          TIMESTAMPTZ      NOT NULL,
    id            BIGSERIAL,
    symbol        TEXT             NOT NULL,
    side          TEXT             NOT NULL, -- SHORT | COVER | BUY | SELL (direction being virtually tracked)
    price         DOUBLE PRECISION NOT NULL,
    quantity      DOUBLE PRECISION NOT NULL,
    fee           DOUBLE PRECISION,
    pnl           DOUBLE PRECISION,          -- nullable, only set on close (COVER/SELL)
    equity_after  DOUBLE PRECISION,          -- running virtual equity, only meaningful on close
    strategy_name TEXT,
    reason        TEXT
);

SELECT create_hypertable('virtual_trades', 'time', if_not_exists => TRUE);

CREATE INDEX IF NOT EXISTS idx_virtual_trades_symbol_side_time ON virtual_trades (symbol, side, time DESC);
CREATE INDEX IF NOT EXISTS idx_virtual_trades_strategy ON virtual_trades (strategy_name, time DESC);
