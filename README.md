# Crypto Screener Backend Service

**Scalable Go microservices for real-time cryptocurrency market data collection and alert processing**

## Overview

This backend service handles data collection, technical analysis, and alert evaluation for 200+ Binance Futures trading pairs. Built with Go and designed to run on Kubernetes, it processes 1-minute candles using sliding window algorithms and triggers intelligent trading alerts.

## Architecture

The system consists of 4 microservices:

1. **Data Collector** - WebSocket connections to Binance Futures API
2. **Metrics Calculator** - Sliding window calculations and technical indicators
3. **Alert Engine** - Rule evaluation and webhook notifications
4. **API Gateway** - REST + WebSocket API for frontend clients

## Key Features

- ✅ **200+ Trading Pairs**: Binance Futures USDT-margined contracts
- ✅ **Real-time Processing**: 1-minute candle updates via WebSocket
- ✅ **Sliding Windows**: O(1) aggregations for 5m/15m/1h/8h/1d timeframes
- ✅ **10 Alert Types**: Big Bull/Bear, Pioneer, Whale, Volume, Flat patterns
- ✅ **48-Hour Retention**: Configurable data persistence
- ✅ **Horizontal Scaling**: Auto-scaling based on load
- ✅ **Sub-100ms Latency**: Real-time alert evaluation

## Technology Stack

- **Language**: Go 1.22+
- **Orchestration**: Kubernetes (K3s/K8s)
- **Database**: TimescaleDB (time-series) + PostgreSQL (metadata)
- **Message Queue**: NATS with JetStream
- **Monitoring**: Prometheus + Grafana
- **IaC**: Terraform + Helm

## Documentation

📋 **[Complete Roadmap](docs/ROADMAP.md)** - 14-week implementation plan with architecture details, database schemas, API contracts, and deployment guides

## Project Status

✅ **Status**: Phase 1, Week 1 Complete - Foundation Setup  
📅 **Start Date**: December 9, 2025  
⏱️ **Timeline**: 14 weeks to production  
💰 **Cost**: $52-247/month depending on infrastructure choice

## Quick Start

### Local Development

```bash
# Install dependencies
make deps

# Start local infrastructure (NATS, TimescaleDB, PostgreSQL, Redis)
make run-local

# Build all services
make build

# Run a specific service
make run-data-collector

# Run with hot reload (requires air)
make install-tools
make dev-data-collector

# Stop local environment
make stop-local
```

### Development Tools

- **Make**: `make help` - Show all available commands
- **Docker Compose**: Local development environment
- **VS Code**: Debug configurations for all services
- **Air**: Hot reload for rapid development

### Project Structure

```
cmd/                    # Service entrypoints (main.go files)
internal/               # Private application code
pkg/                    # Public reusable packages
deployments/            # Docker, K8s, Terraform configs
  docker/              # Multi-stage Dockerfiles
  k8s/                 # Database init scripts
tests/                  # Integration, E2E, load tests
```

## Related Projects

- **Frontend**: [crypto-screener](https://github.com/bl8ckfz/crypto-screener) - React/TypeScript UI

## License

MIT

## Contact

For questions or contributions, please open an issue.

---

**Built with ❤️ for the crypto trading community**
