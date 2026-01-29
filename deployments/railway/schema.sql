-- AUTHORITATIVE DATABASE SCHEMA FOR RAILWAY/TIMESCALEDB
-- This is the ONLY schema file used in production
-- Last updated: 2026-01-29

-- Enable TimescaleDB extension
CREATE EXTENSION IF NOT EXISTS timescaledb;

-- ============================================================================
-- TIME-SERIES DATA (TimescaleDB Hypertables)
-- ============================================================================

-- 1-Minute Candles (compressed hypertable)
-- Retention: 48 hours
CREATE TABLE IF NOT EXISTS candles_1m (
  time TIMESTAMPTZ NOT NULL,
  symbol TEXT NOT NULL,
  open DOUBLE PRECISION NOT NULL,
  high DOUBLE PRECISION NOT NULL,
  low DOUBLE PRECISION NOT NULL,
  close DOUBLE PRECISION NOT NULL,
  volume DOUBLE PRECISION NOT NULL,
  quote_volume DOUBLE PRECISION NOT NULL,
  trades INTEGER NOT NULL,  -- Column name: 'trades' (NOT num_trades or number_of_trades)
  PRIMARY KEY (time, symbol)
);

SELECT create_hypertable('candles_1m', 'time', if_not_exists => TRUE);
SELECT add_retention_policy('candles_1m', INTERVAL '48 hours', if_not_exists => TRUE);
ALTER TABLE candles_1m SET (
  timescaledb.compress,
  timescaledb.compress_segmentby = 'symbol'
);
SELECT add_compression_policy('candles_1m', INTERVAL '1 hour', if_not_exists => TRUE);

CREATE INDEX IF NOT EXISTS idx_candles_symbol_time ON candles_1m (symbol, time DESC);

-- Calculated Metrics (enriched data)
-- Retention: 48 hours
-- Timeframes: 5m, 15m, 1h, 4h, 8h, 1d
CREATE TABLE IF NOT EXISTS metrics_calculated (
  time TIMESTAMPTZ NOT NULL,
  symbol TEXT NOT NULL,
  timeframe TEXT NOT NULL,
  
  -- OHLCV
  open DOUBLE PRECISION NOT NULL,
  high DOUBLE PRECISION NOT NULL,
  low DOUBLE PRECISION NOT NULL,
  close DOUBLE PRECISION NOT NULL,
  volume DOUBLE PRECISION NOT NULL,
  
  -- Percentage changes (vs previous period)
  price_change DOUBLE PRECISION,
  volume_ratio DOUBLE PRECISION,
  
  -- Technical indicators
  vcp DOUBLE PRECISION,
  rsi_14 DOUBLE PRECISION,
  macd DOUBLE PRECISION,
  macd_signal DOUBLE PRECISION,
  
  -- Fibonacci pivot levels
  fib_r3 DOUBLE PRECISION,
  fib_r2 DOUBLE PRECISION,
  fib_r1 DOUBLE PRECISION,
  fib_pivot DOUBLE PRECISION,
  fib_s1 DOUBLE PRECISION,
  fib_s2 DOUBLE PRECISION,
  fib_s3 DOUBLE PRECISION,
  
  PRIMARY KEY (time, symbol, timeframe)
);

SELECT create_hypertable('metrics_calculated', 'time', if_not_exists => TRUE);
SELECT add_retention_policy('metrics_calculated', INTERVAL '48 hours', if_not_exists => TRUE);

CREATE INDEX IF NOT EXISTS idx_metrics_symbol_timeframe ON metrics_calculated (symbol, timeframe, time DESC);

-- ============================================================================
-- ALERT SYSTEM TABLES
-- ============================================================================

-- Alert History (triggered alerts log)
CREATE TABLE IF NOT EXISTS alert_history (
  id SERIAL PRIMARY KEY,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  symbol TEXT NOT NULL,
  rule_type TEXT NOT NULL,
  price DOUBLE PRECISION,
  message TEXT,
  metadata JSONB
);

CREATE INDEX IF NOT EXISTS idx_alert_history_time ON alert_history (created_at DESC);
CREATE INDEX IF NOT EXISTS idx_alert_history_symbol ON alert_history (symbol, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_alert_history_rule_type ON alert_history (rule_type, created_at DESC);

-- Alert Rules (global system rules)
CREATE TABLE IF NOT EXISTS alert_rules (
  rule_type TEXT PRIMARY KEY,
  enabled BOOLEAN DEFAULT true,
  config JSONB NOT NULL DEFAULT '{}',
  description TEXT,
  created_at TIMESTAMPTZ DEFAULT NOW(),
  updated_at TIMESTAMPTZ DEFAULT NOW()
);

-- ============================================================================
-- METADATA TABLES (PostgreSQL)
-- ============================================================================

-- User Settings
CREATE TABLE IF NOT EXISTS user_settings (
  user_id UUID PRIMARY KEY,
  email TEXT,
  notification_preferences JSONB,
  created_at TIMESTAMPTZ DEFAULT NOW(),
  updated_at TIMESTAMPTZ DEFAULT NOW()
);

-- User Alert Subscriptions (which rules users want to receive)
CREATE TABLE IF NOT EXISTS user_alert_subscriptions (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id UUID NOT NULL REFERENCES user_settings(user_id) ON DELETE CASCADE,
  rule_type TEXT NOT NULL REFERENCES alert_rules(rule_type),
  enabled BOOLEAN DEFAULT true,
  created_at TIMESTAMPTZ DEFAULT NOW(),
  UNIQUE(user_id, rule_type)
);

CREATE INDEX IF NOT EXISTS idx_user_subscriptions ON user_alert_subscriptions (user_id, enabled);

-- ============================================================================
-- DEFAULT ALERT RULES
-- ============================================================================

INSERT INTO alert_rules (rule_type, enabled, config, description) VALUES
  ('futures_big_bull_60', true, '{}', '60 Big Bull - Sustained momentum over multiple timeframes'),
  ('futures_big_bear_60', true, '{}', '60 Big Bear - Sustained downward momentum'),
  ('futures_pioneer_bull', true, '{}', 'Pioneer Bull - Early bullish trend detection'),
  ('futures_pioneer_bear', true, '{}', 'Pioneer Bear - Early bearish trend detection'),
  ('futures_5_big_bull', true, '{}', '5 Big Bull - 5-minute bullish spike'),
  ('futures_5_big_bear', true, '{}', '5 Big Bear - 5-minute bearish spike'),
  ('futures_15_big_bull', true, '{}', '15 Big Bull - 15-minute bullish spike'),
  ('futures_15_big_bear', true, '{}', '15 Big Bear - 15-minute bearish spike'),
  ('futures_bottom_hunter', true, '{}', 'Bottom Hunter - Potential bottom reversal'),
  ('futures_top_hunter', true, '{}', 'Top Hunter - Potential top reversal')
ON CONFLICT (rule_type) DO NOTHING;
