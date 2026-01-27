-- Railway PostgreSQL initialization (without TimescaleDB features)
-- This is a simplified version for Railway deployment

-- Candles table
CREATE TABLE IF NOT EXISTS candles_1m (
    time TIMESTAMPTZ NOT NULL,
    symbol TEXT NOT NULL,
    open DOUBLE PRECISION NOT NULL,
    high DOUBLE PRECISION NOT NULL,
    low DOUBLE PRECISION NOT NULL,
    close DOUBLE PRECISION NOT NULL,
    volume DOUBLE PRECISION NOT NULL,
    quote_volume DOUBLE PRECISION NOT NULL,
    num_trades INTEGER NOT NULL,
    PRIMARY KEY (time, symbol)
);

-- Create indexes for query performance
CREATE INDEX IF NOT EXISTS idx_candles_symbol_time ON candles_1m (symbol, time DESC);

-- Metrics calculated table
CREATE TABLE IF NOT EXISTS metrics_calculated (
    time TIMESTAMPTZ NOT NULL,
    symbol TEXT NOT NULL,
    timeframe TEXT NOT NULL,
    open DOUBLE PRECISION NOT NULL,
    high DOUBLE PRECISION NOT NULL,
    low DOUBLE PRECISION NOT NULL,
    close DOUBLE PRECISION NOT NULL,
    volume DOUBLE PRECISION NOT NULL,
    quote_volume DOUBLE PRECISION NOT NULL,
    num_trades INTEGER NOT NULL,
    vcp DOUBLE PRECISION,
    rsi DOUBLE PRECISION,
    macd DOUBLE PRECISION,
    macd_signal DOUBLE PRECISION,
    macd_histogram DOUBLE PRECISION,
    pivot DOUBLE PRECISION,
    resistance1 DOUBLE PRECISION,
    resistance0618 DOUBLE PRECISION,
    resistance0382 DOUBLE PRECISION,
    support0382 DOUBLE PRECISION,
    support0618 DOUBLE PRECISION,
    support1 DOUBLE PRECISION,
    weighted_avg_price DOUBLE PRECISION,
    price_change_percent DOUBLE PRECISION,
    PRIMARY KEY (time, symbol, timeframe)
);

CREATE INDEX IF NOT EXISTS idx_metrics_symbol_timeframe_time ON metrics_calculated (symbol, timeframe, time DESC);

-- Alert history table
CREATE TABLE IF NOT EXISTS alert_history (
    id SERIAL PRIMARY KEY,
    time TIMESTAMPTZ NOT NULL,
    symbol TEXT NOT NULL,
    rule_type TEXT NOT NULL,
    timeframe TEXT NOT NULL,
    message TEXT,
    metadata JSONB
);

CREATE INDEX IF NOT EXISTS idx_alert_history_time ON alert_history (time DESC);
CREATE INDEX IF NOT EXISTS idx_alert_history_symbol ON alert_history (symbol, time DESC);

-- User settings table (for metadata)
CREATE TABLE IF NOT EXISTS user_settings (
    user_id UUID PRIMARY KEY,
    email TEXT,
    notification_preferences JSONB,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

-- Alert rules table (for metadata)
CREATE TABLE IF NOT EXISTS alert_rules (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID REFERENCES user_settings(user_id),
    symbol TEXT NOT NULL,
    rule_type TEXT NOT NULL,
    timeframe TEXT NOT NULL,
    enabled BOOLEAN DEFAULT true,
    conditions JSONB,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_alert_rules_user_id ON alert_rules (user_id);
CREATE INDEX IF NOT EXISTS idx_alert_rules_symbol ON alert_rules (symbol);
