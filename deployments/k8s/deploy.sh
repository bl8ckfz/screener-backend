#!/bin/bash
# Production deployment script for crypto-screener backend
set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Configuration
NAMESPACE="crypto-screener"
DOCKER_REGISTRY="${DOCKER_REGISTRY:-your-registry.example.com}"
IMAGE_TAG="${IMAGE_TAG:-latest}"
KUBECONFIG="${KUBECONFIG:-$HOME/.kube/config}"

echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
echo -e "${BLUE}  Crypto Screener - Production Deployment${NC}"
echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
echo ""

# Check prerequisites
echo -e "${YELLOW}→ Checking prerequisites...${NC}"

if ! command -v kubectl &> /dev/null; then
    echo -e "${RED}✗ kubectl not found. Please install kubectl.${NC}"
    exit 1
fi

if ! command -v docker &> /dev/null; then
    echo -e "${RED}✗ docker not found. Please install Docker.${NC}"
    exit 1
fi

# Test cluster connectivity
if ! kubectl cluster-info &> /dev/null; then
    echo -e "${RED}✗ Cannot connect to Kubernetes cluster${NC}"
    echo -e "  Check your kubeconfig: ${KUBECONFIG}"
    exit 1
fi

echo -e "${GREEN}✓ All prerequisites met${NC}"
echo ""

# Build and push Docker images
echo -e "${YELLOW}→ Building Docker images...${NC}"

build_and_push() {
    local service=$1
    local dockerfile=$2
    local image="${DOCKER_REGISTRY}/crypto-screener-${service}:${IMAGE_TAG}"
    
    echo -e "  Building ${service}..."
    docker build -f ${dockerfile} -t ${image} . --quiet
    
    echo -e "  Pushing ${image}..."
    docker push ${image} --quiet
    
    echo -e "${GREEN}  ✓ ${service} image ready${NC}"
}

cd "$(dirname "$0")/../.."

build_and_push "data-collector" "deployments/docker/Dockerfile.data-collector"
build_and_push "metrics-calculator" "deployments/docker/Dockerfile.metrics-calculator"
build_and_push "alert-engine" "deployments/docker/Dockerfile.alert-engine"
build_and_push "api-gateway" "deployments/docker/Dockerfile.api-gateway"

echo ""

# Create namespace if it doesn't exist
echo -e "${YELLOW}→ Creating namespace...${NC}"
kubectl create namespace ${NAMESPACE} --dry-run=client -o yaml | kubectl apply -f -
echo -e "${GREEN}✓ Namespace ready${NC}"
echo ""

# Apply secrets (prompt user to configure)
echo -e "${YELLOW}→ Checking secrets...${NC}"
if ! kubectl get secret crypto-secrets -n ${NAMESPACE} &> /dev/null; then
    echo -e "${RED}✗ Secrets not found${NC}"
    echo -e "  Please create secrets first:"
    echo -e "  ${BLUE}kubectl apply -f deployments/k8s/namespace-and-secrets.yaml${NC}"
    echo -e "  Then edit the secret values:"
    echo -e "  ${BLUE}kubectl edit secret crypto-secrets -n ${NAMESPACE}${NC}"
    exit 1
fi
echo -e "${GREEN}✓ Secrets configured${NC}"
echo ""

# Deploy infrastructure components
echo -e "${YELLOW}→ Deploying infrastructure...${NC}"

echo -e "  Deploying NATS..."
kubectl apply -f deployments/k8s/nats.yaml

echo -e "  Deploying Redis..."
kubectl apply -f deployments/k8s/redis.yaml

echo -e "  Waiting for infrastructure to be ready..."
kubectl wait --for=condition=ready pod -l app=nats -n ${NAMESPACE} --timeout=120s
kubectl wait --for=condition=ready pod -l app=redis -n ${NAMESPACE} --timeout=120s

echo -e "${GREEN}✓ Infrastructure deployed${NC}"
echo ""

# Deploy application services
echo -e "${YELLOW}→ Deploying application services...${NC}"

kubectl apply -f deployments/k8s/data-collector.yaml
kubectl apply -f deployments/k8s/metrics-calculator.yaml
kubectl apply -f deployments/k8s/alert-engine.yaml
kubectl apply -f deployments/k8s/api-gateway.yaml

echo -e "  Waiting for services to be ready..."
sleep 10

kubectl wait --for=condition=ready pod -l app=data-collector -n ${NAMESPACE} --timeout=120s || true
kubectl wait --for=condition=ready pod -l app=metrics-calculator -n ${NAMESPACE} --timeout=120s || true
kubectl wait --for=condition=ready pod -l app=alert-engine -n ${NAMESPACE} --timeout=120s || true
kubectl wait --for=condition=ready pod -l app=api-gateway -n ${NAMESPACE} --timeout=120s || true

echo -e "${GREEN}✓ Application services deployed${NC}"
echo ""

# Apply HPA (Horizontal Pod Autoscaler)
echo -e "${YELLOW}→ Configuring autoscaling...${NC}"
kubectl apply -f deployments/k8s/hpa.yaml
echo -e "${GREEN}✓ Autoscaling configured${NC}"
echo ""

# Apply network policies
echo -e "${YELLOW}→ Applying network policies...${NC}"
kubectl apply -f deployments/k8s/network-policies.yaml
echo -e "${GREEN}✓ Network policies applied${NC}"
echo ""

# Apply pod security
echo -e "${YELLOW}→ Applying security policies...${NC}"
kubectl apply -f deployments/k8s/pod-security.yaml
echo -e "${GREEN}✓ Security policies applied${NC}"
echo ""

# Deploy monitoring (optional)
if [ -f "deployments/k8s/prometheus.yaml" ]; then
    echo -e "${YELLOW}→ Deploying monitoring stack...${NC}"
    kubectl apply -f deployments/k8s/prometheus.yaml
    echo -e "${GREEN}✓ Monitoring deployed${NC}"
    echo ""
fi

# Deploy Ingress (optional)
if [ -f "deployments/k8s/ingress.yaml" ]; then
    echo -e "${YELLOW}→ Deploying Ingress...${NC}"
    echo -e "${YELLOW}⚠ Remember to update domain in ingress.yaml${NC}"
    kubectl apply -f deployments/k8s/ingress.yaml
    echo -e "${GREEN}✓ Ingress configured${NC}"
    echo ""
fi

# Setup backup CronJob
echo -e "${YELLOW}→ Setting up automated backups...${NC}"
kubectl apply -f deployments/k8s/backup-cronjob.yaml
echo -e "${GREEN}✓ Backup job scheduled${NC}"
echo ""

# Display deployment status
echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
echo -e "${GREEN}  Deployment Complete!${NC}"
echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
echo ""

echo -e "${YELLOW}Service Status:${NC}"
kubectl get pods -n ${NAMESPACE} -o wide

echo ""
echo -e "${YELLOW}Service Endpoints:${NC}"
kubectl get svc -n ${NAMESPACE}

echo ""
echo -e "${YELLOW}HPA Status:${NC}"
kubectl get hpa -n ${NAMESPACE}

echo ""
echo -e "${BLUE}Next Steps:${NC}"
echo -e "  1. Configure DNS to point to LoadBalancer IP"
echo -e "  2. Update secrets with production credentials"
echo -e "  3. Monitor logs: ${GREEN}kubectl logs -f deployment/api-gateway -n ${NAMESPACE}${NC}"
echo -e "  4. Check metrics: ${GREEN}kubectl port-forward svc/prometheus 9090:9090 -n ${NAMESPACE}${NC}"
echo -e "  5. Test API: ${GREEN}curl https://api.crypto-screener.yourdomain.com/health${NC}"
echo ""

echo -e "${GREEN}Deployment successful! 🚀${NC}"
