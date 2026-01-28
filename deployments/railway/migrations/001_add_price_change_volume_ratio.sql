-- Add price_change and volume_ratio columns to metrics_calculated table
-- These are critical for alert evaluation

-- Add price_change column (rename from price_change_percent for consistency)
ALTER TABLE metrics_calculated 
ADD COLUMN IF NOT EXISTS price_change DOUBLE PRECISION;

-- Copy existing data if price_change_percent exists
UPDATE metrics_calculated 
SET price_change = price_change_percent 
WHERE price_change IS NULL AND price_change_percent IS NOT NULL;

-- Add volume_ratio column (volume vs previous period)
ALTER TABLE metrics_calculated 
ADD COLUMN IF NOT EXISTS volume_ratio DOUBLE PRECISION;

-- Update column names in schema to match code expectations
-- The API code expects these exact column names:
-- - rsi_14 (exists)
-- - bb_upper, bb_middle, bb_lower (for Bollinger Bands)
-- - fib_r3, fib_r2, fib_r1, fib_pivot, fib_s1, fib_s2, fib_s3 (for Fibonacci)

-- Add Bollinger Bands columns if they don't exist
ALTER TABLE metrics_calculated 
ADD COLUMN IF NOT EXISTS bb_upper DOUBLE PRECISION,
ADD COLUMN IF NOT EXISTS bb_middle DOUBLE PRECISION,
ADD COLUMN IF NOT EXISTS bb_lower DOUBLE PRECISION;

-- Rename Fibonacci columns to match API expectations
ALTER TABLE metrics_calculated 
ADD COLUMN IF NOT EXISTS fib_r3 DOUBLE PRECISION,
ADD COLUMN IF NOT EXISTS fib_r2 DOUBLE PRECISION,
ADD COLUMN IF NOT EXISTS fib_r1 DOUBLE PRECISION,
ADD COLUMN IF NOT EXISTS fib_pivot DOUBLE PRECISION,
ADD COLUMN IF NOT EXISTS fib_s1 DOUBLE PRECISION,
ADD COLUMN IF NOT EXISTS fib_s2 DOUBLE PRECISION,
ADD COLUMN IF NOT EXISTS fib_s3 DOUBLE PRECISION;

-- Copy data from old columns to new if they exist
UPDATE metrics_calculated SET fib_r3 = resistance1 WHERE fib_r3 IS NULL AND resistance1 IS NOT NULL;
UPDATE metrics_calculated SET fib_r2 = resistance0618 WHERE fib_r2 IS NULL AND resistance0618 IS NOT NULL;
UPDATE metrics_calculated SET fib_r1 = resistance0382 WHERE fib_r1 IS NULL AND resistance0382 IS NOT NULL;
UPDATE metrics_calculated SET fib_pivot = pivot WHERE fib_pivot IS NULL AND pivot IS NOT NULL;
UPDATE metrics_calculated SET fib_s1 = support0382 WHERE fib_s1 IS NULL AND support0382 IS NOT NULL;
UPDATE metrics_calculated SET fib_s2 = support0618 WHERE fib_s2 IS NULL AND support0618 IS NOT NULL;
UPDATE metrics_calculated SET fib_s3 = support1 WHERE fib_s3 IS NULL AND support1 IS NOT NULL;

-- Rename RSI column if needed
ALTER TABLE metrics_calculated 
ADD COLUMN IF NOT EXISTS rsi_14 DOUBLE PRECISION;

UPDATE metrics_calculated SET rsi_14 = rsi WHERE rsi_14 IS NULL AND rsi IS NOT NULL;
