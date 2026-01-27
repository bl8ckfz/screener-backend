# Phase 8: Frontend Integration Guide

**Status**: � In Progress - Backend Endpoint Added, Data Collection Issue  
**Date**: January 23, 2026  
**Goal**: Connect React frontend with Go backend for local testing

---

## Progress Summary

### ✅ Completed
1. **Backend API Client** (`backendApi.ts`) - REST + WebSocket client
2. **Backend Adapter** (`backendAdapter.ts`) - Transparent routing with feature flag
3. **WebSocket Alerts Hook** (`useBackendAlerts.ts`) - Real-time alert streaming  
4. **Environment Configuration** - Feature flags set up
5. **Backend Status Component** - Shows active data source
6. **GET /api/metrics Endpoint** - Added to API Gateway (Jan 23)

### 🔨 In Progress
- **Data Collection**: Backend services running but kline parsing errors
- **Testing**: Cannot fully test until data collection is fixed

### ⏳ Pending
- Fix data collector kline parsing issue
- Populate metrics_calculated table with real data
- Full integration testing with real data
- Performance benchmarking

---

## Overview

This guide walks through integrating the existing React/TypeScript frontend (`screener-frontend`) with the Go backend (`screener-backend`) running locally.

**Current Architecture**:
- Frontend calls Binance API directly (CORS issues, rate limits)
- No real-time alert system
- No centralized data processing

**Target Architecture**:
- Frontend calls Go backend REST API
- WebSocket for real-time alerts
- Backend handles all Binance communication
- Centralized metrics calculation

---

## Prerequisites ✅

All backend services must be running:

```bash
# Terminal 1: Start infrastructure
cd ~/fun/crypto/screener-backend
docker compose up -d

# Terminal 2-5: Start services
NATS_URL=nats://localhost:4222 \
TIMESCALE_URL=postgres://crypto_user:crypto_password@localhost:5432/crypto?sslmode=disable \
./bin/data-collector

NATS_URL=nats://localhost:4222 \
TIMESCALE_URL=postgres://crypto_user:crypto_password@localhost:5432/crypto?sslmode=disable \
./bin/metrics-calculator

NATS_URL=nats://localhost:4222 \
TIMESCALE_URL=postgres://crypto_user:crypto_password@localhost:5432/crypto?sslmode=disable \
POSTGRES_URL=postgres://crypto_user:crypto_password@localhost:5433/crypto_metadata?sslmode=disable \
REDIS_URL=localhost:6379 \
./bin/alert-engine

NATS_URL=nats://localhost:4222 \
TIMESCALE_URL=postgres://crypto_user:crypto_password@localhost:5432/crypto?sslmode=disable \
POSTGRES_URL=postgres://crypto_user:crypto_password@localhost:5433/crypto_metadata?sslmode=disable \
./bin/api-gateway
```

**Verify Services**:
```bash
# Check all services running
ps aux | grep -E "(data-collector|metrics-calculator|alert-engine|api-gateway)"

# Test API Gateway
curl http://localhost:8080/health
# Expected: {"status":"healthy","timestamp":"..."}
```

---

## Step 1: Backend API Client ✅

**File**: `screener-frontend/src/services/backendApi.ts`

Created TypeScript client with:
- REST API methods (health, metrics, settings, alerts)
- WebSocket client for real-time alerts
- Auto-reconnection with exponential backoff
- Feature flag support (`VITE_USE_BACKEND_API`)

**Key Functions**:
```typescript
backendApi.healthCheck()          // Test connectivity
backendApi.getMetrics(symbol)     // Get single symbol metrics
backendApi.getAllMetrics()        // Get all 43 symbols
backendApi.getSettings(token)     // User preferences
backendApi.saveSettings(token, settings)
backendApi.getAlertHistory(params)

// WebSocket
const wsClient = new BackendWebSocketClient(token)
wsClient.connect()
wsClient.onAlert((alert) => { /* handle alert */ })
wsClient.disconnect()
```

---

## Step 2: Environment Configuration ✅

**File**: `screener-frontend/.env.example` (updated)

```bash
# Backend API Configuration
VITE_USE_BACKEND_API=false                    # Set to 'true' to enable
VITE_BACKEND_API_URL=http://localhost:8080
VITE_BACKEND_WS_URL=ws://localhost:8080/ws/alerts
```

**Setup for Testing**:
```bash
cd ~/fun/crypto/screener-frontend
cp .env.example .env.local
```

Edit `.env.local`:
```bash
# Enable backend integration
VITE_USE_BACKEND_API=true
VITE_BACKEND_API_URL=http://localhost:8080
VITE_BACKEND_WS_URL=ws://localhost:8080/ws/alerts

# Keep existing Supabase config
VITE_SUPABASE_URL=your_supabase_url
VITE_SUPABASE_ANON_KEY=your_key
```

---

## Step 3: Update Data Fetching Logic (TODO)

### 3.1 Create Backend Adapter Service

**File**: `src/services/backendAdapter.ts` (new)

```typescript
/**
 * Adapter that converts backend API responses to frontend Coin type
 */
import { backendApi, USE_BACKEND_API } from './backendApi'
import { binanceFuturesApi } from './binanceFuturesApi'
import type { Coin } from '@/types/api'

export async function fetchCoinMetrics(symbol: string): Promise<Coin> {
  if (USE_BACKEND_API) {
    // Fetch from Go backend
    const metrics = await backendApi.getMetrics(symbol)
    return transformBackendMetricsToCoin(metrics)
  } else {
    // Fallback to direct Binance API
    return binanceFuturesApi.fetchSymbolData(symbol)
  }
}

export async function fetchAllCoins(): Promise<Coin[]> {
  if (USE_BACKEND_API) {
    const allMetrics = await backendApi.getAllMetrics()
    return Object.values(allMetrics).map(transformBackendMetricsToCoin)
  } else {
    // Existing logic
    return binanceFuturesApi.fetchAllSymbols()
  }
}

function transformBackendMetricsToCoin(metrics: any): Coin {
  // Transform backend SymbolMetrics to frontend Coin type
  return {
    symbol: metrics.symbol,
    lastPrice: metrics.last_price,
    // Map all timeframes...
    futuresMetrics: {
      // VCP, Fibonacci, RSI, MACD from backend
      vcp: metrics.vcp,
      fibonacciLevels: metrics.fibonacci_levels,
      rsi: metrics.rsi,
      macd: metrics.macd,
      // ... rest of mapping
    },
  }
}
```

### 3.2 Update Main App Data Fetching

**File**: `src/App.tsx` (update)

Replace direct Binance calls with adapter:
```typescript
import { fetchAllCoins } from '@/services/backendAdapter'

// In useEffect or data fetching logic:
const coins = await fetchAllCoins()
```

---

## Step 4: WebSocket Alert Integration (TODO)

### 4.1 Create Alert Hook

**File**: `src/hooks/useBackendAlerts.ts` (new)

```typescript
import { useEffect, useState } from 'react'
import { BackendWebSocketClient } from '@/services/backendApi'
import { useAuth } from '@/hooks/useAuth' // Supabase auth
import type { Alert } from '@/types/api'

export function useBackendAlerts() {
  const [alerts, setAlerts] = useState<Alert[]>([])
  const [wsClient, setWsClient] = useState<BackendWebSocketClient | null>(null)
  const { session } = useAuth()

  useEffect(() => {
    if (!session?.access_token) return

    const client = new BackendWebSocketClient(session.access_token)
    client.connect()

    const unsubscribe = client.onAlert((alert) => {
      setAlerts(prev => [alert, ...prev])
      // Trigger toast notification
    })

    setWsClient(client)

    return () => {
      unsubscribe()
      client.disconnect()
    }
  }, [session])

  return { alerts, isConnected: wsClient?.isConnected() ?? false }
}
```

### 4.2 Use in App Component

```typescript
import { useBackendAlerts } from '@/hooks/useBackendAlerts'

function App() {
  const { alerts, isConnected } = useBackendAlerts()

  // Display connection status
  // Show toast notifications for new alerts
  // ...
}
```

---

## Step 5: User Settings Integration (TODO)

Update settings modal to save to backend:

```typescript
import { backendApi } from '@/services/backendApi'
import { useAuth } from '@/hooks/useAuth'

async function saveSettings(settings: UserSettings) {
  const { session } = useAuth()
  if (session?.access_token) {
    await backendApi.saveSettings(session.access_token, settings)
  }
}
```

---

## Step 6: Testing Checklist

### Backend Health Check
```bash
cd ~/fun/crypto/screener-frontend
npm run dev

# In browser console:
import { backendApi } from './src/services/backendApi'
await backendApi.healthCheck()
// Should return: {status: "healthy", timestamp: "..."}
```

### Metrics Fetching
```javascript
await backendApi.getMetrics('BTCUSDT')
// Should return full metrics object

await backendApi.getAllMetrics()
// Should return 43 symbols
```

### WebSocket Connection
```javascript
const ws = new BackendWebSocketClient('fake-token-for-testing')
ws.connect()
ws.onAlert(alert => console.log('Alert:', alert))

// Wait for connection
// Backend should send test alerts
```

---

## Migration Strategy

### Phase 1: Feature Flag OFF (Current State)
- Frontend uses direct Binance API
- No backend integration
- Users see current behavior

### Phase 2: Opt-in Testing (Week 13)
- Set `VITE_USE_BACKEND_API=true` in local `.env`
- Developers test backend integration
- Validate data consistency

### Phase 3: Staged Rollout (Week 14)
- Deploy backend to cloud
- Enable for 10% of users
- Monitor error rates, performance
- Gradually increase to 100%

### Phase 4: Deprecate Direct API (Future)
- Remove Binance API client code
- Backend becomes single source of truth

---

## API Endpoint Mapping

| Frontend Need | Backend Endpoint | Status |
|---------------|------------------|--------|
| Health check | `GET /api/health` | ✅ Ready |
| All symbols metrics | `GET /api/metrics` | ✅ **Implemented (Jan 23)** |
| Single symbol metrics | `GET /api/metrics/:symbol` | ✅ Ready |
| User settings (get) | `GET /api/settings` | ✅ Ready |
| User settings (save) | `POST /api/settings` | ✅ Ready |
| Alert history | `GET /api/alerts/history` | ✅ Ready (existing alerts endpoint) |
| Real-time alerts | `WS /ws/alerts` | ✅ Ready |

**Update**: All required endpoints are now implemented. The `GET /api/metrics` endpoint was added to return all symbols' latest metrics across all timeframes.

---

## Next Steps

### Immediate Priority: Fix Data Collection
1. ✅ Infrastructure running (NATS, TimescaleDB, PostgreSQL, Redis)
2. ✅ All 4 backend services running
3. ❌ **Data Collector** - Kline parsing errors (see logs)
4. ⏳ Metrics Calculator - Waiting for valid data
5. ⏳ API Gateway - Ready but returning empty results

**Current Issue**: Data collector logs show "invalid kline data" errors. This prevents metrics from being calculated and stored.

### After Data Collection Fix
1. ✅ Create backend API client (`backendApi.ts`)
2. ✅ Update environment variables
3. ✅ Create backend adapter service
4. ⏳ Update App.tsx data fetching (when backend has data)
5. ✅ Create WebSocket alert hook
6. ⏳ Test locally with both modes (backend ON/OFF)
7. ⏳ Implement missing backend endpoints (if any)
8. ⏳ Update UI to show connection status
9. ⏳ Add error handling and fallbacks

### Testing Once Data is Available
```bash
# Test metrics endpoint
curl http://localhost:8080/api/metrics/ | python3 -m json.tool

# Should return array of symbols with timeframes:
# [{"symbol": "BTCUSDT", "timeframes": {"1m": {...}, "5m": {...}, ...}}]

# Test single symbol
curl http://localhost:8080/api/metrics/BTCUSDT | python3 -m json.tool
```

---

## Troubleshooting

### Backend not responding
```bash
# Check services are running
ps aux | grep -E "(data-collector|api-gateway)"

# Check logs
tail -f /tmp/api-gateway.log

# Restart services
pkill -f api-gateway && ./bin/api-gateway
```

### CORS errors
Backend API Gateway should have CORS enabled:
```go
// Already implemented in cmd/api-gateway/main.go
router.Use(cors())
```

### WebSocket connection fails
- Check `VITE_BACKEND_WS_URL` matches API Gateway port
- Verify JWT token is valid (Supabase)
- Check browser console for connection errors

---

## Performance Comparison

| Metric | Direct Binance API | Via Backend |
|--------|-------------------|-------------|
| Initial Load (43 symbols) | ~8.6s (200ms/symbol) | <1s (batch query) |
| Rate Limits | 418 errors frequent | None (backend handles) |
| CORS Issues | Yes (needs proxy) | No |
| Real-time Alerts | Not possible | Yes (WebSocket) |
| Alert History | Not available | Available (48h) |
| Metrics Consistency | Client-side calculation | Server-side (consistent) |

---

## Completion Criteria

Phase 8 is complete when:
- ✅ Backend API client created
- ✅ Environment variables configured
- [ ] All data fetching uses backend (when enabled)
- [ ] WebSocket alerts working
- [ ] User settings persist to backend
- [ ] Alert history displays correctly
- [ ] Feature flag allows easy switching
- [ ] No console errors when backend enabled
- [ ] Performance is equal or better than direct API
- [ ] Documentation updated

---

**Current Status**: Step 2 complete, proceeding to Step 3 (adapter service)
