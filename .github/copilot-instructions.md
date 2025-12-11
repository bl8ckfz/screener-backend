# Crypto Screener Backend - AI Agent Instructions

## Project Overview

Real-time cryptocurrency market data collection and alert processing system for 200+ Binance Futures pairs. Go microservices architecture designed for Kubernetes deployment with <100ms alert evaluation latency.

**Status**: Planning phase → implementation starting Week 1 (see `docs/ROADMAP.md`)

## Architecture: 4 Microservices + Message-Driven

```
Data Collector → NATS → Metrics Calculator → NATS → Alert Engine → NATS → API Gateway
     ↓                        ↓                       ↓                    ↓
  (WebSocket)          (TimescaleDB)           (TimescaleDB)          (WebSocket)
```

### Service Boundaries

1. **data-collector**: WebSocket client for Binance Futures API → publishes to `candles.1m.{symbol}`
2. **metrics-calculator**: Ring buffer (1440 candles/symbol) → calculates VCP/RSI/Fibonacci → publishes to `metrics.calculated`
3. **alert-engine**: Evaluates 10 rule types (Big Bull/Bear, Pioneer, Whale, Volume, Flat) → publishes to `alerts.triggered`
4. **api-gateway**: REST API + WebSocket hub for frontend clients

## Critical Implementation Patterns

### Ring Buffer for O(1) Sliding Windows
```go
// 1440 candles per symbol (24h retention) with fixed-size circular array
type CandleBuffer struct {
    candles [1440]Candle  // Fixed size, no allocations
    head    int           // Write position
    mu      sync.RWMutex  // Thread-safe
}
```

Aggregation timeframes: 5m/15m/1h/8h/1d calculated on-demand with O(1) lookups.

### NATS Topic Structure
- `candles.1m.BTCUSDT` - Raw kline data (binary or JSON)
- `metrics.calculated` - Enriched data with all indicators
- `alerts.triggered` - Alert events for broadcast

**Important**: Use JetStream for persistence (1-hour retention), not core NATS.

### Alert Deduplication
5-minute cooldown per `{symbol}:{rule_type}` using Redis with TTL keys:
```go
key := fmt.Sprintf("alert:%s:%s", symbol, ruleType)
exists := redis.SetNX(key, "1", 5*time.Minute)
if !exists { return } // Skip duplicate
```

### Database Schema Conventions
- **TimescaleDB**: Hypertables for `candles_1m`, `metrics_calculated`, `alert_history` with 48h retention
- **PostgreSQL** (Supabase): `user_settings`, `alert_rules` for metadata
- All timestamps: `TIMESTAMPTZ` (UTC)
- Primary keys: `(time, symbol)` for time-series, `UUID` for metadata
- Price storage: `DOUBLE PRECISION` for maximum accuracy (handles 0.000513 to 100000+ range)

## Tech Stack Decisions

| Component | Technology | Rationale |
|-----------|-----------|-----------|
| Language | Go 1.22+ | 32MB for 288k candles, <2s startup |
| HTTP | `gin-gonic/gin` | Fastest router, middleware-friendly |
| WebSocket | `gorilla/websocket` | De facto standard |
| Database | `jackc/pgx` | Native PostgreSQL driver |
| Messaging | `nats.go` | Lightweight pub/sub |
| Logging | `zerolog` | Zero-allocation JSON logs |

## Project Structure (Future)

```
cmd/{service-name}/main.go         # Service entrypoints
internal/binance/                  # Binance API client
internal/ringbuffer/               # Sliding window implementation
internal/indicators/               # VCP, Fibonacci, RSI, MACD
internal/alerts/                   # Rule engine + webhook client
pkg/database/                      # DB connection pooling
deployments/k8s/                   # Kubernetes manifests
deployments/terraform/             # Infrastructure as code
```

## Development Workflow (When Implemented)

### Build & Test
```bash
make build              # Compile all services
make test               # Unit tests
make test-integration   # Testcontainers (NATS + TimescaleDB)
make docker-build       # Multi-stage Dockerfile
```

### Local Development
```bash
docker-compose up       # NATS + TimescaleDB + PostgreSQL
air                     # Hot reload for Go services
```

### Kubernetes Deployment
```bash
terraform apply -chdir=deployments/terraform
helm upgrade --install crypto-screener deployments/helm/
kubectl port-forward svc/api-gateway 8080:80
```

## Alert Logic from Frontend (Port to Go)

**TypeScript Reference**: `../screener/src/` (sibling repository)

### VCP (Volatility Contraction Pattern)
```typescript
// Source: ../screener/src/utils/indicators.ts
VCP = (P / WA) * [((C - L) - (H - C)) / (H - L)]

// Actual implementation:
const priceToWA = lastPrice / weightedAvgPrice
const numerator = lastPrice - lowPrice - (highPrice - lastPrice)
const denominator = highPrice - lowPrice
const vcp = priceToWA * (numerator / denominator)
// Returns: rounded to 3 decimals
```

### Fibonacci Pivot Levels
```typescript
// Source: ../screener/src/utils/indicators.ts
const pivot = (highPrice + lowPrice + lastPrice) / 3
const range = highPrice - lowPrice

return {
  pivot,
  resistance1: pivot + 1.0 * range,
  resistance0618: pivot + 0.618 * range,
  resistance0382: pivot + 0.382 * range,
  support0382: pivot - 0.382 * range,
  support0618: pivot - 0.618 * range,
  support1: pivot - 1.0 * range
}
// All values rounded to 3 decimals
```

### Alert Rules Implementation
**Source**: `../screener/src/services/alertEngine.ts`
- Big Bull/Bear 60m: Evaluates futures metrics on 1h timeframe
- Pioneer Bull/Bear: Early momentum detection on 15m
- Whale alerts: Volume spike detection (3x threshold)
- Uses `coin.futuresMetrics` for all evaluations

**Critical**: 
- Ensure Go calculations match TypeScript exactly (use same precision)
- **Rounding**: TypeScript uses `Math.round(value * 1000) / 1000` for 3 decimals - adapt precision based on price range (e.g., 0.000513 needs more precision)
- Division by zero check: `if highPrice === lowPrice return 0`
- Memoization in TS (60s cache) - consider similar caching in Go
- Price formatting: Use appropriate decimal places based on price magnitude (low-cap coins like SHIB need 6-8 decimals)

## Performance Targets

- **Candle processing**: <10ms per candle
- **Alert evaluation**: <1ms per symbol
- **REST API p95**: <50ms
- **WebSocket broadcast**: <100ms to 100 clients
- **Memory**: 160KB per symbol (1440 candles × ~111 bytes)

## Integration Points

### Binance Futures WebSocket
- Endpoint: `wss://fstream.binance.com/ws/<symbol>@kline_1m`
- Response: JSON kline events (OHLCV + trades)
- Handle: Reconnection (exponential backoff), rate limits (none for WebSocket)

### Supabase Authentication
- JWT validation middleware in API Gateway
- Extract `user_id` from token for user-specific queries
- Library: `supabase-community/supabase-go`

### Discord/Telegram Webhooks
```json
POST https://discord.com/api/webhooks/{id}/{token}
{
  "embeds": [{
    "title": "🚨 BTCUSDT - Big Bull 60m",
    "fields": [{"name": "Price", "value": "$42,350"}]
  }]
}
```

## Common Gotchas

1. **Goroutine leaks**: Always use `context.Context` for cancellation, never `select {}` without timeout
2. **Time zones**: Store all times in UTC, convert in frontend
3. **Float precision**: Use `float64` for prices, but adapt display formatting based on price magnitude (not fixed `%.2f`)
4. **NATS reconnection**: Set `MaxReconnects(-1)` for infinite retries
5. **Kubernetes probes**: Liveness checks DB connectivity, readiness waits for dependencies

## Migration Notes

Frontend currently calls Binance API directly. Backend will replace this once Phase 8 complete. Feature flag: `VITE_USE_BACKEND_API=true` for gradual rollout.

## Related Resources

- **Frontend Repo**: `../screener` - React/TypeScript UI with existing alert logic
  - Indicators: `../screener/src/utils/indicators.ts` - VCP, Fibonacci calculations
  - Alert Engine: `../screener/src/services/alertEngine.ts` - Rule evaluation
  - Tests: `../screener/tests/utils/indicators.test.ts` - Validation suite
- **Roadmap**: `docs/ROADMAP.md` - Complete 14-week implementation plan (1661 lines)
- **Binance API**: [Futures WebSocket Docs](https://binance-docs.github.io/apidocs/futures/en/)
