# Phase 9: Production Deployment - Complete ✅

**Date**: January 23, 2026  
**Status**: Production-ready deployment configured

## Overview

Phase 9 adds all production-ready components for secure, scalable Kubernetes deployment with high availability, automated backups, and operational tooling.

## Completed Components

### 1. Security Hardening ✅

**TLS/SSL Configuration**:
- Ingress with Let's Encrypt (cert-manager)
- Automatic certificate renewal
- HTTPS redirect enforced
- Created: [ingress.yaml](../deployments/k8s/ingress.yaml)

**Network Security**:
- Network policies for all services
- Default deny-all ingress
- Explicit allow rules for service communication
- External access restricted (Binance API, webhooks only)
- Created: [network-policies.yaml](../deployments/k8s/network-policies.yaml)

**Pod Security**:
- Restricted pod security standards
- Non-root containers
- Read-only root filesystems
- No privilege escalation
- Resource quotas and limits
- Created: [pod-security.yaml](../deployments/k8s/pod-security.yaml)

### 2. High Availability ✅

**Horizontal Pod Autoscaling (HPA)**:
- Data Collector: 1-5 replicas (CPU/memory based)
- Metrics Calculator: 1-3 replicas
- Alert Engine: 2-10 replicas (alert volume based)
- API Gateway: 2-10 replicas (request rate based)
- Intelligent scale-up/down policies
- Created: [hpa.yaml](../deployments/k8s/hpa.yaml)

**Pod Disruption Budgets**:
- Ensures minimum availability during updates
- API Gateway: min 1 replica always running
- Alert Engine: min 1 replica always running
- NATS: min 1 replica always running

### 3. Backup & Disaster Recovery ✅

**Automated Backups**:
- Daily TimescaleDB backups at 2 AM UTC
- Compressed SQL dumps
- 7-day retention
- 50GB persistent volume
- Created: [backup-cronjob.yaml](../deployments/k8s/backup-cronjob.yaml)

**Backup Features**:
- Automatic cleanup of old backups
- Size verification
- Error handling with exit codes
- ServiceAccount with minimal permissions

### 4. Deployment Automation ✅

**Production Deployment Script**:
- One-command deployment: `./deploy.sh`
- Automated Docker image build and push
- Prerequisites checking
- Sequential deployment with wait conditions
- Status validation after deployment
- Created: [deploy.sh](../deployments/k8s/deploy.sh)

**Rollback Script**:
- Quick rollback to previous version: `./rollback.sh`
- Shows deployment history
- Confirmation prompt for safety
- Rolls back all services
- Created: [rollback.sh](../deployments/k8s/rollback.sh)

## Production Architecture

```
                           Internet
                              │
                              │ HTTPS (443)
                              ↓
                    ┌──────────────────┐
                    │  Ingress (nginx) │
                    │  + cert-manager  │
                    │  + TLS/SSL       │
                    └────────┬─────────┘
                             │
                             │ Network Policies
                             ↓
                    ┌──────────────────┐
                    │   API Gateway    │
                    │  (2-10 replicas) │
                    │   + HPA enabled  │
                    └────────┬─────────┘
                             │
            ┌────────────────┼────────────────┐
            │                │                │
            ↓                ↓                ↓
    ┌──────────────┐  ┌──────────────┐  ┌──────────────┐
    │     Data     │  │   Metrics    │  │    Alert     │
    │  Collector   │  │  Calculator  │  │   Engine     │
    │ (1-5 repls)  │  │ (1-3 repls)  │  │ (2-10 repls) │
    └──────┬───────┘  └──────┬───────┘  └──────┬───────┘
           │                 │                 │
           └────────┬────────┴────────┬────────┘
                    │                 │
                    ↓                 ↓
            ┌──────────────┐  ┌──────────────┐
            │     NATS     │  │  TimescaleDB │
            │  (StatefulS) │  │   + Backups  │
            └──────────────┘  └──────────────┘
```

## Deployment Files

| File | Purpose | Status |
|------|---------|--------|
| `namespace-and-secrets.yaml` | Namespace + secrets template | ✅ Ready |
| `data-collector.yaml` | Data collection service | ✅ Updated |
| `metrics-calculator.yaml` | Metrics computation service | ✅ Updated |
| `alert-engine.yaml` | Alert evaluation service | ✅ Updated |
| `api-gateway.yaml` | REST/WebSocket API | ✅ Updated |
| `nats.yaml` | Message queue StatefulSet | ✅ Ready |
| `redis.yaml` | Cache StatefulSet | ✅ Ready |
| `ingress.yaml` | TLS/SSL ingress | ✅ **NEW** |
| `hpa.yaml` | Autoscaling policies | ✅ **NEW** |
| `network-policies.yaml` | Security policies | ✅ **NEW** |
| `pod-security.yaml` | Pod security + quotas | ✅ **NEW** |
| `backup-cronjob.yaml` | Automated backups | ✅ **NEW** |
| `prometheus-alerts.yaml` | Alerting rules | ✅ Existing |
| `deploy.sh` | Deployment script | ✅ **NEW** |
| `rollback.sh` | Rollback script | ✅ **NEW** |

## Production Deployment Steps

### Prerequisites

1. **Kubernetes Cluster**: 1.24+ with kubectl configured
2. **Docker Registry**: Push images to your registry
3. **Domain**: DNS configured for API endpoint
4. **Secrets**: Database credentials, JWT secrets ready
5. **Cert-Manager**: Installed for TLS certificates (optional but recommended)

### Quick Start

```bash
# 1. Configure environment
export DOCKER_REGISTRY="your-registry.example.com"
export IMAGE_TAG="v1.0.0"

# 2. Update secrets
kubectl apply -f deployments/k8s/namespace-and-secrets.yaml
kubectl edit secret crypto-secrets -n crypto-screener

# 3. Update Ingress domain
sed -i 's/api.crypto-screener.yourdomain.com/your-actual-domain.com/g' deployments/k8s/ingress.yaml

# 4. Run deployment
cd deployments/k8s
./deploy.sh

# 5. Verify deployment
kubectl get pods -n crypto-screener
kubectl get hpa -n crypto-screener
kubectl get ingress -n crypto-screener
```

### Manual Deployment (Step-by-Step)

```bash
# Infrastructure
kubectl apply -f namespace-and-secrets.yaml
kubectl apply -f nats.yaml
kubectl apply -f redis.yaml

# Wait for infrastructure
kubectl wait --for=condition=ready pod -l app=nats -n crypto-screener --timeout=120s
kubectl wait --for=condition=ready pod -l app=redis -n crypto-screener --timeout=120s

# Application services
kubectl apply -f data-collector.yaml
kubectl apply -f metrics-calculator.yaml
kubectl apply -f alert-engine.yaml
kubectl apply -f api-gateway.yaml

# Autoscaling
kubectl apply -f hpa.yaml

# Security
kubectl apply -f network-policies.yaml
kubectl apply -f pod-security.yaml

# Ingress (after cert-manager installed)
kubectl apply -f ingress.yaml

# Backups
kubectl apply -f backup-cronjob.yaml
```

## Monitoring & Operations

### Check Deployment Status

```bash
# Pods status
kubectl get pods -n crypto-screener -o wide

# Services and LoadBalancers
kubectl get svc -n crypto-screener

# Autoscaling status
kubectl get hpa -n crypto-screener

# Ingress status
kubectl get ingress -n crypto-screener

# Recent events
kubectl get events -n crypto-screener --sort-by='.lastTimestamp'
```

### View Logs

```bash
# API Gateway logs
kubectl logs -f deployment/api-gateway -n crypto-screener

# Data Collector logs
kubectl logs -f deployment/data-collector -n crypto-screener

# All services
kubectl logs -f -l component=backend -n crypto-screener
```

### Access Metrics

```bash
# Port-forward Prometheus
kubectl port-forward svc/prometheus 9090:9090 -n crypto-screener

# Access at http://localhost:9090
```

### Scale Manually

```bash
# Increase API Gateway replicas
kubectl scale deployment api-gateway --replicas=5 -n crypto-screener

# Check HPA status
kubectl describe hpa api-gateway-hpa -n crypto-screener
```

### Rollback Deployment

```bash
# Quick rollback to previous version
./rollback.sh

# Or manually rollback specific service
kubectl rollout undo deployment/api-gateway -n crypto-screener

# Check rollout history
kubectl rollout history deployment/api-gateway -n crypto-screener
```

## Security Checklist

- [x] TLS certificates configured (Let's Encrypt)
- [x] Network policies applied (restrict inter-service communication)
- [x] Pod security standards enforced (non-root, read-only FS)
- [x] Resource quotas defined
- [x] Secrets managed via Kubernetes (not hardcoded)
- [x] HTTPS redirect enforced on Ingress
- [x] CORS configured properly
- [x] Rate limiting enabled on API Gateway
- [ ] Vault or external secret manager (optional upgrade)
- [ ] Audit logging enabled (cluster-level)

## High Availability Checklist

- [x] HPA configured for all services
- [x] PodDisruptionBudgets defined
- [x] Multiple replicas for critical services (API Gateway, Alert Engine)
- [x] StatefulSets for stateful services (NATS, Redis)
- [x] Health checks configured (liveness, readiness)
- [x] Resource limits prevent resource exhaustion
- [ ] Multi-zone deployment (depends on cluster setup)
- [ ] Database replication (use managed service like RDS)

## Backup & Recovery Checklist

- [x] Automated daily backups scheduled (2 AM UTC)
- [x] 7-day retention policy
- [x] 50GB persistent volume for backups
- [x] Backup verification (size check)
- [x] ServiceAccount with minimal permissions
- [ ] Test restore procedure (manual verification needed)
- [ ] Off-site backup replication (optional)
- [ ] Disaster recovery runbook documented

## Cost Optimization

**Resource Allocation**:
- Start with minimum replicas (1-2 per service)
- Let HPA scale based on actual load
- Monitor actual usage for 1-2 weeks
- Adjust resource limits based on real metrics

**Estimated Monthly Cost** (AWS EKS example):
- 3x t3.medium nodes: ~$100
- LoadBalancer: ~$20
- EBS volumes (200GB): ~$20
- TimescaleDB RDS (if external): ~$100-200
- **Total**: ~$240-340/month

**Scaling** (with HPA at moderate load):
- 5-10 pods total: Still fits on 3 nodes
- No additional compute cost until heavy load

## Production Readiness

### Performance Targets

| Metric | Target | Current Status |
|--------|--------|---------------|
| Alert evaluation latency | <100ms | ✅ Verified (~0.7ms avg) |
| REST API p95 latency | <50ms | ⏳ To be measured |
| WebSocket connections | 100+ concurrent | ⏳ To be tested |
| Memory per service | <512MB | ✅ Verified |
| CPU usage | <70% under load | ⏳ To be tested |

### Next Steps

1. **Load Testing** (Phase 7):
   - Test with realistic load (200 symbols, 100 clients)
   - Verify HPA behavior under stress
   - Measure actual resource usage

2. **DNS Configuration**:
   - Point domain to LoadBalancer IP
   - Verify TLS certificate issuance
   - Test HTTPS access

3. **Monitoring Setup**:
   - Configure Grafana dashboards
   - Set up alerting (Slack, PagerDuty)
   - Document on-call runbook

4. **Documentation**:
   - Update frontend to use production API
   - Document API endpoints for users
   - Create troubleshooting guide

## Troubleshooting

### Pods Not Starting

```bash
# Check pod status
kubectl describe pod <pod-name> -n crypto-screener

# Check logs
kubectl logs <pod-name> -n crypto-screener --previous

# Common issues:
# - ImagePullBackOff: Check Docker registry credentials
# - CrashLoopBackOff: Check environment variables and secrets
# - Pending: Check resource availability
```

### Service Not Accessible

```bash
# Check service endpoints
kubectl get endpoints -n crypto-screener

# Check Ingress
kubectl describe ingress api-gateway-ingress -n crypto-screener

# Test internal connectivity
kubectl run -it --rm debug --image=curlimages/curl --restart=Never -n crypto-screener -- sh
curl http://api-gateway/health
```

### Database Connection Issues

```bash
# Check secret values
kubectl get secret crypto-secrets -n crypto-screener -o jsonpath='{.data.timescaledb-url}' | base64 -d

# Test from pod
kubectl exec -it deployment/api-gateway -n crypto-screener -- sh
# Then try connecting to database
```

### HPA Not Scaling

```bash
# Check metrics server
kubectl top nodes
kubectl top pods -n crypto-screener

# Check HPA status
kubectl describe hpa <hpa-name> -n crypto-screener

# Common issues:
# - Metrics server not installed
# - Resource requests not defined
# - Insufficient cluster capacity
```

## References

- [Kubernetes Documentation](https://kubernetes.io/docs/)
- [cert-manager Documentation](https://cert-manager.io/docs/)
- [Prometheus Operator](https://prometheus-operator.dev/)
- [NATS JetStream](https://docs.nats.io/nats-concepts/jetstream)

## Support

For deployment issues:
1. Check logs: `kubectl logs -f deployment/<service> -n crypto-screener`
2. Check events: `kubectl get events -n crypto-screener`
3. Review this guide's troubleshooting section
4. Check [GitHub Issues](https://github.com/your-repo/issues)

---

**Phase 9 Status**: ✅ **COMPLETE** - Production deployment ready!
