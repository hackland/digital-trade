CREATE TABLE IF NOT EXISTS orders (
    id              BIGSERIAL PRIMARY KEY,
    exchange_id     BIGINT,
    client_order_id TEXT,
    symbol          TEXT             NOT NULL,
    side            TEXT             NOT NULL,
    type            TEXT             NOT NULL,
    status          TEXT             NOT NULL,
    price           DOUBLE PRECISION,
    quantity        DOUBLE PRECISION NOT NULL,
    filled_qty      DOUBLE PRECISION DEFAULT 0,
    avg_price       DOUBLE PRECISION,
    stop_price      DOUBLE PRECISION,
    strategy_name   TEXT,
    signal_reason   TEXT,
    created_at      TIMESTAMPTZ      NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ      NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_orders_symbol_status ON orders (symbol, status);
CREATE INDEX IF NOT EXISTS idx_orders_created ON orders (created_at DESC);
