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

-- Alert rules table (global system rules)
CREATE TABLE IF NOT EXISTS alert_rules (
    rule_type TEXT PRIMARY KEY,
    config JSONB NOT NULL DEFAULT '{}',
    description TEXT,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

-- Insert default alert rules
INSERT INTO alert_rules (rule_type, config, description) VALUES
  ('futures_big_bull_60', '{}', '60 Big Bull - Sustained momentum over multiple timeframes'),
  ('futures_big_bear_60', '{}', '60 Big Bear - Sustained downward momentum'),
  ('futures_pioneer_bull', '{}', 'Pioneer Bull - Early bullish trend detection'),
  ('futures_pioneer_bear', '{}', 'Pioneer Bear - Early bearish trend detection'),
  ('futures_5_big_bull', '{}', '5 Big Bull - 5-minute bullish spike'),
  ('futures_5_big_bear', '{}', '5 Big Bear - 5-minute bearish spike'),
  ('futures_15_big_bull', '{}', '15 Big Bull - 15-minute bullish spike'),
  ('futures_15_big_bear', '{}', '15 Big Bear - 15-minute bearish spike'),
  ('futures_bottom_hunter', '{}', 'Bottom Hunter - Potential bottom reversal'),
  ('futures_top_hunter', '{}', 'Top Hunter - Potential top reversal')
ON CONFLICT (rule_type) DO NOTHING;

-- User alert subscriptions (which rules users want to receive)
CREATE TABLE IF NOT EXISTS user_alert_subscriptions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID REFERENCES user_settings(user_id),
    rule_type TEXT REFERENCES alert_rules(rule_type),
    symbol TEXT,  -- NULL means all symbols
    enabled BOOLEAN DEFAULT true,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_user_alert_subscriptions_user_id ON user_alert_subscriptions (user_id);
CREATE INDEX IF NOT EXISTS idx_user_alert_subscriptions_rule_type ON user_alert_subscriptions (rule_type);
