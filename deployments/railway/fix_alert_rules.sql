-- Fix alert_rules table schema for Railway deployment
-- This script adds the alert_rules table if it doesn't exist or recreates it with correct schema
-- Run this with: railway run psql $DATABASE_URL < deployments/railway/fix_alert_rules.sql

-- Drop the old alert_rules table if it exists (has wrong schema)
DROP TABLE IF EXISTS alert_rules CASCADE;

-- Create correct alert_rules table (global system rules)
CREATE TABLE alert_rules (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    rule_type TEXT NOT NULL UNIQUE,
    enabled BOOLEAN DEFAULT true,
    config JSONB NOT NULL DEFAULT '{}',
    description TEXT,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

-- Insert all 10 alert rule types with their configurations
INSERT INTO alert_rules (rule_type, enabled, config, description) VALUES
  (
    'futures_big_bull_60',
    true,
    '{"criteria": {}}',
    '60 Big Bull - Sustained momentum over multiple timeframes'
  ),
  (
    'futures_big_bear_60',
    true,
    '{"criteria": {}}',
    '60 Big Bear - Sustained downward momentum'
  ),
  (
    'futures_pioneer_bull',
    true,
    '{"criteria": {}}',
    'Pioneer Bull - Early bullish trend detection'
  ),
  (
    'futures_pioneer_bear',
    true,
    '{"criteria": {}}',
    'Pioneer Bear - Early bearish trend detection'
  ),
  (
    'futures_5_big_bull',
    true,
    '{"criteria": {}}',
    '5 Big Bull - 5-minute bullish spike'
  ),
  (
    'futures_5_big_bear',
    true,
    '{"criteria": {}}',
    '5 Big Bear - 5-minute bearish spike'
  ),
  (
    'futures_15_big_bull',
    true,
    '{"criteria": {}}',
    '15 Big Bull - 15-minute bullish spike'
  ),
  (
    'futures_15_big_bear',
    true,
    '{"criteria": {}}',
    '15 Big Bear - 15-minute bearish spike'
  ),
  (
    'futures_bottom_hunter',
    true,
    '{"criteria": {}}',
    'Bottom Hunter - Potential bottom reversal'
  ),
  (
    'futures_top_hunter',
    true,
    '{"criteria": {}}',
    'Top Hunter - Potential top reversal'
  );

-- Create user alert subscriptions table (for user-specific preferences)
CREATE TABLE IF NOT EXISTS user_alert_subscriptions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID,
    rule_type TEXT REFERENCES alert_rules(rule_type),
    symbol TEXT,  -- NULL means all symbols
    enabled BOOLEAN DEFAULT true,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_user_alert_subscriptions_user_id ON user_alert_subscriptions (user_id);
CREATE INDEX IF NOT EXISTS idx_user_alert_subscriptions_rule_type ON user_alert_subscriptions (rule_type);
CREATE INDEX IF NOT EXISTS idx_alert_rules_enabled ON alert_rules(enabled) WHERE enabled = true;

-- Verify
SELECT rule_type, enabled, description FROM alert_rules ORDER BY rule_type;

