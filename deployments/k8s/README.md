# Kubernetes Deployment Guide

## Prerequisites

- Kubernetes cluster (1.24+)
- kubectl configured
- Docker images built and pushed to registry
- External PostgreSQL/TimescaleDB (or deploy in cluster)

## Quick Deploy

```bash
# 1. Create namespace and secrets
kubectl apply -f deployments/k8s/namespace-and-secrets.yaml

# Edit secrets with real values:
kubectl edit secret crypto-secrets -n crypto-screener

# 2. Deploy infrastructure (NATS, Redis)
kubectl apply -f deployments/k8s/nats.yaml
kubectl apply -f deployments/k8s/redis.yaml

# 3. Deploy services
kubectl apply -f deployments/k8s/data-collector.yaml
kubectl apply -f deployments/k8s/metrics-calculator.yaml
kubectl apply -f deployments/k8s/alert-engine.yaml
kubectl apply -f deployments/k8s/api-gateway.yaml

# 4. Verify deployment
kubectl get pods -n crypto-screener
kubectl get svc -n crypto-screener
```

## Configuration

### Secrets (REQUIRED)

Edit `namespace-and-secrets.yaml` or use kubectl:

```bash
kubectl create secret generic crypto-secrets \
  --from-literal=timescaledb-url='postgres://user:pass@host:5432/crypto?sslmode=require' \
  --from-literal=postgres-url='postgres://user:pass@host:5432/crypto_metadata?sslmode=require' \
  --from-literal=supabase-jwt-secret='your-jwt-secret' \
  --from-literal=webhook-urls='https://discord.com/api/webhooks/...' \
  -n crypto-screener
```

### Resource Limits

Current defaults (adjust based on load):

| Service | Memory Request | Memory Limit | CPU Request | CPU Limit |
|---------|---------------|--------------|-------------|-----------|
| data-collector | 128Mi | 512Mi | 100m | 500m |
| metrics-calculator | 256Mi | 1Gi | 200m | 1000m |
| alert-engine | 256Mi | 512Mi | 200m | 500m |
| api-gateway | 128Mi | 512Mi | 100m | 500m |
| NATS | 256Mi | 1Gi | 100m | 500m |
| Redis | 128Mi | 512Mi | 50m | 200m |

### Scaling

```bash
# Scale API Gateway for high traffic
kubectl scale deployment api-gateway --replicas=5 -n crypto-screener

# Horizontal Pod Autoscaler (HPA)
kubectl autoscale deployment api-gateway \
  --cpu-percent=70 \
  --min=2 \
  --max=10 \
  -n crypto-screener
```

## Monitoring

### Health Checks

All services expose:
- `/health/live` - Liveness probe
- `/health/ready` - Readiness probe
- `/metrics` - Prometheus metrics

### Prometheus Integration

Services expose metrics on ports 9090-9093:
- data-collector: 9090
- metrics-calculator: 9091
- alert-engine: 9092
- api-gateway: 9093 (also port 80)

Configure Prometheus to scrape:
```yaml
- job_name: 'crypto-screener'
  kubernetes_sd_configs:
  - role: pod
    namespaces:
      names:
      - crypto-screener
  relabel_configs:
  - source_labels: [__meta_kubernetes_pod_label_app]
    action: keep
    regex: (data-collector|metrics-calculator|alert-engine|api-gateway)
  - source_labels: [__meta_kubernetes_pod_annotation_prometheus_io_scrape]
    action: keep
    regex: true
  - source_labels: [__meta_kubernetes_pod_annotation_prometheus_io_port]
    action: replace
    target_label: __address__
    regex: ([^:]+)(?::\d+)?;(\d+)
    replacement: $1:$2
```

### Alerting

Apply Prometheus alerting rules:
```bash
kubectl apply -f deployments/k8s/prometheus-alerts.yaml
```

## Troubleshooting

### Check Service Status

```bash
# View all pods
kubectl get pods -n crypto-screener

# Check logs
kubectl logs -f deployment/data-collector -n crypto-screener
kubectl logs -f deployment/metrics-calculator -n crypto-screener
kubectl logs -f deployment/alert-engine -n crypto-screener
kubectl logs -f deployment/api-gateway -n crypto-screener

# Check events
kubectl get events -n crypto-screener --sort-by='.lastTimestamp'
```

### Test Health Endpoints

```bash
# Port forward to test locally
kubectl port-forward svc/api-gateway 8080:80 -n crypto-screener

# Check health
curl http://localhost:8080/health
curl http://localhost:8080/health/ready

# Check metrics
kubectl port-forward svc/data-collector 9090:9090 -n crypto-screener
curl http://localhost:9090/metrics
```

### Database Connectivity

```bash
# Test from a pod
kubectl run -it --rm debug --image=postgres:16 --restart=Never -n crypto-screener -- \
  psql "postgres://user:pass@timescaledb:5432/crypto"
```

### NATS Connectivity

```bash
# Check NATS server
kubectl exec -it nats-0 -n crypto-screener -- nats-server --version
kubectl exec -it nats-0 -n crypto-screener -- nats-server --connz
```

## Production Checklist

- [ ] Update secrets with production values
- [ ] Configure external database (managed TimescaleDB/PostgreSQL)
- [ ] Set up SSL/TLS for API Gateway (Ingress + cert-manager)
- [ ] Configure backup for persistent volumes
- [ ] Set up log aggregation (Loki, ELK, or cloud provider)
- [ ] Configure Prometheus alerting to Slack/PagerDuty
- [ ] Enable pod security policies
- [ ] Set resource quotas for namespace
- [ ] Configure network policies
- [ ] Set up HPA for auto-scaling
- [ ] Test disaster recovery procedures

## Networking

### Expose API Gateway

**LoadBalancer (cloud providers):**
```yaml
apiVersion: v1
kind: Service
metadata:
  name: api-gateway
spec:
  type: LoadBalancer
  ...
```

**Ingress (recommended):**
```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: crypto-screener
  namespace: crypto-screener
  annotations:
    cert-manager.io/cluster-issuer: letsencrypt-prod
spec:
  tls:
  - hosts:
    - api.crypto-screener.com
    secretName: crypto-screener-tls
  rules:
  - host: api.crypto-screener.com
    http:
      paths:
      - path: /
        pathType: Prefix
        backend:
          service:
            name: api-gateway
            port:
              number: 80
```

## Cost Optimization

### Resource Requests (minimum viable)
```yaml
resources:
  requests:
    memory: "64Mi"  # Start lower, monitor
    cpu: "50m"      # Start lower, scale up
```

### Spot Instances
Use spot/preemptible instances for non-critical workloads:
```yaml
nodeSelector:
  cloud.google.com/gke-preemptible: "true"
tolerations:
- key: cloud.google.com/gke-preemptible
  operator: Equal
  value: "true"
  effect: NoSchedule
```

## Backup & Restore

### NATS JetStream
```bash
# Backup
kubectl exec nats-0 -n crypto-screener -- tar czf /data/backup.tar.gz /data

# Copy to local
kubectl cp crypto-screener/nats-0:/data/backup.tar.gz ./nats-backup.tar.gz
```

### Redis
```bash
# Backup
kubectl exec redis-0 -n crypto-screener -- redis-cli BGSAVE
kubectl cp crypto-screener/redis-0:/data/dump.rdb ./redis-backup.rdb
```

---

**For local development, use docker-compose instead**: `docker-compose up`
