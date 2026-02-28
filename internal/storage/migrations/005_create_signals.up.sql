CREATE TABLE IF NOT EXISTS signals (
    time          TIMESTAMPTZ      NOT NULL,
    id            BIGSERIAL,
    symbol        TEXT             NOT NULL,
    strategy_name TEXT             NOT NULL,
    action        TEXT             NOT NULL,
    strength      DOUBLE PRECISION,
    reason        TEXT,
    indicators    JSONB,
    was_executed  BOOLEAN DEFAULT FALSE
);

SELECT create_hypertable('signals', 'time', if_not_exists => TRUE);

CREATE INDEX IF NOT EXISTS idx_signals_strategy_time ON signals (strategy_name, time DESC);
