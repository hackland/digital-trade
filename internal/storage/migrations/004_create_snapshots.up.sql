CREATE TABLE IF NOT EXISTS account_snapshots (
    time              TIMESTAMPTZ      NOT NULL,
    total_equity      DOUBLE PRECISION NOT NULL,
    free_cash         DOUBLE PRECISION NOT NULL,
    position_value    DOUBLE PRECISION,
    unrealized_pnl    DOUBLE PRECISION,
    realized_pnl      DOUBLE PRECISION,
    daily_pnl         DOUBLE PRECISION,
    drawdown_pct      DOUBLE PRECISION,
    positions         JSONB
);

SELECT create_hypertable('account_snapshots', 'time', if_not_exists => TRUE);
