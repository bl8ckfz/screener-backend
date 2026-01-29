# Hetzner VPS Migration Roadmap

## Overview
Migrate from Railway to Hetzner VPS running Docker Compose. Total time: ~6-8 hours over 1-2 days.

---

## Phase 1: VPS Provisioning (30 minutes)

### 1.1 Create Hetzner Account
- Go to hetzner.com/cloud
- Sign up (requires email + payment method)
- €20 credit available for new accounts

### 1.2 Create Server
**Recommended Spec: CX21**
- 2 vCPU
- 4 GB RAM
- 40 GB SSD
- €4.51/month (~$5 USD)
- Location: Choose closest to your users (Nuremberg, Helsinki, or Ashburn USA)

**Configuration:**
```bash
OS: Ubuntu 24.04 LTS
SSH Key: Upload your public key (~/.ssh/id_rsa.pub)
Firewall: Enable basic firewall
  - Allow: 22 (SSH), 80 (HTTP), 443 (HTTPS)
  - Deny: Everything else by default
```

### 1.3 Initial Server Access
```bash
# Get IP from Hetzner console (e.g., 5.161.123.45)
ssh root@YOUR_SERVER_IP

# Update system
apt update && apt upgrade -y

# Install essentials
apt install -y curl git ufw fail2ban
```

---

## Phase 2: Docker Setup (30 minutes)

### 2.1 Install Docker & Docker Compose
```bash
# Install Docker
curl -fsSL https://get.docker.com -o get-docker.sh
sh get-docker.sh

# Install Docker Compose v2
apt install -y docker-compose-plugin

# Verify
docker --version
docker compose version
```

### 2.2 Configure Firewall
```bash
# UFW setup
ufw allow 22/tcp    # SSH
ufw allow 80/tcp    # HTTP
ufw allow 443/tcp   # HTTPS
ufw enable

# Fail2ban for SSH protection
systemctl enable fail2ban
systemctl start fail2ban
```

### 2.3 Create App Directory
```bash
mkdir -p /opt/screener
cd /opt/screener
```

---

## Phase 3: Deploy Application (1 hour)

### 3.1 Clone Repository
```bash
cd /opt/screener
git clone https://github.com/bl8ckfz/screener-backend.git
cd screener-backend
```

### 3.2 Create Production docker-compose.yml
You already have `docker-compose.yml` - enhance it:

```bash
# Create production override
cat > docker-compose.prod.yml <<'EOF'
version: '3.8'

services:
  data-collector:
    restart: always
    environment:
      - NATS_URL=nats://nats:4222
      - REDIS_URL=redis://redis:6379
    depends_on:
      - nats
      - redis

  metrics-calculator:
    restart: always
    environment:
      - NATS_URL=nats://nats:4222
      - TIMESCALEDB_URL=postgresql://crypto_user:${POSTGRES_PASSWORD}@timescaledb:5432/crypto?sslmode=disable
    depends_on:
      - timescaledb
      - nats

  alert-engine:
    restart: always
    environment:
      - NATS_URL=nats://nats:4222
      - TIMESCALEDB_URL=postgresql://crypto_user:${POSTGRES_PASSWORD}@timescaledb:5432/crypto?sslmode=disable
    depends_on:
      - timescaledb
      - nats

  api-gateway:
    restart: always
    ports:
      - "8080:8080"  # Internal only, nginx will proxy
    environment:
      - NATS_URL=nats://nats:4222
      - TIMESCALEDB_URL=postgresql://crypto_user:${POSTGRES_PASSWORD}@timescaledb:5432/crypto?sslmode=disable
      - REDIS_URL=redis://redis:6379
    depends_on:
      - timescaledb
      - nats
      - redis

  timescaledb:
    restart: always
    volumes:
      - timescale-data:/var/lib/postgresql/data
    environment:
      - POSTGRES_PASSWORD=${POSTGRES_PASSWORD}
      - POSTGRES_USER=crypto_user
      - POSTGRES_DB=crypto

  nats:
    restart: always
    volumes:
      - nats-data:/data
    command: ["-js", "-sd", "/data"]

  redis:
    restart: always
    volumes:
      - redis-data:/data

volumes:
  timescale-data:
  nats-data:
  redis-data:
EOF
```

### 3.3 Set Environment Variables
```bash
# Create .env file
cat > .env <<EOF
POSTGRES_PASSWORD=$(openssl rand -base64 32)
EOF

# Secure it
chmod 600 .env
```

### 3.4 Build and Start Services
```bash
# Build images
docker compose -f docker-compose.yml -f docker-compose.prod.yml build

# Start infrastructure first
docker compose -f docker-compose.yml -f docker-compose.prod.yml up -d timescaledb nats redis

# Wait 10 seconds for DB to initialize
sleep 10

# Start application services
docker compose -f docker-compose.yml -f docker-compose.prod.yml up -d
```

### 3.5 Verify Services Running
```bash
docker compose ps

# Should see all services "Up"
# Check logs
docker compose logs -f --tail=50
```

---

## Phase 4: Database Migration from Railway (1 hour)

### 4.1 Backup Railway Database
```bash
# On your local machine
railway connect timescale

# In psql session:
\! pg_dump $DATABASE_URL > railway_backup.sql
\q
```

### 4.2 Transfer and Restore
```bash
# On local machine
scp railway_backup.sql root@YOUR_SERVER_IP:/opt/screener/

# On server
cd /opt/screener
docker compose exec timescaledb psql -U crypto_user -d crypto < railway_backup.sql

# Verify data
docker compose exec timescaledb psql -U crypto_user -d crypto -c "SELECT COUNT(*) FROM metrics_calculated;"
```

---

## Phase 5: Nginx & SSL Setup (45 minutes)

### 5.1 Install Nginx
```bash
apt install -y nginx certbot python3-certbot-nginx
```

### 5.2 Configure Nginx for API
```bash
cat > /etc/nginx/sites-available/screener-api <<'EOF'
server {
    listen 80;
    server_name api.your-domain.com;  # Replace with your domain

    location / {
        proxy_pass http://localhost:8080;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection 'upgrade';
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_cache_bypass $http_upgrade;
        
        # WebSocket support
        proxy_read_timeout 86400;
    }
}
EOF

# Enable site
ln -s /etc/nginx/sites-available/screener-api /etc/nginx/sites-enabled/
nginx -t
systemctl reload nginx
```

### 5.3 Setup SSL with Let's Encrypt
```bash
certbot --nginx -d api.your-domain.com

# Auto-renewal
systemctl enable certbot.timer
```

---

## Phase 6: CI/CD with GitHub Actions (1.5 hours)

### 6.1 Create Deploy User
```bash
# On server
useradd -m -s /bin/bash deploy
usermod -aG docker deploy
su - deploy
ssh-keygen -t ed25519 -C "deploy@hetzner"
cat ~/.ssh/id_ed25519.pub >> ~/.ssh/authorized_keys
```

### 6.2 Create Deployment Script
```bash
# On server as root
cat > /opt/screener/deploy.sh <<'EOF'
#!/bin/bash
set -e

cd /opt/screener/screener-backend

echo "Pulling latest code..."
git pull origin main

echo "Building images..."
docker compose -f docker-compose.yml -f docker-compose.prod.yml build

echo "Restarting services..."
docker compose -f docker-compose.yml -f docker-compose.prod.yml up -d

echo "Deployment complete!"
docker compose ps
EOF

chmod +x /opt/screener/deploy.sh
chown deploy:deploy /opt/screener/deploy.sh
```

### 6.3 GitHub Actions Workflow
Create in your repo: `.github/workflows/deploy-hetzner.yml`

```yaml
name: Deploy to Hetzner

on:
  push:
    branches: [ main ]

jobs:
  deploy:
    runs-on: ubuntu-latest
    steps:
      - name: Deploy to VPS
        uses: appleboy/ssh-action@master
        with:
          host: ${{ secrets.HETZNER_HOST }}
          username: deploy
          key: ${{ secrets.HETZNER_SSH_KEY }}
          script: |
            /opt/screener/deploy.sh
```

### 6.4 Add GitHub Secrets
In GitHub repo settings → Secrets:
- `HETZNER_HOST`: Your server IP
- `HETZNER_SSH_KEY`: Contents of deploy user's private key

---

## Phase 7: Monitoring Setup (1 hour)

### 7.1 Install Monitoring Stack
```bash
cat > /opt/screener/docker-compose.monitoring.yml <<'EOF'
version: '3.8'

services:
  prometheus:
    image: prom/prometheus:latest
    restart: always
    ports:
      - "9090:9090"
    volumes:
      - ./prometheus.yml:/etc/prometheus/prometheus.yml
      - prometheus-data:/prometheus
    command:
      - '--config.file=/etc/prometheus/prometheus.yml'

  grafana:
    image: grafana/grafana:latest
    restart: always
    ports:
      - "3000:3000"
    volumes:
      - grafana-data:/var/lib/grafana
    environment:
      - GF_SECURITY_ADMIN_PASSWORD=your_secure_password

volumes:
  prometheus-data:
  grafana-data:
EOF
```

### 7.2 Prometheus Config
```bash
cat > /opt/screener/prometheus.yml <<'EOF'
global:
  scrape_interval: 15s

scrape_configs:
  - job_name: 'data-collector'
    static_configs:
      - targets: ['data-collector:9090']
  
  - job_name: 'metrics-calculator'
    static_configs:
      - targets: ['metrics-calculator:9091']
  
  - job_name: 'alert-engine'
    static_configs:
      - targets: ['alert-engine:9092']
  
  - job_name: 'api-gateway'
    static_configs:
      - targets: ['api-gateway:9093']
EOF

# Start monitoring
docker compose -f docker-compose.monitoring.yml up -d
```

### 7.3 Setup Uptime Monitoring
Use free external service:
- **UptimeRobot**: Monitor https://api.your-domain.com/health/ready
- Alert via email/Telegram on downtime
- 5-minute check interval

---

## Phase 8: Frontend Update (15 minutes)

### 8.1 Update Frontend API URL
In `screener-frontend/.env.production`:
```bash
VITE_API_URL=https://api.your-domain.com
VITE_WS_URL=wss://api.your-domain.com
```

### 8.2 Redeploy Frontend
```bash
# Vercel will auto-deploy on git push
git add .env.production
git commit -m "chore: update API URL to Hetzner VPS"
git push
```

---

## Phase 9: Cutover & Testing (1 hour)

### 9.1 Pre-cutover Checklist
- [ ] All services running: `docker compose ps`
- [ ] Database has data: `docker compose exec timescaledb psql -U crypto_user -d crypto -c "SELECT COUNT(*) FROM candles_1m;"`
- [ ] SSL certificate active: `curl https://api.your-domain.com/health/ready`
- [ ] WebSocket working: Test in browser console
- [ ] Alerts triggering: Check alert_history table

### 9.2 DNS Update
```bash
# In your DNS provider (Cloudflare/Namecheap/etc)
# Update A record:
api.your-domain.com → YOUR_HETZNER_IP

# Verify DNS propagation (may take 5-60 minutes)
dig api.your-domain.com
```

### 9.3 Test Production
```bash
# Health check
curl https://api.your-domain.com/health/ready

# Metrics endpoint
curl https://api.your-domain.com/api/metrics

# WebSocket (in browser console)
const ws = new WebSocket('wss://api.your-domain.com/ws');
ws.onmessage = (e) => console.log(JSON.parse(e.data));
```

### 9.4 Monitor for 24 Hours
- Check Grafana dashboards
- Monitor logs: `docker compose logs -f`
- Verify candles_1m populating
- Verify alerts triggering

---

## Phase 10: Railway Shutdown (After 48h stable)

```bash
# Pause Railway services (keeps data)
railway service delete --service data-collector --confirm
railway service delete --service metrics-calculator --confirm
railway service delete --service alert-engine --confirm
railway service delete --service api-gateway --confirm

# Export final DB backup
railway connect timescale
pg_dump ... > final_railway_backup.sql

# Delete Railway project (optional, saves money)
```

---

## Ongoing Maintenance

### Daily Tasks (Automated)
```bash
# Add to crontab
crontab -e

# Backup database daily at 3am
0 3 * * * docker exec screener-backend-timescaledb-1 pg_dump -U crypto_user crypto | gzip > /backups/db_$(date +\%Y\%m\%d).sql.gz

# Cleanup old logs weekly
0 0 * * 0 docker system prune -f
```

### Weekly Tasks (Manual)
- Check disk usage: `df -h`
- Review logs for errors: `docker compose logs --tail=1000 | grep ERROR`
- Verify backups exist: `ls -lh /backups/`

### Monthly Tasks
- Update system: `apt update && apt upgrade -y`
- Rotate backups (keep last 30 days)
- Review Grafana metrics for anomalies

---

## Cost Breakdown

**Monthly Costs:**
- Hetzner CX21 VPS: €4.51 ($5 USD)
- Domain (if buying new): $12/year = $1/month
- **Total: ~$6/month** 💰

**vs Railway:** ~$50+/month = **$44/month savings**

**Annual Savings:** $528/year

---

## Rollback Plan (If Issues)

### Quick Rollback to Railway (< 5 minutes)
```bash
# Railway services are paused, not deleted
railway service resume --service api-gateway
railway service resume --service metrics-calculator
railway service resume --service alert-engine  
railway service resume --service data-collector

# Update DNS back to Railway URLs
# Redeploy frontend with Railway API URL
```

---

## Next Steps to Start

**Option A: Test Locally First (Recommended)**
```bash
# On your local machine
cd screener-backend
docker compose -f docker-compose.yml -f docker-compose.prod.yml up

# Verify everything works locally before VPS deploy
```

**Option B: Provision VPS Now**
1. Create Hetzner account
2. Provision CX21 server
3. SSH in and start Phase 2

---

## Notes

- **Timeline**: Plan for weekend deployment (Saturday morning start)
- **Downtime**: Expect ~15 minutes during DNS cutover
- **Dependencies**: Need domain name (or use IP temporarily)
- **Backup Strategy**: Keep Railway running for 48h as fallback
- **Cost Savings**: 88% reduction in hosting costs ($50 → $6/month)
