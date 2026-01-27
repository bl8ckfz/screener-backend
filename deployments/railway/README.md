# Railway.app Deployment Guide

Railway.app offers a simpler deployment experience with better free tier support and no geo-restrictions.

## Free Tier Benefits
- **$5/month credit** (enough for 2-3 users)
- **512MB RAM** per service
- **PostgreSQL included** (1GB storage)
- **No geo-restrictions** - Works with Binance API
- Auto-scaling and easy rollbacks

## Quick Start (5 minutes)

### 1. Install Railway CLI
```bash
npm install -g @railway/cli
# or
brew install railway
```

### 2. Login
```bash
railway login
```

### 3. Create New Project
```bash
cd /home/yaro/fun/crypto/screener-backend
railway init
# Choose: "Empty Project"
# Name: "crypto-screener"
```

### 4. Add PostgreSQL
```bash
railway add postgresql
```

This creates a PostgreSQL database with:
- Standard PostgreSQL 16 (TimescaleDB NOT included)
- Automatic backups
- Connection URL in `DATABASE_URL` environment variable

> **Note**: Railway's managed PostgreSQL does not include TimescaleDB extension. We use standard PostgreSQL tables with time-based indexes instead. For production deployments requiring TimescaleDB features (compression, automatic retention), consider TimescaleDB Cloud or self-hosted Kubernetes deployment.

### 5. Add NATS (using Docker)
```bash
railway add
# Choose: "Docker Image"
# Image: nats:2.10-alpine
# Name: nats
# Command: nats-server -js
```

### 6. Deploy Services

**Deploy each service separately:**

```bash
# Data Collector
railway up --service data-collector -d deployments/docker/Dockerfile.data-collector

# Metrics Calculator
railway up --service metrics-calculator -d deployments/docker/Dockerfile.metrics-calculator

# Alert Engine
railway up --service alert-engine -d deployments/docker/Dockerfile.alert-engine

# API Gateway
railway up --service api-gateway -d deployments/docker/Dockerfile.api-gateway
```

### 7. Set Environment Variables

Railway auto-provides `DATABASE_URL`. You need to set:

```bash
# Get your Railway project
railway status

# Set variables for each service
railway variables set NATS_URL=nats://nats.railway.internal:4222
railway variables set TIMESCALEDB_URL=$DATABASE_URL
railway variables set POSTGRES_URL=$DATABASE_URL
railway variables set REDIS_URL=localhost:6379
railway variables set LOG_LEVEL=info
```

### 8. Initialize Database

```bash
# Connect to PostgreSQL
railway connect postgres

# Run migrations (use Railway-specific init file)
\i deployments/railway/init-postgres.sql
```

> **Important**: Use `deployments/railway/init-postgres.sql` which creates standard PostgreSQL tables. DO NOT use `deployments/k8s/init-timescaledb.sql` as it contains TimescaleDB-specific commands that will fail on Railway's standard PostgreSQL.

### 9. Expose API Gateway

```bash
railway domain
# This generates a public URL like: crypto-screener-api.up.railway.app
```

## Architecture on Railway

```
┌─────────────────────────────────────────────────────────┐
│                   Railway Project                        │
├─────────────────────────────────────────────────────────┤
│                                                           │
│  ┌──────────────┐    ┌──────────────┐                  │
│  │   API        │    │    Data      │                  │
│  │  Gateway     │◄───┤  Collector   │◄─── Binance API │
│  │  (Public)    │    │              │                  │
│  └──────┬───────┘    └──────┬───────┘                  │
│         │                   │                           │
│         │    ┌──────────────▼──────┐                   │
│         │    │                     │                   │
│         │    │   NATS JetStream    │                   │
│         │    │   (Docker Image)    │                   │
│         │    │                     │                   │
│         │    └──────────┬──────────┘                   │
│         │               │                              │
│         │    ┌──────────▼──────┐                       │
│         │    │    Metrics      │                       │
│         │    │   Calculator    │                       │
│         │    │                 │                       │
│         │    └──────┬──────────┘                       │
│         │           │                                  │
│    ┌────▼───────────▼─────┐                           │
│    │                      │                           │
│    │  PostgreSQL +        │                           │
│    │  TimescaleDB         │                           │
│    │  (1GB Free)          │                           │
│    │                      │                           │
│    └──────────────────────┘                           │
│                                                         │
└─────────────────────────────────────────────────────────┘
```

## Environment Variables Reference

**All Services:**
- `LOG_LEVEL=info`
- `POSTGRES_URL` - Auto-provided by Railway as `DATABASE_URL`
- `TIMESCALEDB_URL` - Same as `DATABASE_URL`

**Data Collector:**
- `NATS_URL=nats://nats.railway.internal:4222`

**Metrics Calculator:**
- `NATS_URL=nats://nats.railway.internal:4222`

**Alert Engine:**
- `NATS_URL=nats://nats.railway.internal:4222`
- `REDIS_URL=localhost:6379` (in-memory for free tier)

**API Gateway:**
- `NATS_URL=nats://nats.railway.internal:4222`
- `HTTP_ADDR=:8080`

## Web Dashboard

Railway provides an excellent web dashboard:
- **Metrics**: CPU, Memory, Network usage
- **Logs**: Real-time log streaming
- **Deployments**: Easy rollbacks
- **Settings**: Environment variables, domains, replicas

Access at: https://railway.app/dashboard

## Cost Management

**Free Tier:**
- $5/month credit
- Enough for: API Gateway + Data Collector + Calculator + Alert Engine + Postgres
- ~160 hours/month runtime

**When you exceed free tier:**
- Each service: ~$0.000231/GB-hour (memory)
- ~$0.000463/vCPU-hour (compute)
- Estimated: $8-12/month for full stack

## Monitoring

```bash
# Watch logs
railway logs --service api-gateway

# Check metrics
railway status

# Shell into service
railway shell --service api-gateway
```

## Troubleshooting

### Service won't start
```bash
railway logs --service <service-name>
```

### Database connection issues
Check that `DATABASE_URL` is available:
```bash
railway variables --service api-gateway
```

### NATS connection issues
Ensure NATS service is running:
```bash
railway status
```

### Binance API blocked
Railway uses US/EU datacenters - should work fine with Binance.
If blocked, add HTTP_PROXY environment variable.

## Next Steps

1. **Custom Domain**: Add your domain in Railway dashboard
2. **Monitoring**: Set up Railway's native monitoring
3. **Alerts**: Configure webhook alerts for service failures
4. **Scaling**: Increase replicas in dashboard when needed
5. **CI/CD**: Connect GitHub repo for automatic deployments

## Alternative: Railway CLI Project File

Create `railway.toml` in project root for automated deployments:

```toml
[build]
builder = "DOCKERFILE"
dockerfilePath = "deployments/docker/Dockerfile.api-gateway"

[deploy]
numReplicas = 1
restartPolicyType = "ON_FAILURE"
restartPolicyMaxRetries = 10
```

Then deploy with:
```bash
railway up
```
