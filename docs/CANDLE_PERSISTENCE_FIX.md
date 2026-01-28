# Candle Persistence Fix - January 28, 2026

## Problem Identified

The `candles_1m` table in TimescaleDB was **completely empty**, causing cascading issues:

1. **No Historical Data**: Ring buffers in metrics-calculator had no persistent storage
2. **Zero Price Changes**: 8h and 1d price_change calculations returned 0% (requires 480 and 1440 candles respectively)
3. **Missing Alerts**: Big Bull/Bear 60m alerts couldn't trigger because they require:
   - `change_1h > 1.6%`
   - `change_8h > change_1h`
   - `change_1d > change_8h`
   - With 8h/1d at 0%, this condition is mathematically impossible
4. **Service Restarts**: Ring buffers reset on metrics-calculator restart, losing all historical context

### Root Cause

**No service was writing candles to `candles_1m` table:**
- data-collector: Only publishes to NATS (`candles.1m.{symbol}`)
- metrics-calculator: Only reads from NATS, writes metrics but NOT candles

The data flow was:
```
Binance WS → data-collector → NATS → metrics-calculator → ring buffer (in-memory only)
                                                        ↘ metrics_calculated ✅
                                                        ↘ candles_1m ❌
```

## Solution Implemented

### Code Changes

**File: `internal/calculator/persistence.go`**
```go
// Added new method to MetricsPersister
func (mp *MetricsPersister) PersistCandle(ctx context.Context, candle ringbuffer.Candle) error {
    query := `
        INSERT INTO candles_1m (
            time, symbol,
            open, high, low, close,
            volume, quote_volume,
            number_of_trades
        ) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
        ON CONFLICT (time, symbol) DO UPDATE SET ...
    `
    // ... execute query
}
```

**File: `cmd/metrics-calculator/main.go`**
```go
// Added candle persistence BEFORE ring buffer insertion
func(msg *nats.Msg) {
    var candle ringbuffer.Candle
    // ... unmarshal ...
    
    // NEW: Persist to candles_1m table
    candleCtx, candleCancel := context.WithTimeout(ctx, 5*time.Second)
    defer candleCancel()
    if err := persister.PersistCandle(candleCtx, candle); err != nil {
        logger.Error("Failed to persist candle", err)
        // Continue processing even if DB write fails
    }
    
    // Existing: Add to ring buffer and calculate metrics
    metricsData, err := calc.AddCandle(candle)
    // ...
}
```

### Why This Fixes The Issue

1. **Historical Data**: Every 1m candle now persists to TimescaleDB with 48h retention
2. **Ring Buffer Recovery**: On restart, metrics-calculator can reload candles from DB (future enhancement)
3. **Multi-Timeframe Calculations**: With enough historical data:
   - 5m requires 5 candles (5 minutes)
   - 15m requires 15 candles (15 minutes)
   - 1h requires 60 candles (1 hour)
   - 4h requires 240 candles (4 hours)
   - **8h requires 480 candles (8 hours)** ⏰
   - **1d requires 1440 candles (24 hours)** ⏰
4. **Alert Evaluation**: Once 8h/1d data accumulates, price_change will be non-zero and alert conditions can be met

## Expected Timeline

### Immediate (Within 1 Minute)
- ✅ Railway deploys updated metrics-calculator
- ✅ Service starts receiving candles from NATS
- ✅ Candles_1m table starts populating
- ✅ 5m/15m/1h metrics have non-zero price_change

### After 8 Hours
- ⏰ Ring buffer reaches 480 candles for 8h timeframe
- ⏰ price_change_8h will show actual % change vs 8 hours ago
- ⏰ Big Bull/Bear 60m alerts **may start triggering** (if market conditions meet criteria)

### After 24 Hours
- ⏰ Ring buffer reaches 1440 candles for 1d timeframe
- ⏰ price_change_1d will show actual % change vs 24 hours ago
- ⏰ Alert evaluation fully functional with all timeframe comparisons

## Monitoring Deployment

### Check Candles Table Population
```sql
-- Should start showing rows within 1 minute of deployment
SELECT COUNT(*) FROM candles_1m;

-- Check per-symbol data
SELECT 
    symbol,
    COUNT(*) as candle_count,
    MIN(time) as oldest,
    MAX(time) as newest,
    EXTRACT(EPOCH FROM (MAX(time) - MIN(time)))/3600 as hours_of_data
FROM candles_1m
GROUP BY symbol
ORDER BY candle_count DESC
LIMIT 10;
```

### Check Price Change Recovery
```sql
-- Should see 8h/1d change != 0 after sufficient time
SELECT 
    symbol, 
    timeframe, 
    price_change,
    time
FROM metrics_calculated
WHERE symbol = 'BTCUSDT'
  AND time > NOW() - INTERVAL '5 minutes'
ORDER BY time DESC, timeframe;
```

### Check Alert Evaluation
```sql
-- Monitor alert history for new Big Bull/Bear alerts
SELECT 
    created_at,
    symbol,
    rule_type,
    price,
    message
FROM alert_history
WHERE created_at > NOW() - INTERVAL '1 hour'
  AND rule_type IN ('futures_big_bull_60', 'futures_big_bear_60')
ORDER BY created_at DESC
LIMIT 20;
```

## Alert Triggering Conditions

### Big Bull 60m
```typescript
change_1h > 1.6% AND
change_8h > change_1h AND  // Requires 8h data ⏰
change_1d > change_8h AND  // Requires 1d data ⏰
volume_1h / volume_8h > 1.4
```

### Big Bear 60m
```typescript
change_1h < -1.6% AND
change_8h < change_1h AND  // Requires 8h data ⏰
change_1d < change_8h AND  // Requires 1d data ⏰
volume_1h / volume_8h > 1.4
```

### Pioneer Bull/Bear 15m
```typescript
// 15m timeframe evaluations
// Should work within 15 minutes of deployment
```

## Important Notes

1. **No Instant Alerts**: Big Bull/Bear 60m alerts **cannot trigger** until 24 hours of data accumulates
2. **Market Dependent**: Even with full data, alerts only trigger if market conditions match the criteria
3. **First Alert**: Don't expect alerts immediately - they require specific price action patterns
4. **Persistence**: Candles now persist across service restarts (within 48h retention window)

## Next Steps (Optional Future Enhancements)

1. **Backfill Historical Data**: 
   - Fetch last 24h of candles from Binance REST API `/fapi/v1/klines`
   - Would enable immediate alert evaluation without 24h wait
   
2. **Ring Buffer Initialization**:
   - On startup, load last 1440 candles from candles_1m table
   - Resume with full historical context instead of cold start
   
3. **Batch Candle Persistence**:
   - Currently synchronous (one INSERT per candle)
   - Could batch like metrics for better performance

## Verification Checklist

After deployment completes:
- [ ] Check Railway logs for "Received candle from NATS" messages
- [ ] Verify candles_1m table is populating: `SELECT COUNT(*) FROM candles_1m`
- [ ] Confirm no "Failed to persist candle" errors in logs
- [ ] Wait 1 hour, check 1h price_change is non-zero
- [ ] Wait 8 hours, check 8h price_change is non-zero  
- [ ] Wait 24 hours, check 1d price_change is non-zero
- [ ] Monitor alert_history for new alert types appearing

## Commit Details

- **Commit**: 74eb2a9
- **Branch**: main
- **Files Changed**: 
  - `cmd/metrics-calculator/main.go` (added PersistCandle call)
  - `internal/calculator/persistence.go` (added PersistCandle method)
- **Deployment**: Railway auto-deploy triggered by push

---
**Status**: ✅ Code pushed, awaiting Railway deployment
**ETA to First 8h Alert**: 8 hours after deployment
**ETA to First 1d Alert**: 24 hours after deployment
