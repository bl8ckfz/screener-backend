-- Insert futures alert rules (only if not already present)
-- This prevents duplicates
-- NOTE: Market cap filters removed - not used in backend evaluation

-- Delete existing rules to avoid duplicates
DELETE FROM alert_rules WHERE rule_type IN (
  'futures_big_bull_60',
  'futures_big_bear_60',
  'futures_pioneer_bull',
  'futures_pioneer_bear',
  'futures_5_big_bull',
  'futures_5_big_bear',
  'futures_15_big_bull',
  'futures_15_big_bear',
  'futures_bottom_hunter',
  'futures_top_hunter'
);

-- Insert all 10 alert rule types
-- Empty config {} means use hardcoded thresholds in alert engine (no market cap filters)
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
  ('futures_top_hunter', '{}', 'Top Hunter - Potential top reversal');

-- Verify inserts
SELECT rule_type, description FROM alert_rules ORDER BY rule_type;
