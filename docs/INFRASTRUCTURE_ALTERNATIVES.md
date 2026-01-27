# Infrastructure Alternatives for Starting Small

## Overview: 2-3 Clients / Free-Tier Friendly

For initial deployment with 2-3 connected clients, you have several cost-effective options ranging from completely free to ~$10-20/month.

## Option 1: Free Tier (Recommended for Start) 💰 $0-5/month

### Fly.io (Best for Getting Started)

**Why**: Generous free tier, easy deployment, includes Postgres

**Free Tier Includes**:
- 3x shared-cpu VMs (256MB RAM each)
- 3GB persistent storage
- 160GB bandwidth/month
- Free Postgres (256MB RAM, 1GB storage)
- Free TLS certificates

**Deployment**:
```bash
# Install Fly CLI
curl -L https://fly.io/install.sh | sh

# Deploy each service
cd cmd/api-gateway
fly launch --name crypto-api-gateway

cd ../data-collector
fly launch --name crypto-data-collector

# Create Postgres
fly postgres create --name crypto-db --initial-cluster-size 1

# Create Redis
fly redis create --name crypto-redis
```

**Configuration**:
```toml
# fly.toml for each service
app = "crypto-api-gateway"

[build]
  dockerfile = "../../deployments/docker/Dockerfile.api-gateway"

[[services]]
  internal_port = 8080
  protocol = "tcp"

  [[services.ports]]
    port = 80
    handlers = ["http"]
  
  [[services.ports]]
    port = 443
    handlers = ["tls", "http"]
```

**Pros**:
- ✅ Completely free for 2-3 services
- ✅ Automatic TLS certificates
- ✅ Global edge network
- ✅ Easy deployment (git push)
- ✅ Built-in Postgres and Redis

**Cons**:
- ⚠️ Shared CPU (slower than dedicated)
- ⚠️ Limited to 3 VMs on free tier
- ⚠️ 256MB RAM per VM (need to optimize)

**Best For**: MVP, testing, first users

---

### Railway.app (Easiest Setup)

**Why**: One-click deployment, $5 free credit/month, generous starter plan

**Free Tier**:
- $5 credit/month (covers ~500 hours of small instances)
- After credit: $0.000463/GB-hour RAM
- Includes Postgres, Redis
- Free TLS + custom domains

**Deployment**:
```bash
# Install Railway CLI
npm i -g @railway/cli

# Deploy from repo
railway init
railway up

# Add services
railway add postgres
railway add redis
```

**Estimated Cost for 2-3 Clients**:
- 4 services × 256MB RAM × 730 hours = ~$0.34/service = **$1.36/month**
- Free with $5 credit

**Pros**:
- ✅ Extremely easy deployment
- ✅ Generous free tier
- ✅ Built-in monitoring
- ✅ GitHub integration
- ✅ No credit card for free tier

**Cons**:
- ⚠️ Free credit runs out after ~500 hours
- ⚠️ Need to optimize for low RAM

**Best For**: Quickest MVP, non-technical deployment

---

### Render.com (Good Middle Ground)

**Free Tier**:
- Free web services (512MB RAM, 0.1 CPU)
- Free PostgreSQL (90 days, then pauses when inactive)
- 750 hours/month compute
- Free TLS + custom domains

**Deployment**:
```yaml
# render.yaml
services:
  - type: web
    name: api-gateway
    env: docker
    dockerfilePath: ./deployments/docker/Dockerfile.api-gateway
    envVars:
      - key: NATS_URL
        value: nats://nats:4222
    
  - type: web
    name: data-collector
    env: docker
    dockerfilePath: ./deployments/docker/Dockerfile.data-collector
    
databases:
  - name: crypto-db
    databaseName: crypto
    user: crypto_user
    plan: free
```

**Pros**:
- ✅ Free PostgreSQL
- ✅ Good free tier limits
- ✅ Native Docker support
- ✅ Auto-scaling available

**Cons**:
- ⚠️ Services spin down after 15min inactivity (on free tier)
- ⚠️ Database expires after 90 days

**Best For**: Demo, staging environment

---

## Option 2: Single VPS (~$5-10/month)

### Hetzner Cloud (Best Value)

**Specs**: CX11 (1 vCPU, 2GB RAM, 20GB SSD) = **€3.79/month (~$4)**

**What Runs**:
- All 4 services via Docker Compose
- PostgreSQL/TimescaleDB
- NATS
- Redis
- Prometheus/Grafana

**Setup**:
```bash
# Create server
hcloud server create --name crypto-backend --type cx11 --image ubuntu-22.04

# SSH and setup
ssh root@<server-ip>

# Install Docker
curl -fsSL https://get.docker.com | sh

# Clone repo and start
git clone your-repo
cd crypto-screener-backend
docker compose up -d

# Setup nginx reverse proxy
apt install nginx certbot python3-certbot-nginx
certbot --nginx -d api.yourdomain.com
```

**nginx Configuration**:
```nginx
server {
    listen 80;
    server_name api.yourdomain.com;
    
    location / {
        proxy_pass http://localhost:8080;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_set_header Host $host;
    }
}
```

**Pros**:
- ✅ Cheapest option with full control
- ✅ No limitations
- ✅ Can run everything
- ✅ Fixed monthly cost

**Cons**:
- ⚠️ Manual setup required
- ⚠️ Need to manage security/updates
- ⚠️ Single point of failure

**Best For**: Cost-conscious, full control needed

---

### DigitalOcean Droplet ($6/month)

**Specs**: Basic Droplet (1 vCPU, 1GB RAM, 25GB SSD) = **$6/month**

**App Platform** (Alternative):
- $5/month per service
- Automatic scaling
- Managed Postgres ($15/month)

**Setup**:
```bash
# Create droplet with Docker
doctl compute droplet create crypto-backend \
  --image docker-20-04 \
  --size s-1vcpu-1gb \
  --region nyc1

# Deploy via docker-compose
scp docker-compose.yml root@<droplet-ip>:~/
ssh root@<droplet-ip> 'cd ~ && docker compose up -d'
```

**Pros**:
- ✅ Well-documented
- ✅ 1-click Docker image
- ✅ Good support
- ✅ Managed Postgres available

**Cons**:
- ⚠️ Slightly more expensive than Hetzner
- ⚠️ App Platform services add up quickly

**Best For**: Users wanting managed services with good docs

---

## Option 3: Serverless/Edge (Mixed Approach)

### Cloudflare Workers + Supabase (Mostly Free)

**Architecture**:
- **API Gateway**: Cloudflare Workers (free 100k requests/day)
- **Database**: Supabase (free tier: 500MB, 2 concurrent connections)
- **Backend Services**: Fly.io or Railway (free tier)
- **CDN/WAF**: Cloudflare (free)

**Supabase Free Tier**:
- 500MB database
- 2GB file storage
- 50MB file uploads
- Social OAuth providers
- 50k monthly active users

**Setup**:
```bash
# Create Supabase project (web UI)
# Get connection string

# Deploy Workers for API Gateway
cd cmd/api-gateway
npx wrangler init
npx wrangler deploy
```

**Estimated Cost**: **$0/month** for 2-3 users

**Pros**:
- ✅ Completely free
- ✅ Scales automatically
- ✅ Global CDN
- ✅ Built-in auth (Supabase)

**Cons**:
- ⚠️ More complex setup
- ⚠️ Split architecture
- ⚠️ Cold starts on Workers

**Best For**: Long-term free hosting, global users

---

## Option 4: Kubernetes Free Tier

### Google Cloud GKE Autopilot (Free Trial + Always Free)

**Free Trial**: $300 credit for 90 days

**Always Free Tier**:
- 1 non-preemptible e2-micro VM (0.25 vCPU, 1GB RAM)
- 30GB storage
- 1GB network egress/month

**For Crypto Screener** (optimized):
- 1x e2-small node (2GB RAM) = ~$13/month after trial
- Run all services with tight resource limits
- Use Cloud SQL for PostgreSQL (free tier: db-f1-micro)

**Setup**:
```bash
# Create GKE cluster
gcloud container clusters create-auto crypto-cluster \
  --region us-central1 \
  --release-channel regular

# Deploy
kubectl apply -f deployments/k8s/
```

**Estimated Cost After Trial**: ~$15-20/month

**Pros**:
- ✅ True Kubernetes experience
- ✅ Managed service
- ✅ Auto-scaling
- ✅ $300 free trial

**Cons**:
- ⚠️ Complex setup
- ⚠️ Costs add up after trial
- ⚠️ Overkill for 2-3 users

**Best For**: Learning Kubernetes, planning to scale

---

## Recommended Stack for Starting

### Ultra-Budget: **$0/month** (First 6 months)

```
Frontend: Vercel (free)
Backend:  Fly.io (free tier - 3 VMs)
Database: Supabase (free tier - 500MB)
Cache:    Fly.io Redis (free)
Queue:    Fly.io (run NATS in VM)
Domain:   Cloudflare (free DNS + CDN)
TLS:      Automatic (Fly.io/Cloudflare)
```

**Limitations**:
- 256MB RAM per service
- Shared CPU
- 2 concurrent DB connections
- Need to combine services (data-collector + metrics-calculator in one VM)

---

### Low-Budget: **$4-6/month**

```
Frontend:     Vercel (free)
Backend:      Hetzner VPS CX11 (€3.79/month)
              - Docker Compose all services
              - PostgreSQL/TimescaleDB
              - NATS, Redis
Domain:       Namecheap ($1/year .xyz domain)
TLS:          Let's Encrypt (free)
Monitoring:   Grafana Cloud (free tier)
```

**Advantages**:
- Full control
- No resource limitations
- Fixed cost
- Can handle 100+ users easily

---

### Balanced: **$10-15/month**

```
Frontend:     Vercel (free)
API Gateway:  Railway ($5 credit/month)
Backend:      Fly.io (3 VMs free)
Database:     Supabase (free) or Railway Postgres ($5)
Cache:        Railway Redis (included)
Domain:       Your existing ($10/year)
Monitoring:   Included in platforms
```

**Advantages**:
- Easy deployment
- Managed services
- Good DX
- Auto-scaling
- Built-in monitoring

---

## Comparison Table

| Platform | Monthly Cost | RAM Available | Setup Time | Best For |
|----------|-------------|---------------|------------|----------|
| **Fly.io** | $0 | 3x256MB | 1 hour | Quick start, free MVP |
| **Railway** | $0-5 | Unlimited (pay-as-you-go) | 30 min | Easiest deployment |
| **Render** | $0 | 512MB/service | 1 hour | Simple free hosting |
| **Hetzner VPS** | $4 | 2GB total | 2 hours | Maximum control, cheapest |
| **DigitalOcean** | $6 | 1GB total | 2 hours | Good docs, managed options |
| **Supabase + Workers** | $0 | Function-based | 3 hours | Serverless, global |
| **GKE Autopilot** | $0 (trial) | 2GB+ | 4 hours | K8s learning, scales later |

---

## Memory Optimization for Free Tiers

Since many free tiers limit RAM to 256-512MB, optimize your services:

### Combine Services (Run 2 services per VM)

```dockerfile
# Combined data-collector + metrics-calculator
FROM golang:1.23-alpine AS builder
WORKDIR /app
COPY . .
RUN go build -o data-collector cmd/data-collector/main.go && \
    go build -o metrics-calculator cmd/metrics-calculator/main.go

FROM alpine:latest
RUN apk --no-cache add ca-certificates
COPY --from=builder /app/data-collector /app/metrics-calculator /
COPY start.sh /
CMD ["/start.sh"]
```

```bash
# start.sh
#!/bin/sh
/data-collector &
/metrics-calculator &
wait
```

### Use Lighter Alternatives

```yaml
# Use PostgreSQL instead of TimescaleDB (saves ~50MB RAM)
# Use Redis 7 alpine (50MB instead of 100MB)
# Use NATS 2.10 alpine (30MB instead of 60MB)
```

---

## My Recommendation for Your Use Case

**Phase 1: Free MVP (0-10 users)** → **Fly.io + Supabase**
- Deploy 3 combined services on Fly.io free tier
- Use Supabase free PostgreSQL
- Total cost: $0/month
- Can handle 10-20 concurrent users

**Phase 2: Growing (10-100 users)** → **Hetzner VPS**
- Single €3.79/month server
- Docker Compose everything
- Total cost: $4/month
- Can handle 100+ users with proper caching

**Phase 3: Scaling (100+ users)** → **Railway/Render**
- Separate services with auto-scaling
- Managed databases
- Total cost: $20-40/month
- Handles thousands of users

**Phase 4: Production (1000+ users)** → **GKE/EKS/AKS**
- Full Kubernetes deployment
- All features from Phase 9
- Total cost: $100-300/month
- Unlimited scale

---

## Quick Start: Deploy to Fly.io (Free)

```bash
# 1. Install Fly CLI
curl -L https://fly.io/install.sh | sh

# 2. Login
fly auth login

# 3. Create Postgres
fly postgres create --name crypto-db --initial-cluster-size 1

# 4. Deploy services
cd cmd/api-gateway
fly launch --name crypto-api --internal-port 8080

# 5. Set secrets
fly secrets set POSTGRES_URL="postgres://..." NATS_URL="nats://..."

# 6. Deploy
fly deploy
```

**Done!** Your API is live at `https://crypto-api.fly.dev`

Would you like me to create deployment configs for any specific platform?
