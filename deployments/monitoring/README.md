# Phase 6: Observability & Monitoring ✅

**Implementation Date**: January 23, 2026

## Quick Start

```bash
# 1. Start monitoring stack
docker compose up -d prometheus grafana

# 2. Rebuild services with observability
make build

# 3. Start services (metrics on ports 9090-9093)
# Data Collector: METRICS_PORT=9090 ./bin/data-collector
# Metrics Calculator: METRICS_PORT=9091 ./bin/metrics-calculator

# 4. Access monitoring
# Grafana: http://localhost:3001 (admin/admin)
# Prometheus: http://localhost:9090
```

## What's Included

### Metrics Collection
- ✅ Prometheus-compatible metrics
- ✅ Counters, Gauges, Histograms
- ✅ Auto-exported on `/metrics` endpoint
- ✅ 30+ predefined metrics

### Structured Logging
- ✅ zerolog-based JSON logging
- ✅ Context-aware fields
- ✅ Development mode (pretty console)
- ✅ Production mode (JSON output)

### Health Checks
- ✅ Kubernetes liveness probes (`/health/live`)
- ✅ Readiness probes (`/health/ready`)
- ✅ Dependency checks (DB, NATS)
- ✅ Detailed health status (`/health`)

### Monitoring Stack
- ✅ Prometheus (metrics aggregation)
- ✅ Grafana (visualization)
- ✅ Pre-configured dashboard
- ✅ Docker Compose integration

## Key Metrics

### Data Collector (port 9090)
```
data_collector_candles_received_total
data_collector_websocket_connections
nats_messages_published_total
```

### Metrics Calculator (port 9091)
```
metrics_calculator_candles_processed_total
metrics_calculator_metrics_calculated_total
metrics_calculator_db_insert_duration_seconds
database_connection_pool_size
```

### Alert Engine (port 9092)
```
alert_engine_alerts_triggered_total
alert_engine_evaluation_duration_seconds
alert_engine_webhooks_sent_total
```

### API Gateway (port 9093)
```
api_gateway_http_requests_total
api_gateway_http_duration_seconds
api_gateway_websocket_connections
```

## Usage Examples

### View Metrics
```bash
curl http://localhost:9090/metrics
```

### Check Health
```bash
curl http://localhost:9090/health/ready | jq
```

### Query Prometheus
```bash
# Candle processing rate
curl 'http://localhost:9090/api/v1/query?query=rate(metrics_calculator_candles_processed_total[5m])'

# P95 latency
curl 'http://localhost:9090/api/v1/query?query=histogram_quantile(0.95, rate(metrics_calculator_db_insert_duration_seconds_sum[5m]))'
```

## Documentation

- **Full Guide**: [docs/PHASE6_COMPLETE.md](./PHASE6_COMPLETE.md)
- **Summary**: [docs/PHASE6_SUMMARY.md](./PHASE6_SUMMARY.md)

## Next Steps

1. ⏳ Add observability to alert-engine
2. ⏳ Complete API Gateway metrics
3. ⏳ Load testing with metrics validation
4. ⏳ Production alerting rules (Phase 9)

---

**Status**: Infrastructure complete, services instrumented, ready for testing
