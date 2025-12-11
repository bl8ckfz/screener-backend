-- Initialize PostgreSQL for metadata storage

-- User Settings
CREATE TABLE IF NOT EXISTS user_settings (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id TEXT NOT NULL UNIQUE,
  selected_alerts TEXT[] DEFAULT '{}', -- Array of enabled alert types
  webhook_url TEXT, -- Discord/Telegram webhook
  notification_enabled BOOLEAN DEFAULT true,
  created_at TIMESTAMPTZ DEFAULT NOW(),
  updated_at TIMESTAMPTZ DEFAULT NOW()
);

-- Alert Rules (system-wide presets)
CREATE TABLE IF NOT EXISTS alert_rules (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  rule_type TEXT NOT NULL UNIQUE,
  enabled BOOLEAN DEFAULT true,
  config JSONB NOT NULL, -- Rule-specific thresholds
  description TEXT,
  created_at TIMESTAMPTZ DEFAULT NOW()
);

-- Insert default futures alert rules
INSERT INTO alert_rules (rule_type, config, description) VALUES
  (
    'futures_big_bull_60',
    '{
      "criteria": {
        "change_1h_min": 1.6,
        "change_1d_max": 15.0,
        "change_8h_gt_1h": true,
        "change_1d_gt_8h": true,
        "volume_1h_min": 500000,
        "volume_8h_min": 5000000,
        "volume_ratio_1h_8h": 6,
        "volume_ratio_1h_1d": 16,
        "market_cap_min": 23000000,
        "market_cap_max": 2500000000
      },
      "timeframes": ["1h", "8h", "1d"],
      "volumeIntervals": ["1h", "8h", "1d"]
    }',
    '60 Big Bull - Sustained momentum over multiple timeframes'
  ),
  (
    'futures_big_bear_60',
    '{
      "criteria": {
        "change_1h_max": -1.6,
        "change_1d_min": -15.0,
        "change_8h_lt_1h": true,
        "change_1d_lt_8h": true,
        "volume_1h_min": 500000,
        "volume_8h_min": 5000000,
        "volume_ratio_1h_8h": 6,
        "volume_ratio_1h_1d": 16,
        "market_cap_min": 23000000,
        "market_cap_max": 2500000000
      },
      "timeframes": ["1h", "8h", "1d"],
      "volumeIntervals": ["1h", "8h", "1d"]
    }',
    '60 Big Bear - Sustained downward momentum over multiple timeframes'
  ),
  (
    'futures_pioneer_bull',
    '{
      "criteria": {
        "change_5m_min": 1.0,
        "change_15m_min": 1.0,
        "change_acceleration": 3.0,
        "volume_ratio_5m_15m": 2.0,
        "market_cap_min": 23000000,
        "market_cap_max": 2500000000
      },
      "timeframes": ["5m", "15m"],
      "volumeIntervals": ["5m", "15m"]
    }',
    'Pioneer Bull - Early detection of emerging trends with accelerating momentum'
  ),
  (
    'futures_pioneer_bear',
    '{
      "criteria": {
        "change_5m_max": -1.0,
        "change_15m_max": -1.0,
        "change_acceleration": 3.0,
        "volume_ratio_5m_15m": 2.0,
        "market_cap_min": 23000000,
        "market_cap_max": 2500000000
      },
      "timeframes": ["5m", "15m"],
      "volumeIntervals": ["5m", "15m"]
    }',
    'Pioneer Bear - Early detection of emerging downtrends with accelerating momentum'
  ),
  (
    'futures_5_big_bull',
    '{
      "criteria": {
        "change_5m_min": 0.6,
        "change_1d_max": 15.0,
        "change_15m_gt_5m": true,
        "change_1h_gt_15m": true,
        "volume_5m_min": 100000,
        "volume_1h_min": 1000000,
        "volume_ratio_5m_15m": 3,
        "volume_ratio_5m_1h": 6,
        "volume_ratio_5m_8h": 66,
        "market_cap_min": 23000000,
        "market_cap_max": 2500000000
      },
      "timeframes": ["5m", "15m", "1h", "1d"],
      "volumeIntervals": ["5m", "15m", "1h", "8h"]
    }',
    '5 Big Bull - Explosive moves with progressive momentum acceleration'
  ),
  (
    'futures_5_big_bear',
    '{
      "criteria": {
        "change_5m_max": -0.6,
        "change_1d_min": -15.0,
        "change_15m_lt_5m": true,
        "change_1h_lt_15m": true,
        "volume_5m_min": 100000,
        "volume_1h_min": 1000000,
        "volume_ratio_5m_15m": 3,
        "volume_ratio_5m_1h": 6,
        "volume_ratio_5m_8h": 66,
        "market_cap_min": 23000000,
        "market_cap_max": 2500000000
      },
      "timeframes": ["5m", "15m", "1h", "1d"],
      "volumeIntervals": ["5m", "15m", "1h", "8h"]
    }',
    '5 Big Bear - Explosive downward moves with progressive momentum acceleration'
  ),
  (
    'futures_15_big_bull',
    '{
      "criteria": {
        "change_15m_min": 1.0,
        "change_1d_max": 15.0,
        "change_1h_gt_15m": true,
        "change_8h_gt_1h": true,
        "volume_15m_min": 400000,
        "volume_1h_min": 1000000,
        "volume_ratio_15m_1h": 3,
        "volume_ratio_15m_8h": 26,
        "market_cap_min": 23000000,
        "market_cap_max": 2500000000
      },
      "timeframes": ["15m", "1h", "8h", "1d"],
      "volumeIntervals": ["15m", "1h", "8h"]
    }',
    '15 Big Bull - Strong trending moves with progressive momentum acceleration'
  ),
  (
    'futures_15_big_bear',
    '{
      "criteria": {
        "change_15m_max": -1.0,
        "change_1d_min": -15.0,
        "change_1h_lt_15m": true,
        "change_8h_lt_1h": true,
        "volume_15m_min": 400000,
        "volume_1h_min": 1000000,
        "volume_ratio_15m_1h": 3,
        "volume_ratio_15m_8h": 26,
        "market_cap_min": 23000000,
        "market_cap_max": 2500000000
      },
      "timeframes": ["15m", "1h", "8h", "1d"],
      "volumeIntervals": ["15m", "1h", "8h"]
    }',
    '15 Big Bear - Strong downward trending moves with progressive momentum acceleration'
  ),
  (
    'futures_bottom_hunter',
    '{
      "criteria": {
        "change_1h_max": -0.7,
        "change_15m_max": -0.6,
        "change_5m_min": 0.5,
        "volume_ratio_5m_15m": 2,
        "volume_ratio_5m_1h": 8,
        "market_cap_min": 23000000,
        "market_cap_max": 2500000000
      },
      "timeframes": ["5m", "15m", "1h"],
      "volumeIntervals": ["5m", "15m", "1h"]
    }',
    'Bottom Hunter - Detect reversal from bottom with volume confirmation'
  ),
  (
    'futures_top_hunter',
    '{
      "criteria": {
        "change_1h_min": 0.7,
        "change_15m_min": 0.6,
        "change_5m_max": -0.5,
        "volume_ratio_5m_15m": 2,
        "volume_ratio_5m_1h": 8,
        "market_cap_min": 23000000,
        "market_cap_max": 2500000000
      },
      "timeframes": ["5m", "15m", "1h"],
      "volumeIntervals": ["5m", "15m", "1h"]
    }',
    'Top Hunter - Detect reversal from top with volume confirmation'
  )
ON CONFLICT (rule_type) DO NOTHING;

-- Create indexes
CREATE INDEX IF NOT EXISTS idx_user_settings_user_id ON user_settings(user_id);
CREATE INDEX IF NOT EXISTS idx_alert_rules_enabled ON alert_rules(enabled) WHERE enabled = true;

-- Grant permissions
GRANT ALL PRIVILEGES ON ALL TABLES IN SCHEMA public TO crypto_user;
