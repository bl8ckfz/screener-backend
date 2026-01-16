-- Initialize TimescaleDB extension and create hypertables

-- Enable TimescaleDB extension
CREATE EXTENSION IF NOT EXISTS timescaledb;

-- 1-Minute Candles (compressed hypertable)
CREATE TABLE IF NOT EXISTS candles_1m (
  time TIMESTAMPTZ NOT NULL,
  symbol TEXT NOT NULL,
  open DOUBLE PRECISION,
  high DOUBLE PRECISION,
  low DOUBLE PRECISION,
  close DOUBLE PRECISION,
  volume DOUBLE PRECISION,
  quote_volume DOUBLE PRECISION,
  trades INTEGER,
  PRIMARY KEY (time, symbol)
);

-- Create hypertable (TimescaleDB extension)
SELECT create_hypertable('candles_1m', 'time', if_not_exists => TRUE);

-- Retention policy: 48 hours
SELECT add_retention_policy('candles_1m', INTERVAL '48 hours', if_not_exists => TRUE);

-- Compression policy (after 1 hour)
ALTER TABLE candles_1m SET (
  timescaledb.compress,
  timescaledb.compress_segmentby = 'symbol'
);
SELECT add_compression_policy('candles_1m', INTERVAL '1 hour', if_not_exists => TRUE);

-- Calculated Metrics (enriched data)
CREATE TABLE IF NOT EXISTS metrics_calculated (
  time TIMESTAMPTZ NOT NULL,
  symbol TEXT NOT NULL,
  timeframe TEXT NOT NULL, -- '5m', '15m', '1h', '8h', '1d'
  
  -- Price metrics
  open DOUBLE PRECISION,
  high DOUBLE PRECISION,
  low DOUBLE PRECISION,
  close DOUBLE PRECISION,
  volume DOUBLE PRECISION,
  
  -- Technical indicators
  vcp DOUBLE PRECISION,
  rsi_14 DOUBLE PRECISION,
  macd DOUBLE PRECISION,
  macd_signal DOUBLE PRECISION,
  bb_upper DOUBLE PRECISION,
  bb_middle DOUBLE PRECISION,
  bb_lower DOUBLE PRECISION,
  
  -- Fibonacci levels
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

-- Alert History (triggered alerts)
CREATE TABLE IF NOT EXISTS alert_history (
  time TIMESTAMPTZ NOT NULL,
  symbol TEXT NOT NULL,
  rule_type TEXT NOT NULL,
  description TEXT,
  price DOUBLE PRECISION NOT NULL,
  metadata JSONB,
  
  PRIMARY KEY (time, symbol, rule_type)
);

SELECT create_hypertable('alert_history', 'time', if_not_exists => TRUE);
SELECT add_retention_policy('alert_history', INTERVAL '48 hours', if_not_exists => TRUE);

-- Compression policy (after 1 hour)
ALTER TABLE alert_history SET (
  timescaledb.compress,
  timescaledb.compress_segmentby = 'symbol'
);
SELECT add_compression_policy('alert_history', INTERVAL '1 hour', if_not_exists => TRUE);

-- Index for fast queries
CREATE INDEX IF NOT EXISTS idx_alert_history_symbol ON alert_history (symbol, time DESC);
CREATE INDEX IF NOT EXISTS idx_alert_history_type ON alert_history (rule_type, time DESC);

-- Grant permissions
GRANT ALL PRIVILEGES ON ALL TABLES IN SCHEMA public TO crypto_user;
