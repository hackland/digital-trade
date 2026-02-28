CREATE TABLE IF NOT EXISTS klines (
    time         TIMESTAMPTZ      NOT NULL,
    symbol       TEXT             NOT NULL,
    interval     TEXT             NOT NULL,
    open         DOUBLE PRECISION NOT NULL,
    high         DOUBLE PRECISION NOT NULL,
    low          DOUBLE PRECISION NOT NULL,
    close        DOUBLE PRECISION NOT NULL,
    volume       DOUBLE PRECISION NOT NULL,
    quote_volume DOUBLE PRECISION,
    trades       INTEGER,
    is_final     BOOLEAN DEFAULT TRUE
);

SELECT create_hypertable('klines', 'time', if_not_exists => TRUE);

CREATE INDEX IF NOT EXISTS idx_klines_symbol_interval_time
    ON klines (symbol, interval, time DESC);

-- Compression policy: compress data older than 7 days
ALTER TABLE klines SET (
    timescaledb.compress,
    timescaledb.compress_segmentby = 'symbol,interval'
);

SELECT add_compression_policy('klines', INTERVAL '7 days', if_not_exists => TRUE);
