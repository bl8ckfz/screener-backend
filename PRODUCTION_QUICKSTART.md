# Production Deployment Quick Reference

## One-Command Deployment

```bash
cd deployments/k8s
./deploy.sh
```

## Prerequisites Checklist

- [ ] Kubernetes cluster configured (kubectl working)
- [ ] Docker registry accessible
- [ ] Domain DNS configured
- [ ] Secrets prepared (database URLs, JWT secret)
- [ ] cert-manager installed (for TLS)

## Essential Commands

### Deploy
```bash
# Full deployment
./deploy.sh

# Just services (after infrastructure is up)
kubectl apply -f data-collector.yaml
kubectl apply -f metrics-calculator.yaml
kubectl apply -f alert-engine.yaml
kubectl apply -f api-gateway.yaml
```

### Monitor
```bash
# Status
kubectl get pods -n crypto-screener
kubectl get hpa -n crypto-screener
kubectl get ingress -n crypto-screener

# Logs
kubectl logs -f deployment/api-gateway -n crypto-screener

# Metrics
kubectl port-forward svc/prometheus 9090:9090 -n crypto-screener
```

### Scale
```bash
# Manual scaling
kubectl scale deployment api-gateway --replicas=5 -n crypto-screener

# HPA status
kubectl get hpa -n crypto-screener
```

### Rollback
```bash
# Quick rollback all services
./rollback.sh

# Rollback specific service
kubectl rollout undo deployment/api-gateway -n crypto-screener
```

## Configuration Files

| File | Purpose |
|------|---------|
| `deploy.sh` | Automated deployment |
| `rollback.sh` | Rollback to previous version |
| `ingress.yaml` | TLS + domain config |
| `hpa.yaml` | Autoscaling rules |
| `network-policies.yaml` | Security policies |
| `backup-cronjob.yaml` | Automated backups |

## Port Reference

| Service | Metrics Port | Main Port |
|---------|-------------|-----------|
| Data Collector | 8090 | - |
| Metrics Calculator | 8091 | - |
| Alert Engine | 8092 | - |
| API Gateway | 8080 | 8080 (HTTP) |
| NATS | 8222 | 4222 |
| Redis | - | 6379 |
| Prometheus | 9090 | 9090 |

## Troubleshooting

**Pods not starting?**
```bash
kubectl describe pod <pod-name> -n crypto-screener
kubectl logs <pod-name> -n crypto-screener
```

**Service not accessible?**
```bash
kubectl get endpoints -n crypto-screener
kubectl describe ingress api-gateway-ingress -n crypto-screener
```

**HPA not scaling?**
```bash
kubectl top pods -n crypto-screener
kubectl describe hpa <hpa-name> -n crypto-screener
```

## Production URLs

- API Endpoint: `https://api.crypto-screener.yourdomain.com`
- Health Check: `https://api.crypto-screener.yourdomain.com/health`
- WebSocket: `wss://api.crypto-screener.yourdomain.com/ws/alerts`

## Support

See [PHASE9_PRODUCTION_COMPLETE.md](PHASE9_PRODUCTION_COMPLETE.md) for full documentation.
