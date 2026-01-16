# Backend Service Architecture Roadmap
## Crypto Screener - Data Collection & Alert Engine

**Created**: December 9, 2025  
**Project**: Separate data collection from presentation  
**Goal**: Scalable Go microservices running on Kubernetes for 200+ futures pairs with 1m candle sliding windows

**Progress Update (Jan 16, 2026)**
- Phase 1 (foundation) ✅
- Phase 2 (data-collector) ✅
- Phase 3 (metrics-calculator) ✅
- Phase 4 (alert-engine) ✅ — alert-first focus; per-candle dedup in place
- Next: Phase 5 (API gateway alerts REST/WS + Supabase auth), observability, deployment scaffolding

---

## Executive Summary

### Vision
Build a production-grade backend service that handles:
- **Data Collection**: Real-time WebSocket streams from Binance Futures (200+ USDT pairs)
- **Sliding Window Calculations**: 1-minute candles with O(1) metrics for 5m/15m/1h/4h/8h/1d timeframes
- **Alert Evaluation**: 10 futures alert types with complex multi-timeframe logic
- **Data Persistence**: 48-hour configurable retention for alerts and metrics
- **Real-time Push**: WebSocket connections to frontend clients + webhook notifications

### Architecture Decision: Go
**Rationale**:
- ✅ **Memory efficiency**: ~32MB for 288,000 candles (1440 × 200 symbols)
- ✅ **Concurrency**: Goroutines handle 200+ parallel WebSocket connections efficiently
- ✅ **Performance**: <100ms alert evaluation latency for real-time processing
- ✅ **Kubernetes-native**: Small container images (~15MB), fast startup (<2s)
- ✅ **Type safety**: Compile-time guarantees for critical financial data

### Key Metrics
- **Trading Pairs**: 200+ Binance Futures USDT pairs
- **Data Points**: 288,000 candles in memory (1440 × 200)
- **Update Frequency**: 1-minute candle closes
- **Alert Types**: 10 futures signals (Big Bull/Bear 60m, Pioneer, Whale, etc.)
- **Retention**: 48 hours (configurable)
- **Frontend Clients**: 100s concurrent WebSocket connections
- **Deployment**: Kubernetes (K3s initially, K8s production-ready)

---

## Architecture Overview

### System Components

```
┌─────────────────────────────────────────────────────────────────┐
│                         FRONTEND CLIENTS                         │
│                  (React/TypeScript - Existing)                   │
└────────────────┬──────────────────────┬─────────────────────────┘
                 │                      │
                 │ WebSocket            │ REST API
                 │ (Real-time)          │ (Historical)
                 ↓                      ↓
┌─────────────────────────────────────────────────────────────────┐
│                         API GATEWAY                              │
│  - WebSocket Hub (Socket.io / Gorilla WebSocket)                │
│  - REST API (Gin / Echo framework)                              │
│  - Rate Limiting & Authentication                                │
└────────────────┬──────────────────────┬─────────────────────────┘
                 │                      │
                 │                      │ Query
                 │                      ↓
                 │              ┌──────────────────┐
                 │              │   PostgreSQL     │
                 │              │  (Supabase)      │
                 │              │ - User settings  │
                 │              │ - Alert rules    │
                 │              │ - Auth data      │
                 │              └──────────────────┘
                 │
                 ↓
┌─────────────────────────────────────────────────────────────────┐
│                       MESSAGE QUEUE (NATS)                       │
│  Topics:                                                         │
│  - candles.1m.{symbol}     (raw candles)                        │
│  - metrics.calculated      (computed indicators)                │
│  - alerts.triggered        (alert events)                       │
└────┬─────────────────────┬──────────────────────┬───────────────┘
     │                     │                      │
     ↓                     ↓                      ↓
┌──────────────┐  ┌──────────────────┐  ┌──────────────────┐
│    DATA      │  │     METRICS      │  │     ALERT        │
│  COLLECTOR   │  │   CALCULATOR     │  │     ENGINE       │
│              │  │                  │  │                  │
│ - WebSocket  │  │ - Sliding Window │  │ - Rule Engine    │
│   to Binance │  │ - VCP/Fib/RSI    │  │ - Evaluation     │
│ - 200 pairs  │  │ - Ring Buffers   │  │ - Deduplication  │
│ - Health     │  │ - O(1) lookups   │  │ - Webhooks       │
│   checks     │  │                  │  │                  │
└──────────────┘  └─────────┬────────┘  └─────────┬────────┘
                            │                     │
                            ↓                     ↓
                    ┌──────────────────┐  ┌──────────────────┐
                    │   TimescaleDB    │  │   TimescaleDB    │
                    │ - 1m candles     │  │ - Alert history  │
                    │ - Metrics        │  │ - 48h retention  │
                    │ - 48h retention  │  │ - Hypertables    │
                    └──────────────────┘  └──────────────────┘
```

### Service Responsibilities

#### 1. **Data Collector Service** (`data-collector`)
**Purpose**: Establish and maintain WebSocket connections to Binance Futures API

**Responsibilities**:
- Connect to Binance Futures WebSocket streams (200+ symbols)
- Receive 1-minute kline (candle) updates
- Parse and validate incoming data
- Publish raw candles to NATS topic: `candles.1m.{symbol}`
- Health monitoring and auto-reconnection
- Graceful degradation (exponential backoff on errors)

**Scaling**: Horizontal - can split symbols across multiple instances

**Tech Stack**:
- `gorilla/websocket` for WebSocket connections
- `nats.go` for message publishing
- Connection pooling with 200 concurrent goroutines

#### 2. **Metrics Calculator Service** (`metrics-calculator`)
**Purpose**: Compute technical indicators and maintain sliding windows

**Responsibilities**:
- Subscribe to `candles.1m.{symbol}` from NATS
- Maintain ring buffers for 1440 candles per symbol (24 hours)
- Calculate on-the-fly:
  - 5m/15m/1h/4h/8h/1d aggregated candles (O(1) sliding window)
  - VCP (Volatility Contraction Pattern)
  - Fibonacci pivot levels
  - RSI, MACD, Bollinger Bands
  - Volume-weighted metrics
- Persist computed metrics to TimescaleDB
- Publish enriched data to `metrics.calculated` topic

**Scaling**: Vertical initially (single instance handles all symbols), horizontal by symbol sharding if needed

**Tech Stack**:
- Custom ring buffer implementation (circular arrays)
- `jackc/pgx` for PostgreSQL/TimescaleDB
- In-memory caching with `sync.Map`

#### 3. **Alert Engine Service** (`alert-engine`)
**Purpose**: Evaluate alert rules and trigger notifications

**Responsibilities**:
- Subscribe to `metrics.calculated` from NATS
- Load alert rules from PostgreSQL (system-wide presets)
- Evaluate 10 futures alert types:
  - Big Bull 60m / Big Bear 60m
  - Pioneer Bull / Pioneer Bear
  - Bull Whale / Bear Whale
  - Bull Volume / Bear Volume
  - Flat 1m / Flat 8h
- Deduplicate alerts (cooldown: 5 minutes per symbol+rule)
- Persist triggered alerts to TimescaleDB (48h retention)
- Publish to `alerts.triggered` topic
- Send webhook notifications (Discord/Telegram)

**Scaling**: Horizontal with consistent hashing (by symbol)

**Tech Stack**:
- Rule engine with Go structs/interfaces
- Webhook client with retry logic
- Alert deduplication with Redis cache

#### 4. **API Gateway Service** (`api-gateway`)
**Purpose**: Expose REST and WebSocket APIs to frontend

**Responsibilities**:
- **REST API**:
  - `GET /api/alerts` - Query alert history (48h)
  - `GET /api/metrics/{symbol}` - Fetch latest metrics
  - `GET /api/health` - Service health checks
  - `POST /api/settings` - Save user preferences
- **WebSocket API**:
  - Subscribe to real-time alerts: `ws://api/alerts/stream`
  - Broadcast alerts from `alerts.triggered` topic to connected clients
  - Handle client authentication (Supabase JWT)
  - Connection management (heartbeat, reconnection)
- Rate limiting (100 req/min per IP)
- CORS configuration for frontend domain

**Scaling**: Horizontal with sticky sessions (WebSocket affinity)

**Tech Stack**:
- `gin-gonic/gin` for REST API
- `gorilla/websocket` for WebSocket hub
- `supabase-go` for auth validation

---

## Database Schema

### PostgreSQL (Supabase) - Metadata & User Data

```sql
-- Users (managed by Supabase Auth)
-- auth.users table (built-in)

-- User Settings
CREATE TABLE user_settings (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id UUID REFERENCES auth.users(id) ON DELETE CASCADE,
  selected_alerts TEXT[] DEFAULT '{}', -- Array of enabled alert types
  webhook_url TEXT, -- Discord/Telegram webhook
  notification_enabled BOOLEAN DEFAULT true,
  created_at TIMESTAMPTZ DEFAULT NOW(),
  updated_at TIMESTAMPTZ DEFAULT NOW(),
  UNIQUE(user_id)
);

-- Alert Rules (system-wide presets)
CREATE TABLE alert_rules (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  rule_type TEXT NOT NULL, -- 'big_bull_60m', 'pioneer_bear', etc.
  enabled BOOLEAN DEFAULT true,
  config JSONB NOT NULL, -- Rule-specific thresholds
  description TEXT,
  created_at TIMESTAMPTZ DEFAULT NOW(),
  UNIQUE(rule_type)
);

-- Insert default futures alert rules
INSERT INTO alert_rules (rule_type, config, description) VALUES
  ('big_bull_60m', '{"volume_increase": 2.0, "price_change": 0.03}', 'Large bullish candle on 60m'),
  ('big_bear_60m', '{"volume_increase": 2.0, "price_change": -0.03}', 'Large bearish candle on 60m'),
  ('pioneer_bull', '{"timeframe": "15m", "volume_threshold": 1.5}', 'Early bullish momentum'),
  ('pioneer_bear', '{"timeframe": "15m", "volume_threshold": 1.5}', 'Early bearish momentum'),
  ('bull_whale', '{"volume_spike": 3.0, "price_support": 0.02}', 'Whale accumulation'),
  ('bear_whale', '{"volume_spike": 3.0, "price_dump": -0.02}', 'Whale distribution'),
  ('bull_volume', '{"volume_ratio": 2.5, "consecutive_candles": 3}', 'Sustained buying volume'),
  ('bear_volume', '{"volume_ratio": 2.5, "consecutive_candles": 3}', 'Sustained selling volume'),
  ('flat_1m', '{"volatility_threshold": 0.001, "duration_minutes": 10}', 'Low volatility consolidation'),
  ('flat_8h', '{"range_pct": 0.02, "duration_hours": 8}', 'Extended sideways range');
```

### TimescaleDB - Time-Series Data

```sql
-- 1-Minute Candles (compressed hypertable)
CREATE TABLE candles_1m (
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
SELECT create_hypertable('candles_1m', 'time');

-- Retention policy: 48 hours
SELECT add_retention_policy('candles_1m', INTERVAL '48 hours');

-- Compression policy (after 1 hour)
ALTER TABLE candles_1m SET (
  timescaledb.compress,
  timescaledb.compress_segmentby = 'symbol'
);
SELECT add_compression_policy('candles_1m', INTERVAL '1 hour');

-- Calculated Metrics (enriched data)
CREATE TABLE metrics_calculated (
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

SELECT create_hypertable('metrics_calculated', 'time');
SELECT add_retention_policy('metrics_calculated', INTERVAL '48 hours');

-- Alert History (triggered alerts)
CREATE TABLE alert_history (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  triggered_at TIMESTAMPTZ NOT NULL,
  symbol TEXT NOT NULL,
  alert_type TEXT NOT NULL,
  timeframe TEXT,
  
  -- Alert context
  price DOUBLE PRECISION,
  volume DOUBLE PRECISION,
  change_pct DOUBLE PRECISION,
  metadata JSONB, -- Additional context (VCP, volume ratio, etc.)
  
  -- Notification tracking
  webhook_sent BOOLEAN DEFAULT false,
  webhook_sent_at TIMESTAMPTZ,
  
  PRIMARY KEY (triggered_at, symbol, alert_type)
);

SELECT create_hypertable('alert_history', 'triggered_at');
SELECT add_retention_policy('alert_history', INTERVAL '48 hours');

-- Index for fast queries
CREATE INDEX idx_alert_history_symbol ON alert_history (symbol, triggered_at DESC);
CREATE INDEX idx_alert_history_type ON alert_history (alert_type, triggered_at DESC);
```

---

## API Contracts

### REST API Endpoints

#### 1. **GET /api/health**
Health check for monitoring

**Response**:
```json
{
  "status": "healthy",
  "services": {
    "data_collector": "up",
    "metrics_calculator": "up",
    "alert_engine": "up",
    "database": "up",
    "nats": "up"
  },
  "timestamp": "2025-12-09T10:30:00Z"
}
```

#### 2. **GET /api/alerts**
Query alert history (last 48 hours)

**Query Parameters**:
- `symbol` (optional) - Filter by symbol (e.g., `BTCUSDT`)
- `type` (optional) - Filter by alert type (e.g., `big_bull_60m`)
- `limit` (default: 100, max: 1000)
- `offset` (default: 0)

**Response**:
```json
{
  "alerts": [
    {
      "id": "123e4567-e89b-12d3-a456-426614174000",
      "triggered_at": "2025-12-09T10:28:00Z",
      "symbol": "BTCUSDT",
      "alert_type": "big_bull_60m",
      "timeframe": "1h",
      "price": 42350.50,
      "volume": 1250000,
      "change_pct": 3.2,
      "metadata": {
        "vcp": 0.85,
        "volume_increase": 2.3,
        "candle_size_pct": 3.2
      }
    }
  ],
  "total": 1,
  "limit": 100,
  "offset": 0
}
```

#### 3. **GET /api/metrics/{symbol}**
Fetch latest calculated metrics for a symbol

**Response**:
```json
{
  "symbol": "BTCUSDT",
  "timestamp": "2025-12-09T10:30:00Z",
  "timeframes": {
    "5m": {
      "open": 42300.00,
      "high": 42400.00,
      "low": 42250.00,
      "close": 42350.50,
      "volume": 125000,
      "vcp": 0.78,
      "rsi_14": 62.5,
      "macd": 15.3,
      "fib_pivot": 42325.00
    },
    "1h": { /* ... */ },
    "8h": { /* ... */ }
  }
}
```

#### 4. **POST /api/settings**
Save user preferences (requires authentication)

**Headers**:
```
Authorization: Bearer <supabase_jwt>
```

**Request Body**:
```json
{
  "selected_alerts": ["big_bull_60m", "pioneer_bull", "bull_whale"],
  "webhook_url": "https://discord.com/api/webhooks/...",
  "notification_enabled": true
}
```

**Response**:
```json
{
  "success": true,
  "settings": { /* saved settings */ }
}
```

### WebSocket API

#### Connection
```
ws://api-gateway.yourdomain.com/ws/alerts
```

**Authentication**: Send Supabase JWT in first message
```json
{
  "type": "auth",
  "token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..."
}
```

#### Subscribe to Alerts
```json
{
  "type": "subscribe",
  "channels": ["alerts"]
}
```

#### Real-time Alert Message
```json
{
  "type": "alert",
  "data": {
    "id": "123e4567-e89b-12d3-a456-426614174000",
    "triggered_at": "2025-12-09T10:30:15Z",
    "symbol": "ETHUSDT",
    "alert_type": "pioneer_bull",
    "timeframe": "15m",
    "price": 2250.75,
    "volume": 850000,
    "change_pct": 2.1,
    "metadata": {
      "volume_increase": 1.8,
      "momentum_score": 0.92
    }
  }
}
```

#### Heartbeat (every 30s)
```json
{
  "type": "ping"
}
```

**Client Response**:
```json
{
  "type": "pong"
}
```

### Webhook Notifications

**Discord/Telegram Format**:
```json
{
  "content": "🚨 **BTCUSDT Alert** 🚨",
  "embeds": [
    {
      "title": "Big Bull 60m Signal",
      "description": "Large bullish momentum detected",
      "color": 3066993,
      "fields": [
        {"name": "Price", "value": "$42,350.50", "inline": true},
        {"name": "Change", "value": "+3.2%", "inline": true},
        {"name": "Volume", "value": "1.25M", "inline": true},
        {"name": "VCP", "value": "0.85", "inline": true},
        {"name": "Timeframe", "value": "1h", "inline": true}
      ],
      "timestamp": "2025-12-09T10:28:00Z"
    }
  ]
}
```

---

## Implementation Roadmap

### Phase 1: Foundation & Infrastructure (Weeks 1-2)

#### Week 1: Project Setup
- [ ] **Initialize Go monorepo**
  - Directory structure: `cmd/`, `internal/`, `pkg/`, `deployments/`
  - Go modules setup with dependencies
  - Makefile for build/test/deploy commands
  - Docker multi-stage builds
  
- [ ] **Setup local development environment**
  - Docker Compose with NATS + TimescaleDB + PostgreSQL
  - Hot reload with `air` or `reflex`
  - VS Code debugging configuration
  
- [ ] **Infrastructure as Code**
  - Terraform modules for:
    - Kubernetes cluster (K3s/K8s)
    - Load balancer
    - DNS records
    - SSL certificates (cert-manager)
  - Helm chart scaffolding for 4 services
  
- [ ] **CI/CD Pipeline**
  - GitHub Actions workflows:
    - Build: compile Go binaries, run tests, lint
    - Docker: build and push images to registry
    - Deploy: Helm upgrade to K8s cluster
  - Environment-specific configs (dev/staging/prod)

#### Week 2: Database & Messaging
- [ ] **TimescaleDB Setup**
  - Deploy TimescaleDB instance (managed or self-hosted)
  - Create schemas (candles_1m, metrics_calculated, alert_history)
  - Configure hypertables + compression + retention policies
  - Migration tooling (golang-migrate)
  
- [ ] **NATS Cluster**
  - Deploy NATS with JetStream enabled
  - Configure streams for candles/metrics/alerts
  - Retention: 1 hour (messages deleted after processing)
  - Monitoring: NATS exporter for Prometheus
  
- [ ] **PostgreSQL/Supabase Integration**
  - Setup Supabase project or self-hosted PostgreSQL
  - Create user_settings and alert_rules tables
  - Seed default alert rules (10 futures types)
  - Auth integration library

**Deliverables**:
- ✅ Local dev environment working (Docker Compose)
- ✅ K8s cluster provisioned with Terraform
- ✅ Databases ready with schemas
- ✅ CI/CD pipeline building Docker images

---

### Phase 2: Data Collector Service (Weeks 3-4)

#### Week 3: WebSocket Connection Manager
- [ ] **Binance Futures API Client**
  - WebSocket connection pool (200 concurrent connections)
  - Subscribe to kline streams: `<symbol>@kline_1m`
  - Parse JSON responses into Go structs
  - Validate data (non-null prices, positive volumes)
  
- [ ] **Connection Health Management**
  - Heartbeat monitoring (detect stale connections)
  - Auto-reconnection with exponential backoff
  - Circuit breaker pattern (stop reconnecting after 10 failures)
  - Metrics: connection count, reconnection rate, message rate
  
- [ ] **Symbol Management**
  - Fetch active futures pairs from Binance API: `/fapi/v1/exchangeInfo`
  - Filter for USDT-margined contracts
  - Dynamic subscription (add/remove symbols at runtime)

#### Week 4: Data Publishing & Testing
- [ ] **NATS Publisher**
  - Publish raw candles to `candles.1m.{symbol}` topic
  - Message format: Protocol Buffers or JSON
  - At-least-once delivery guarantee
  - Error handling: retry failed publishes
  
- [ ] **Observability**
  - Structured logging (zerolog or zap)
  - Prometheus metrics:
    - `data_collector_messages_received_total`
    - `data_collector_messages_published_total`
    - `data_collector_websocket_connections`
    - `data_collector_errors_total`
  - Health check endpoint: `/health`
  
- [ ] **Testing**
  - Unit tests: WebSocket message parsing
  - Integration tests: NATS publishing (testcontainers)
  - Load test: Handle 200 symbols × 60 updates/hour = 12k msgs/hour

**Deliverables**:
- ✅ Data Collector service deployed to K8s
- ✅ Receiving real-time 1m candles from Binance
- ✅ Publishing to NATS successfully
- ✅ Metrics visible in Prometheus

---

### Phase 3: Metrics Calculator Service (Weeks 5-6)

#### Week 5: Ring Buffer Implementation
- [ ] **Sliding Window Data Structure**
  - Ring buffer for 1440 candles per symbol (24 hours)
  - O(1) insert, O(1) aggregation for 5m/15m/1h/8h/1d
  - Efficient memory layout (struct of arrays for cache locality)
  - Thread-safe with sync.RWMutex
  
- [ ] **NATS Subscriber**
  - Subscribe to `candles.1m.{symbol}` topic
  - Consume messages and update ring buffers
  - Handle out-of-order messages (timestamp-based insertion)
  - Backpressure handling (if processing is slow)
  
- [ ] **Aggregation Logic**
  - Calculate OHLCV for higher timeframes:
    - 5m: last 5 candles
    - 15m: last 15 candles
    - 1h: last 60 candles
    - 4h: last 240 candles
    - 8h: last 480 candles
    - 1d: last 1440 candles
  - Volume-weighted average price (VWAP)

#### Week 6: Technical Indicators
- [ ] **Port Existing Indicators from TypeScript**
  - VCP (Volatility Contraction Pattern): `(P/WA) * [((C-L)-(H-C))/(H-L)]`
  - Fibonacci Pivots: 7 levels (R3, R2, R1, Pivot, S1, S2, S3)
  - RSI (14-period Relative Strength Index)
  - MACD (12, 26, 9 parameters)
  - Bollinger Bands (20-period, 2 std dev)
  
- [ ] **TimescaleDB Persistence**
  - Batch insert metrics every minute
  - Use prepared statements for performance
  - Upsert logic (ON CONFLICT UPDATE)
  - Connection pooling (pgxpool)
  
- [ ] **NATS Publishing**
  - Publish enriched data to `metrics.calculated` topic
  - Include all timeframes in single message
  - Message size optimization (compress if >1KB)

**Deliverables**:
- ✅ Metrics Calculator service deployed
- ✅ Sliding windows calculating correctly
- ✅ Technical indicators matching frontend logic
- ✅ Data persisted to TimescaleDB

---

### Phase 4: Alert Engine Service (Weeks 7-8)

#### Week 7: Rule Engine
- [ ] **Alert Rule Definitions**
  - Load rules from PostgreSQL `alert_rules` table
  - Hot-reload on rule changes (watch for updates)
  - Rule evaluation interface:
    ```go
    type AlertRule interface {
      Evaluate(metrics Metrics) (triggered bool, metadata map[string]interface{})
    }
    ```
  
- [ ] **Implement 10 Futures Alert Types**
  - **Big Bull 60m**: 1h ≥1.6%, progressive 8h>1h, 1d>8h, volume ratios 1h/8h≥6, 1h/1d≥16
  - **Big Bear 60m**: 1h ≤-1.6%, progressive 8h<1h, 1d<8h, volume ratios 1h/8h≥6, 1h/1d≥16
  - **Pioneer Bull**: 5m≥1%, 15m≥1%, 3x acceleration, volume 5m/15m≥2 (early trend detection)
  - **Pioneer Bear**: 5m≤-1%, 15m≤-1%, 3x acceleration, volume 5m/15m≥2 (early downtrend)
  - **5 Big Bull**: 5m≥0.6%, progressive 15m>5m, 1h>15m, volume ratios 5m/15m≥3, 5m/1h≥6
  - **5 Big Bear**: 5m≤-0.6%, progressive 15m<5m, 1h<15m, volume ratios 5m/15m≥3, 5m/1h≥6
  - **15 Big Bull**: 15m≥1%, progressive 1h>15m, 8h>1h, volume ratios 15m/1h≥3, 15m/8h≥26
  - **15 Big Bear**: 15m≤-1%, progressive 1h<15m, 8h<1h, volume ratios 15m/1h≥3, 15m/8h≥26
  - **Bottom Hunter**: Reversal detection: 1h≤-0.7%, 15m≤-0.6%, 5m≥0.5% (bounce from lows)
  - **Top Hunter**: Reversal detection: 1h≥0.7%, 15m≥0.6%, 5m≤-0.5% (rejection from highs)
  
- [ ] **Deduplication Logic**
  - Redis cache with 5-minute TTL per `{symbol}:{rule_type}`
  - Prevent spamming same alert
  - Cooldown configurable per rule

#### Week 8: Notification & Persistence
- [ ] **Alert Persistence**
  - Insert triggered alerts to `alert_history` table
  - Batch inserts (every 10 seconds or 100 alerts)
  - Include full context (price, volume, metadata)
  
- [ ] **Webhook Integration**
  - HTTP client with retry logic (3 attempts, exponential backoff)
  - Discord webhook formatting
  - Telegram bot API support
  - Track delivery status (`webhook_sent` flag)
  
- [ ] **NATS Publishing**
  - Publish to `alerts.triggered` topic
  - Fan-out to API Gateway for WebSocket broadcast
  
- [ ] **Testing**
  - Unit tests: Each rule type with mock data
  - Integration tests: End-to-end alert flow
  - Load test: Evaluate 200 symbols × 10 rules = 2000 evaluations/min

**Deliverables**:
- ✅ Alert Engine deployed and evaluating rules
- ✅ All 10 futures alerts implemented
- ✅ Webhook notifications working
- ✅ Alerts visible in TimescaleDB

---

### Phase 5: API Gateway Service (Weeks 9-10)

#### Week 9: REST API
- [ ] **HTTP Server Setup**
  - Gin framework with middleware:
    - CORS (allow frontend domain)
    - Rate limiting (100 req/min per IP)
    - Request logging
    - Error handling
  
- [ ] **Endpoint Implementation**
  - `GET /api/health`: Service health checks
  - `GET /api/alerts`: Query alert history with filters
  - `GET /api/metrics/{symbol}`: Latest metrics
  - `POST /api/settings`: Save user preferences
  - `GET /api/symbols`: List all tracked symbols
  
- [ ] **Database Queries**
  - Optimized TimescaleDB queries with indexes
  - Pagination support (limit/offset)
  - Query result caching (Redis, 30s TTL)
  
- [ ] **Authentication**
  - Supabase JWT validation middleware
  - Extract user ID from token
  - Role-based access control (future: admin endpoints)

#### Week 10: WebSocket Hub
- [ ] **WebSocket Server**
  - Gorilla WebSocket with connection manager
  - Handle 100s of concurrent connections
  - Client authentication on connect
  - Heartbeat (30s ping/pong)
  
- [ ] **NATS Subscription**
  - Subscribe to `alerts.triggered` topic
  - Broadcast alerts to all connected WebSocket clients
  - Client-side filtering (if user has specific alerts enabled)
  
- [ ] **Connection Management**
  - Auto-reconnect handling (client-side)
  - Graceful shutdown (drain connections)
  - Metrics: active connections, messages sent/sec
  
- [ ] **Testing**
  - Unit tests: Message routing logic
  - Integration tests: WebSocket connection lifecycle
  - Load test: 100 concurrent connections, 10 alerts/min

**Deliverables**:
- ✅ API Gateway deployed with REST + WebSocket
- ✅ Frontend can query historical alerts
- ✅ Real-time alerts pushed via WebSocket
- ✅ Authentication working

---

### Phase 6: Observability & Monitoring (Week 11)

#### Metrics & Logging
- [ ] **Prometheus Setup**
  - Deploy Prometheus server (or use managed service)
  - Scrape metrics from all 4 services
  - ServiceMonitor CRDs for Kubernetes
  
- [ ] **Grafana Dashboards**
  - **Data Collector Dashboard**:
    - WebSocket connections count
    - Messages received/published rate
    - Error rate, reconnection rate
  - **Metrics Calculator Dashboard**:
    - Ring buffer memory usage
    - Calculation latency (p50, p95, p99)
    - Database insert rate
  - **Alert Engine Dashboard**:
    - Alerts triggered per type
    - Rule evaluation latency
    - Webhook success/failure rate
  - **API Gateway Dashboard**:
    - Request rate, latency, status codes
    - WebSocket connections, message throughput
  
- [ ] **Logging Stack**
  - Loki for log aggregation (or ELK stack)
  - Structured JSON logs from all services
  - Log levels: DEBUG, INFO, WARN, ERROR
  - Correlation IDs for tracing requests across services
  
- [ ] **Alerting Rules**
  - PagerDuty or Slack alerts for:
    - Service down (health check failing)
    - High error rate (>5% in 5 minutes)
    - Database connection loss
    - NATS disconnection

#### Health Checks
- [ ] **Kubernetes Probes**
  - Liveness probe: `/health` endpoint
  - Readiness probe: Check dependencies (DB, NATS)
  - Startup probe: Allow 30s for initialization
  
- [ ] **Dependency Checks**
  - Ping TimescaleDB, PostgreSQL, NATS
  - Return detailed status per dependency
  - Used by load balancer for traffic routing

**Deliverables**:
- ✅ Grafana dashboards showing all metrics
- ✅ Alerts configured for critical issues
- ✅ Logs centralized and searchable

---

### Phase 7: Testing & Optimization (Week 12)

#### Performance Testing
- [ ] **Load Testing with K6**
  - Simulate 200 symbols × 60 candles/hour = 12k messages/hour
  - WebSocket load: 100 concurrent clients
  - REST API load: 1000 req/min
  - Measure: latency, throughput, error rate
  
- [ ] **Stress Testing**
  - Push beyond normal load (500 symbols, 500 clients)
  - Identify bottlenecks and breaking points
  - Test auto-scaling (Horizontal Pod Autoscaler)
  
- [ ] **Chaos Engineering**
  - Kill random pods (test resilience)
  - Inject network latency (test timeout handling)
  - Simulate database downtime (test fallback logic)

#### Optimization
- [ ] **Memory Profiling**
  - Use `pprof` to analyze memory usage
  - Optimize ring buffer allocations
  - Reduce GC pressure (reuse objects)
  
- [ ] **CPU Profiling**
  - Identify hot paths in alert evaluation
  - Optimize indicator calculations
  - Consider caching computed values
  
- [ ] **Database Tuning**
  - Analyze slow queries (pg_stat_statements)
  - Add missing indexes
  - Tune TimescaleDB compression settings
  
- [ ] **Benchmarking**
  - Go benchmarks for critical functions
  - Target: <100ms alert evaluation latency
  - Target: <50ms REST API response time (p95)

**Deliverables**:
- ✅ System handles 200+ symbols reliably
- ✅ Performance benchmarks documented
- ✅ No memory leaks or goroutine leaks

---

### Phase 8: Frontend Integration (Week 13)

#### API Client Updates
- [ ] **Update React Frontend**
  - Replace direct Binance API calls with backend API
  - WebSocket client for real-time alerts
  - Fallback to polling if WebSocket fails
  
- [ ] **Authentication Flow**
  - Pass Supabase JWT to backend
  - Handle token refresh (401 responses)
  - Logout: Close WebSocket connection
  
- [ ] **Alert Subscription UI**
  - Checkbox list for 10 alert types
  - Save preferences to backend
  - Show toast notifications on alerts
  
- [ ] **Historical Alerts View**
  - Table showing last 48 hours of alerts
  - Filters: symbol, type, date range
  - Pagination with infinite scroll

#### Backwards Compatibility
- [ ] **Feature Flag**
  - Environment variable: `USE_BACKEND_API=true`
  - Frontend falls back to direct Binance API if false
  - A/B test with small user group first
  
- [ ] **Migration Guide**
  - Document API changes for users
  - Update README with new setup instructions

**Deliverables**:
- ✅ Frontend fully integrated with backend
- ✅ Real-time alerts working end-to-end
- ✅ User settings persisted

---

### Phase 9: Production Deployment (Week 14)

#### Production Checklist
- [ ] **Security Hardening**
  - Enable TLS for all services (cert-manager)
  - Network policies (restrict inter-service communication)
  - Pod security policies (non-root user, read-only filesystem)
  - Secret management (Kubernetes secrets or Vault)
  
- [ ] **Backup & Disaster Recovery**
  - TimescaleDB automated backups (daily)
  - PostgreSQL backups (Supabase handles this)
  - Backup NATS configuration
  - Document recovery procedures
  
- [ ] **Scaling Configuration**
  - Horizontal Pod Autoscaler (HPA):
    - Data Collector: 1-5 replicas (CPU-based)
    - Metrics Calculator: 1-3 replicas
    - Alert Engine: 2-10 replicas (based on queue depth)
    - API Gateway: 2-10 replicas (based on request rate)
  - Resource limits: CPU/memory per pod
  
- [ ] **DNS & Load Balancing**
  - Domain: `api.crypto-screener.yourdomain.com`
  - Ingress controller (nginx or Traefik)
  - SSL certificate (Let's Encrypt)
  - CDN for static assets (optional)

#### Rollout Strategy
- [ ] **Blue-Green Deployment**
  - Deploy new version alongside old
  - Test with smoke tests
  - Switch traffic with zero downtime
  - Keep old version for 24h (rollback if issues)
  
- [ ] **Monitoring During Rollout**
  - Watch error rates, latency, alerts
  - Compare metrics before/after deployment
  - User feedback channel (Discord, GitHub issues)

**Deliverables**:
- ✅ Production system live and stable
- ✅ Monitoring confirms no regressions
- ✅ Users migrated to new backend

---

## Infrastructure Details

### Kubernetes Resources

#### Namespace
```yaml
apiVersion: v1
kind: Namespace
metadata:
  name: crypto-screener
```

#### ConfigMap (shared configuration)
```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: app-config
  namespace: crypto-screener
data:
  BINANCE_API_URL: "https://fapi.binance.com"
  BINANCE_WS_URL: "wss://fstream.binance.com"
  NATS_URL: "nats://nats:4222"
  TIMESCALEDB_URL: "postgres://timescale:5432/crypto"
  POSTGRES_URL: "postgres://postgres:5432/crypto"
  LOG_LEVEL: "info"
  RETENTION_HOURS: "48"
```

#### Secrets (example - use sealed-secrets or Vault in production)
```yaml
apiVersion: v1
kind: Secret
metadata:
  name: app-secrets
  namespace: crypto-screener
type: Opaque
stringData:
  SUPABASE_JWT_SECRET: "your-jwt-secret"
  DATABASE_PASSWORD: "your-db-password"
  REDIS_PASSWORD: "your-redis-password"
```

#### Deployment: Data Collector
```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: data-collector
  namespace: crypto-screener
spec:
  replicas: 2
  selector:
    matchLabels:
      app: data-collector
  template:
    metadata:
      labels:
        app: data-collector
    spec:
      containers:
      - name: data-collector
        image: your-registry/data-collector:v1.0.0
        ports:
        - containerPort: 8080
          name: http
        - containerPort: 9090
          name: metrics
        env:
        - name: BINANCE_WS_URL
          valueFrom:
            configMapKeyRef:
              name: app-config
              key: BINANCE_WS_URL
        - name: NATS_URL
          valueFrom:
            configMapKeyRef:
              name: app-config
              key: NATS_URL
        resources:
          requests:
            cpu: 200m
            memory: 256Mi
          limits:
            cpu: 500m
            memory: 512Mi
        livenessProbe:
          httpGet:
            path: /health
            port: 8080
          initialDelaySeconds: 10
          periodSeconds: 30
        readinessProbe:
          httpGet:
            path: /ready
            port: 8080
          initialDelaySeconds: 5
          periodSeconds: 10
```

#### Service: API Gateway
```yaml
apiVersion: v1
kind: Service
metadata:
  name: api-gateway
  namespace: crypto-screener
spec:
  type: LoadBalancer
  selector:
    app: api-gateway
  ports:
  - name: http
    port: 80
    targetPort: 8080
  - name: https
    port: 443
    targetPort: 8443
```

#### Ingress: External Access
```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: crypto-screener-ingress
  namespace: crypto-screener
  annotations:
    cert-manager.io/cluster-issuer: "letsencrypt-prod"
    nginx.ingress.kubernetes.io/websocket-services: "api-gateway"
spec:
  tls:
  - hosts:
    - api.crypto-screener.com
    secretName: crypto-screener-tls
  rules:
  - host: api.crypto-screener.com
    http:
      paths:
      - path: /
        pathType: Prefix
        backend:
          service:
            name: api-gateway
            port:
              number: 80
```

#### HorizontalPodAutoscaler: Alert Engine
```yaml
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: alert-engine-hpa
  namespace: crypto-screener
spec:
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: alert-engine
  minReplicas: 2
  maxReplicas: 10
  metrics:
  - type: Resource
    resource:
      name: cpu
      target:
        type: Utilization
        averageUtilization: 70
  - type: Pods
    pods:
      metric:
        name: nats_pending_messages
      target:
        type: AverageValue
        averageValue: "100"
```

### Helm Chart Structure
```
charts/crypto-screener/
├── Chart.yaml
├── values.yaml
├── templates/
│   ├── namespace.yaml
│   ├── configmap.yaml
│   ├── secrets.yaml
│   ├── data-collector/
│   │   ├── deployment.yaml
│   │   ├── service.yaml
│   │   └── hpa.yaml
│   ├── metrics-calculator/
│   │   ├── deployment.yaml
│   │   └── service.yaml
│   ├── alert-engine/
│   │   ├── deployment.yaml
│   │   ├── service.yaml
│   │   └── hpa.yaml
│   ├── api-gateway/
│   │   ├── deployment.yaml
│   │   ├── service.yaml
│   │   ├── hpa.yaml
│   │   └── ingress.yaml
│   └── monitoring/
│       ├── servicemonitor.yaml
│       └── prometheusrule.yaml
```

### Terraform Modules
```
terraform/
├── main.tf
├── variables.tf
├── outputs.tf
├── modules/
│   ├── kubernetes/
│   │   ├── cluster.tf       # K3s/K8s cluster setup
│   │   ├── addons.tf        # cert-manager, ingress-nginx
│   │   └── monitoring.tf    # Prometheus, Grafana
│   ├── database/
│   │   ├── timescaledb.tf   # TimescaleDB instance
│   │   └── postgresql.tf    # PostgreSQL for metadata
│   ├── messaging/
│   │   └── nats.tf          # NATS cluster with JetStream
│   └── networking/
│       ├── dns.tf           # DNS records
│       └── loadbalancer.tf  # External load balancer
```

---

## Technology Stack Summary

### Backend Services (Go)
- **Language**: Go 1.22+
- **HTTP Framework**: Gin or Echo
- **WebSocket**: gorilla/websocket
- **Database**: jackc/pgx (PostgreSQL driver)
- **Messaging**: nats.go
- **Logging**: zerolog or zap
- **Metrics**: prometheus/client_golang
- **Testing**: testify, testcontainers

### Infrastructure
- **Container Orchestration**: Kubernetes (K3s initially, K8s for production)
- **IaC**: Terraform + Helm
- **CI/CD**: GitHub Actions
- **Container Registry**: Docker Hub or GitHub Container Registry
- **Secrets**: Kubernetes Secrets (+ sealed-secrets for GitOps)

### Data Layer
- **Time-Series Database**: TimescaleDB (PostgreSQL extension)
- **Metadata Database**: PostgreSQL (Supabase)
- **Caching**: Redis
- **Message Queue**: NATS with JetStream

### Observability
- **Metrics**: Prometheus + Grafana
- **Logging**: Loki or ELK stack
- **Tracing**: Jaeger or Zipkin (optional, for advanced debugging)
- **Alerting**: Prometheus Alertmanager + PagerDuty/Slack

### External Services
- **Data Source**: Binance Futures WebSocket API
- **Authentication**: Supabase Auth
- **Notifications**: Discord/Telegram webhooks

---

## Cost Estimation (Monthly)

### Self-Hosted (Raspberry Pi K3s + Cloud DB)
| Component | Cost |
|-----------|------|
| Raspberry Pi K3s cluster (existing) | $0 |
| TimescaleDB (managed, 2GB RAM) | $25 |
| Supabase Pro | $25 |
| Domain + SSL | $2 |
| Cloudflare (CDN) | $0 (free tier) |
| **Total** | **~$52/month** |

### Fully Managed (Cloud K8s)
| Component | Cost |
|-----------|------|
| Kubernetes cluster (3 nodes, 2 CPU, 4GB RAM each) | $150 |
| TimescaleDB (managed, 4GB RAM) | $50 |
| Supabase Pro | $25 |
| Load Balancer | $20 |
| Domain + SSL | $2 |
| Monitoring (Grafana Cloud) | $0 (free tier) |
| **Total** | **~$247/month** |

### Recommended Starting Point
- **Phase 1-7**: Develop on Raspberry Pi K3s ($52/month)
- **Phase 8-9**: Migrate to cloud K8s for production ($247/month)

---

## Migration Strategy from Current Frontend

### Current State
- Frontend directly calls Binance API
- Client-side calculations (VCP, Fibonacci, RSI)
- TanStack Query for caching
- Supabase for auth + user settings
- No historical data persistence

### Target State
- Frontend calls backend API Gateway
- Server-side calculations (more reliable, less client CPU)
- Real-time WebSocket for alerts
- 48-hour historical data queryable
- Webhooks for external notifications

### Migration Steps

#### Step 1: Parallel Run (Weeks 1-12)
- Build backend services without touching frontend
- Test backend with synthetic clients (K6 scripts)
- Validate data accuracy (compare with frontend calculations)

#### Step 2: Soft Launch (Week 13)
- Add feature flag to frontend: `VITE_USE_BACKEND_API=false`
- Deploy backend to production
- Enable flag for 10% of users (A/B test)
- Monitor: error rates, latency, user feedback

#### Step 3: Full Migration (Week 14)
- Enable backend for 100% of users
- Remove direct Binance API calls from frontend
- Remove client-side indicator calculations
- Simplify frontend (less code, faster bundle)

#### Step 4: Cleanup (Week 15+)
- Remove old code paths from frontend
- Archive legacy `fast.html` completely
- Update documentation
- Celebrate! 🎉

### Rollback Plan
If backend issues occur:
1. Set `VITE_USE_BACKEND_API=false` in frontend (instant rollback)
2. Frontend continues working with direct Binance API
3. Fix backend issues without user impact
4. Re-enable backend when stable

---

## Success Metrics

### Performance
- ✅ **Alert Latency**: <100ms from candle close to alert triggered
- ✅ **API Latency**: <50ms (p95) for REST endpoints
- ✅ **WebSocket Latency**: <200ms from alert triggered to frontend notification
- ✅ **Throughput**: Handle 200 symbols × 60 updates/hour = 12k messages/hour
- ✅ **Uptime**: 99.5% (allow for maintenance windows)

### Scalability
- ✅ **Horizontal Scaling**: Auto-scale from 2 to 10 replicas based on load
- ✅ **Symbol Capacity**: Support 500+ symbols without code changes
- ✅ **Client Capacity**: Handle 500 concurrent WebSocket connections
- ✅ **Data Retention**: 48 hours configurable (tested up to 7 days)

### Reliability
- ✅ **Zero Data Loss**: All candles captured from Binance (verified with audit log)
- ✅ **Alert Accuracy**: 100% match with frontend calculations (test suite)
- ✅ **Graceful Degradation**: System continues with partial outages (e.g., webhook down)
- ✅ **Recovery Time**: <5 minutes from pod restart to full operation

### Developer Experience
- ✅ **Build Time**: <2 minutes for full Docker build
- ✅ **Test Suite**: <30 seconds for all unit tests
- ✅ **Deploy Time**: <5 minutes from commit to production
- ✅ **Documentation**: 100% API endpoints documented (OpenAPI spec)

---

## Risks & Mitigations

### Risk 1: Binance API Rate Limits
**Impact**: High - Could miss candle updates  
**Mitigation**:
- WebSocket streams (no rate limits)
- Fallback to REST API polling if WebSocket disconnects
- Exponential backoff on errors

### Risk 2: High Memory Usage (288k candles)
**Impact**: Medium - Pods could OOM  
**Mitigation**:
- Use ring buffers (fixed size, no growing allocations)
- Compress old candles to disk (TimescaleDB)
- Vertical pod autoscaling if needed

### Risk 3: Alert Spam (too many notifications)
**Impact**: Medium - Users annoyed, webhook rate limits  
**Mitigation**:
- 5-minute cooldown per symbol+rule
- User preference: max alerts per hour
- Aggregate alerts (batch multiple into one message)

### Risk 4: Database Storage Growth
**Impact**: Low - TimescaleDB could fill disk  
**Mitigation**:
- 48-hour retention policy (automatic deletion)
- TimescaleDB compression (10x storage reduction)
- Monitor disk usage (alert at 80%)

### Risk 5: Kubernetes Complexity
**Impact**: Medium - Steep learning curve  
**Mitigation**:
- Start with K3s (simpler than K8s)
- Use Helm charts (community best practices)
- Terraform automates setup (repeatable)

### Risk 6: Go Learning Curve
**Impact**: Low - Team unfamiliar with Go  
**Mitigation**:
- Go is simple (25 keywords, easy to learn)
- Port existing TypeScript logic (clear reference)
- Leverage ChatGPT/Copilot for code generation

---

## Future Enhancements (Post-MVP)

### Phase 10: Advanced Features
- [ ] **Machine Learning Alerts**
  - Train models on historical data
  - Predict price movements (bullish/bearish probability)
  - Anomaly detection (unusual patterns)

- [ ] **Multi-Exchange Support**
  - Add Bybit, OKX, Kraken
  - Aggregate liquidity across exchanges
  - Arbitrage opportunity alerts

- [ ] **Custom User Rules**
  - Visual rule builder in frontend
  - Combine multiple conditions (price AND volume)
  - Backtest rules on historical data

- [ ] **Social Features**
  - Share alert configurations with community
  - Leaderboard (most profitable alerts)
  - Follow other users' strategies

### Phase 11: Mobile App
- [ ] React Native app
- [ ] Push notifications (Firebase Cloud Messaging)
- [ ] Offline mode (cache last 24h of data)

### Phase 12: Premium Features
- [ ] Advanced technical indicators (Ichimoku, Elliott Waves)
- [ ] Portfolio tracking (PnL, risk analysis)
- [ ] Automated trading (execute trades based on alerts)

---

## Appendix

### A. Go Project Structure
```
crypto-screener-backend/
├── cmd/
│   ├── data-collector/
│   │   └── main.go
│   ├── metrics-calculator/
│   │   └── main.go
│   ├── alert-engine/
│   │   └── main.go
│   └── api-gateway/
│       └── main.go
├── internal/
│   ├── binance/
│   │   ├── client.go
│   │   ├── websocket.go
│   │   └── types.go
│   ├── ringbuffer/
│   │   ├── candle_buffer.go
│   │   └── aggregator.go
│   ├── indicators/
│   │   ├── vcp.go
│   │   ├── fibonacci.go
│   │   ├── rsi.go
│   │   └── macd.go
│   ├── alerts/
│   │   ├── engine.go
│   │   ├── rules.go
│   │   └── webhook.go
│   └── api/
│       ├── handlers.go
│       ├── websocket.go
│       └── middleware.go
├── pkg/
│   ├── database/
│   │   ├── timescale.go
│   │   └── postgres.go
│   ├── messaging/
│   │   └── nats.go
│   └── observability/
│       ├── metrics.go
│       └── logging.go
├── deployments/
│   ├── docker/
│   │   ├── Dockerfile.data-collector
│   │   ├── Dockerfile.metrics-calculator
│   │   ├── Dockerfile.alert-engine
│   │   └── Dockerfile.api-gateway
│   ├── k8s/
│   │   └── (YAML manifests)
│   └── terraform/
│       └── (Terraform modules)
├── tests/
│   ├── integration/
│   ├── e2e/
│   └── load/
├── go.mod
├── go.sum
├── Makefile
└── README.md
```

### B. Example Alert Rule (Go Code)
```go
package alerts

import "github.com/your-org/crypto-screener/internal/ringbuffer"

type BigBull60mRule struct {
  VolumeIncreaseThreshold float64 // Default: 2.0
  PriceChangeThreshold    float64 // Default: 0.03 (3%)
}

func (r *BigBull60mRule) Evaluate(buffer *ringbuffer.CandleBuffer) (bool, map[string]interface{}) {
  // Get last 60 candles (1 hour)
  hourCandles := buffer.GetLast(60)
  
  // Calculate aggregated 1h candle
  hourCandle := ringbuffer.Aggregate(hourCandles)
  
  // Calculate average volume (previous 24 hours)
  avgVolume := buffer.AverageVolume(1440)
  
  // Check conditions
  volumeIncrease := hourCandle.Volume / avgVolume
  priceChange := (hourCandle.Close - hourCandle.Open) / hourCandle.Open
  
  triggered := volumeIncrease >= r.VolumeIncreaseThreshold && 
               priceChange >= r.PriceChangeThreshold
  
  metadata := map[string]interface{}{
    "volume_increase": volumeIncrease,
    "price_change_pct": priceChange * 100,
    "candle_size_pct":  (hourCandle.High - hourCandle.Low) / hourCandle.Open * 100,
  }
  
  return triggered, metadata
}
```

### C. Example NATS Message Format
```json
{
  "type": "candle.1m",
  "timestamp": "2025-12-09T10:30:00Z",
  "symbol": "BTCUSDT",
  "data": {
    "open": 42300.00,
    "high": 42350.00,
    "low": 42280.00,
    "close": 42320.50,
    "volume": 125.45,
    "quote_volume": 5308750.25,
    "trades": 1523,
    "taker_buy_volume": 62.30,
    "taker_buy_quote_volume": 2635410.15
  }
}
```

### D. Performance Benchmarks (Target)
| Metric | Target | Measurement Method |
|--------|--------|-------------------|
| Candle processing | <10ms | Go benchmark |
| VCP calculation | <5ms | Go benchmark |
| Alert evaluation (per symbol) | <1ms | Go benchmark |
| Database insert (batch 100) | <50ms | Integration test |
| WebSocket broadcast (100 clients) | <100ms | Load test (K6) |
| REST API (p95) | <50ms | Load test (K6) |
| Memory per symbol (1440 candles) | <160KB | Go pprof |

### E. Monitoring Queries (PromQL)

**Alert Rate by Type**:
```promql
rate(alert_engine_alerts_triggered_total[5m])
```

**WebSocket Connection Count**:
```promql
api_gateway_websocket_connections
```

**Database Insert Latency (p95)**:
```promql
histogram_quantile(0.95, rate(metrics_calculator_db_insert_duration_seconds_bucket[5m]))
```

**NATS Message Lag**:
```promql
nats_consumer_num_pending{stream="candles"}
```

**Pod CPU Usage**:
```promql
rate(container_cpu_usage_seconds_total{namespace="crypto-screener"}[5m])
```

---

## Next Steps

### Immediate Actions (This Week)
1. **Decision Point**: Confirm architecture and tech stack
2. **Repository Setup**: Initialize Go monorepo with project structure
3. **Infrastructure**: Provision K3s cluster on Raspberry Pi with Terraform
4. **Kickoff Meeting**: Review roadmap with stakeholders, assign responsibilities

### Questions to Answer Before Starting
- [ ] Domain name for production API? (e.g., `api.crypto-screener.com`)
- [ ] Docker registry preference? (Docker Hub, GitHub Container Registry, private)
- [ ] Monitoring preference? (Self-hosted Prometheus/Grafana or managed like Grafana Cloud)
- [ ] Git workflow? (Trunk-based, GitFlow, GitHub Flow)
- [ ] Code review process? (Required approvals, CI checks)

### Documentation to Create
- [ ] API documentation (OpenAPI/Swagger spec)
- [ ] Architecture Decision Records (ADRs)
- [ ] Runbook for common operations (deploy, rollback, debug)
- [ ] Developer onboarding guide

---

## Conclusion

This roadmap transforms your crypto screener from a client-side application into a production-grade, scalable backend system. By leveraging Go's performance and Kubernetes' orchestration, you'll achieve:

- **Reliability**: No more client-side calculation inconsistencies
- **Scalability**: Handle 200+ symbols today, 1000+ tomorrow
- **Observability**: Full visibility into data flow and alert triggers
- **User Experience**: Real-time WebSocket alerts, 48-hour history
- **Cost Efficiency**: Optimized for resource usage (Raspberry Pi capable!)

**Total Timeline**: 14 weeks (3.5 months) from start to production

**Estimated Effort**:
- Backend development: ~280 hours (2 developers × 10 weeks)
- Infrastructure setup: ~40 hours
- Testing & optimization: ~40 hours
- Frontend integration: ~20 hours
- **Total**: ~380 hours (~10 weeks full-time for a team of 2)

Ready to build the future of crypto market screening? Let's get started! 🚀

---

**Document Version**: 1.0  
**Last Updated**: December 9, 2025  
**Author**: GitHub Copilot  
**Status**: Ready for Review
