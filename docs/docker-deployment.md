# Docker Deployment Guide

This guide explains how to deploy Shannon using pre-built Docker images from GitHub Container Registry.

## Quick Start with Pre-built Images

### Prerequisites

- Docker Engine 20.10+
- Docker Compose 2.0+
- At least 4GB RAM
- 10GB disk space

### 1. Pull Pre-built Images

```bash
# Pull all Shannon images
docker pull ghcr.io/kocoro-lab/shannon-orchestrator:latest
docker pull ghcr.io/kocoro-lab/shannon-agent-core:latest
docker pull ghcr.io/kocoro-lab/shannon-llm-service:latest
docker pull ghcr.io/kocoro-lab/shannon-dashboard:latest
```

### 2. Create Environment File

```bash
# Copy example and edit with your API keys
cp .env.example .env
# Edit .env with your actual API keys
```

### 3. Start Services

```bash
# Using pre-built images
docker compose -f deploy/compose/docker-compose.prebuilt.yml up -d

# Check status
docker compose -f deploy/compose/docker-compose.prebuilt.yml ps

# View logs
docker compose -f deploy/compose/docker-compose.prebuilt.yml logs -f
```

### 4. Verify Deployment

```bash
# Check health endpoints
curl http://localhost:8090/health    # Orchestrator
curl http://localhost:8000/health    # LLM Service

# Access dashboard
open http://localhost:3000

# Access Temporal UI
open http://localhost:8088
```

## Available Images

### Core Services

| Service | Image | Ports | Description |
|---------|-------|-------|-------------|
| Orchestrator | `ghcr.io/kocoro-lab/shannon-orchestrator` | 50052, 2112, 8090 | Go-based workflow orchestration |
| Agent Core | `ghcr.io/kocoro-lab/shannon-agent-core` | 50051, 2113 | Rust-based agent execution engine |
| LLM Service | `ghcr.io/kocoro-lab/shannon-llm-service` | 50053, 8000 | Python LLM provider abstraction |
| Dashboard | `ghcr.io/kocoro-lab/shannon-dashboard` | 3000 | Next.js monitoring dashboard |

### Infrastructure Services

| Service | Image | Ports | Description |
|---------|-------|-------|-------------|
| PostgreSQL | `postgres:16-alpine` | 5432 | Primary database |
| Redis | `redis:7-alpine` | 6379 | Cache & session store |
| Temporal | `temporalio/auto-setup:1.22.4` | 7233, 8088 | Workflow engine |
| Qdrant | `qdrant/qdrant:v1.7.4` | 6333, 6334 | Vector database |

## Image Tags

### Semantic Versioning

```bash
# Specific version
docker pull ghcr.io/kocoro-lab/shannon-orchestrator:v0.1.0

# Major.Minor version (recommended)
docker pull ghcr.io/kocoro-lab/shannon-orchestrator:0.1

# Major version only
docker pull ghcr.io/kocoro-lab/shannon-orchestrator:0

# Latest stable
docker pull ghcr.io/kocoro-lab/shannon-orchestrator:latest
```

### Branch Tags

```bash
# Main branch (cutting edge)
docker pull ghcr.io/kocoro-lab/shannon-orchestrator:main

# Feature branch
docker pull ghcr.io/kocoro-lab/shannon-orchestrator:feat-new-feature

# Commit SHA
docker pull ghcr.io/kocoro-lab/shannon-orchestrator:main-sha-abc1234
```

## Production Deployment

### 1. Use Specific Versions

```yaml
# docker-compose.prod.yml
services:
  orchestrator:
    image: ghcr.io/kocoro-lab/shannon-orchestrator:0.1.5  # Pin version
    
  agent-core:
    image: ghcr.io/kocoro-lab/shannon-agent-core:0.1.5
    
  llm-service:
    image: ghcr.io/kocoro-lab/shannon-llm-service:0.1.5
```

### 2. Resource Limits

```yaml
services:
  orchestrator:
    image: ghcr.io/kocoro-lab/shannon-orchestrator:0.1.5
    deploy:
      resources:
        limits:
          cpus: '2'
          memory: 2G
        reservations:
          cpus: '1'
          memory: 1G
```

### 3. Health Checks

```yaml
services:
  orchestrator:
    image: ghcr.io/kocoro-lab/shannon-orchestrator:0.1.5
    healthcheck:
      test: ["CMD", "grpcurl", "-plaintext", "localhost:50052", "grpc.health.v1.Health/Check"]
      interval: 30s
      timeout: 10s
      retries: 3
      start_period: 40s
```

### 4. Logging

```yaml
services:
  orchestrator:
    image: ghcr.io/kocoro-lab/shannon-orchestrator:0.1.5
    logging:
      driver: "json-file"
      options:
        max-size: "100m"
        max-file: "10"
```

### 5. Restart Policies

```yaml
services:
  orchestrator:
    image: ghcr.io/kocoro-lab/shannon-orchestrator:0.1.5
    restart: unless-stopped
    
  postgres:
    image: postgres:16-alpine
    restart: always
```

## Multi-Architecture Support

All Shannon images support both AMD64 and ARM64 architectures:

```bash
# Auto-detects architecture
docker pull ghcr.io/kocoro-lab/shannon-orchestrator:latest

# Explicit architecture
docker pull --platform linux/amd64 ghcr.io/kocoro-lab/shannon-orchestrator:latest
docker pull --platform linux/arm64 ghcr.io/kocoro-lab/shannon-orchestrator:latest
```

## Security

### Image Scanning

All images are automatically scanned for vulnerabilities using Trivy:

```bash
# Scan image locally
docker run --rm \
  -v /var/run/docker.sock:/var/run/docker.sock \
  aquasec/trivy:latest \
  image ghcr.io/kocoro-lab/shannon-orchestrator:latest
```

### Verify Image Signatures

```bash
# Install cosign
curl -Lo cosign https://github.com/sigstore/cosign/releases/latest/download/cosign-linux-amd64
chmod +x cosign && sudo mv cosign /usr/local/bin/

# Verify image signature
cosign verify ghcr.io/kocoro-lab/shannon-orchestrator:latest \
  --certificate-identity-regexp="https://github.com/Kocoro-lab/Shannon" \
  --certificate-oidc-issuer="https://token.actions.githubusercontent.com"
```

### Use Secrets

```yaml
services:
  llm-service:
    image: ghcr.io/kocoro-lab/shannon-llm-service:latest
    secrets:
      - openai_api_key
    environment:
      OPENAI_API_KEY_FILE: /run/secrets/openai_api_key

secrets:
  openai_api_key:
    file: ./secrets/openai_api_key.txt
```

## Updating Images

### Rolling Updates

```bash
# Pull latest images
docker compose -f deploy/compose/docker-compose.prebuilt.yml pull

# Recreate containers
docker compose -f deploy/compose/docker-compose.prebuilt.yml up -d

# Remove old images
docker image prune -f
```

### Zero-Downtime Updates

```bash
# 1. Scale up with new version
docker compose -f deploy/compose/docker-compose.prebuilt.yml up -d --scale orchestrator=2

# 2. Wait for health checks
sleep 30

# 3. Remove old container
docker compose -f deploy/compose/docker-compose.prebuilt.yml up -d --scale orchestrator=1 --force-recreate
```

## Troubleshooting

### Image Pull Failures

```bash
# Check authentication
echo $GITHUB_TOKEN | docker login ghcr.io -u USERNAME --password-stdin

# Pull with verbose output
docker pull -v ghcr.io/kocoro-lab/shannon-orchestrator:latest
```

### Container Crashes

```bash
# Check logs
docker logs shannon-orchestrator

# Inspect container
docker inspect shannon-orchestrator

# Check resource usage
docker stats shannon-orchestrator
```

### Version Compatibility

Check version compatibility:

```bash
# Orchestrator version
docker run --rm ghcr.io/kocoro-lab/shannon-orchestrator:latest --version

# Agent Core version
docker run --rm ghcr.io/kocoro-lab/shannon-agent-core:latest --version

# LLM Service version
docker run --rm ghcr.io/kocoro-lab/shannon-llm-service:latest python -c "import llm_service; print(llm_service.__version__)"
```

## Kubernetes Deployment

### Helm Chart _(Coming Soon)_

```bash
# Add Shannon Helm repository
helm repo add shannon https://helm.shannon.ai

# Install Shannon
helm install shannon shannon/shannon \
  --namespace shannon \
  --create-namespace \
  --set image.tag=0.1.5
```

### Direct Kubernetes Manifests

```bash
# Apply manifests
kubectl apply -f k8s/

# Check status
kubectl get pods -n shannon
kubectl get svc -n shannon
```

## CI/CD Integration

### GitHub Actions

```yaml
- name: Pull Shannon images
  run: |
    docker pull ghcr.io/kocoro-lab/shannon-orchestrator:${{ github.ref_name }}
    docker pull ghcr.io/kocoro-lab/shannon-agent-core:${{ github.ref_name }}
    docker pull ghcr.io/kocoro-lab/shannon-llm-service:${{ github.ref_name }}
```

### GitLab CI

```yaml
deploy:
  image: docker:latest
  script:
    - docker pull ghcr.io/kocoro-lab/shannon-orchestrator:latest
    - docker compose up -d
```

## Monitoring

### Prometheus Metrics

```yaml
# prometheus.yml
scrape_configs:
  - job_name: 'shannon-orchestrator'
    static_configs:
      - targets: ['localhost:2112']
  
  - job_name: 'shannon-agent-core'
    static_configs:
      - targets: ['localhost:2113']
```

### Grafana Dashboards

Import pre-built dashboards:

```bash
# Import dashboard
curl -X POST http://localhost:3000/api/dashboards/import \
  -H "Content-Type: application/json" \
  -d @observability/grafana/dashboards/shannon-overview.json
```

## Next Steps

- Read the [Environment Configuration Guide](./docs/environment-configuration.md)
- Explore [Template Workflows](./docs/template-workflows.md)
- Check out [Monitoring & Observability](./docs/monitoring.md)
- Learn about [Security Best Practices](./docs/security.md)

---

Need help? Join our [Discord](https://discord.gg/shannon) or open an [issue](https://github.com/Kocoro-lab/Shannon/issues).

