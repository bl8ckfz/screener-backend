#!/bin/bash
# Rollback script for production deployments
set -e

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

NAMESPACE="crypto-screener"

echo -e "${RED}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
echo -e "${RED}  Crypto Screener - Rollback${NC}"
echo -e "${RED}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
echo ""

rollback_deployment() {
    local deployment=$1
    echo -e "${YELLOW}→ Rolling back ${deployment}...${NC}"
    
    # Get rollout history
    echo -e "  Recent revisions:"
    kubectl rollout history deployment/${deployment} -n ${NAMESPACE} | tail -5
    
    # Rollback to previous revision
    kubectl rollout undo deployment/${deployment} -n ${NAMESPACE}
    
    # Wait for rollout to complete
    kubectl rollout status deployment/${deployment} -n ${NAMESPACE} --timeout=120s
    
    echo -e "${GREEN}✓ ${deployment} rolled back${NC}"
}

# Show current status
echo -e "${YELLOW}Current deployment status:${NC}"
kubectl get deployments -n ${NAMESPACE}
echo ""

# Confirm rollback
read -p "Are you sure you want to rollback all services? (yes/no): " confirm
if [ "$confirm" != "yes" ]; then
    echo -e "${BLUE}Rollback cancelled${NC}"
    exit 0
fi

echo ""

# Rollback all deployments
rollback_deployment "data-collector"
rollback_deployment "metrics-calculator"
rollback_deployment "alert-engine"
rollback_deployment "api-gateway"

echo ""
echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
echo -e "${GREEN}  Rollback Complete!${NC}"
echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
echo ""

echo -e "${YELLOW}Current status:${NC}"
kubectl get pods -n ${NAMESPACE} -o wide
