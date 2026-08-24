CREATE UNIQUE INDEX IF NOT EXISTS ux_klines_symbol_interval_time
    ON klines (symbol, interval, time);
