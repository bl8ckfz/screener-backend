#!/bin/bash
# Railway Deployment Script
set -e

echo "🚂 Railway.app Deployment Setup"
echo "================================"
echo ""

# Check if railway CLI is installed
if ! command -v railway &> /dev/null; then
    echo "❌ Railway CLI not found. Install it with:"
    echo "   npm install -g @railway/cli"
    exit 1
fi

echo "✅ Railway CLI installed"
echo ""

# Check if logged in
if ! railway whoami &> /dev/null; then
    echo "🔐 Logging into Railway..."
    railway login --browserless
fi

echo "✅ Logged in to Railway"
echo ""

PROJECT_URL=$(railway status | grep "Project" | awk '{print $3}')
echo "📦 Project: $PROJECT_URL"
echo ""

echo "📝 Next Steps:"
echo ""
echo "1. Add PostgreSQL Database:"
echo "   - Open: $PROJECT_URL"
echo "   - Click '+ New' → 'Database' → 'Add PostgreSQL'"
echo "   - Railway will auto-create DATABASE_URL environment variable"
echo ""

echo "2. Add NATS Server:"
echo "   - Click '+ New' → 'Empty Service'"
echo "   - Name it 'nats'"
echo "   - Settings → Deploy → Docker Image: nats:2.10-alpine"
echo "   - Command: nats-server -js -m 8222"
echo ""

echo "3. Deploy Services (run these after database is ready):"
echo ""
echo "   # API Gateway (public)"
echo "   railway up --service api-gateway"
echo ""
echo "   # Data Collector"
echo "   railway up --service data-collector"
echo ""
echo "   # Metrics Calculator"
echo "   railway up --service metrics-calculator"
echo ""
echo "   # Alert Engine"
echo "   railway up --service alert-engine"
echo ""

echo "4. Set Environment Variables:"
echo "   After services are created, set these variables in Railway dashboard:"
echo ""
echo "   For all services:"
echo "   - LOG_LEVEL=info"
echo "   - POSTGRES_URL=\${{Postgres.DATABASE_URL}}"
echo "   - TIMESCALEDB_URL=\${{Postgres.DATABASE_URL}}"
echo ""
echo "   For services that use NATS:"
echo "   - NATS_URL=nats://nats.railway.internal:4222"
echo ""
echo "   For Alert Engine only:"
echo "   - REDIS_URL=localhost:6379"
echo ""

echo "5. Generate Public URL for API Gateway:"
echo "   - Click on 'api-gateway' service"
echo "   - Settings → Networking → Generate Domain"
echo ""

echo "🎉 Setup instructions complete!"
echo ""
echo "Visit Railway Dashboard: $PROJECT_URL"
