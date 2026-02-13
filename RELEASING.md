# Releasing Shannon

This document describes the release process for Shannon maintainers.

## Overview

Shannon uses GitHub Actions for automated releases. When a git tag matching `v*` is pushed, the [release workflow](.github/workflows/release.yml) builds and publishes:

- **Docker images** (4 services) → Docker Hub (`waylandzhang/*`)
- **Desktop apps** (macOS, Windows, Linux) → GitHub Releases
- **GitHub Release** with auto-generated notes

## Pre-Release Checklist

### 1. Prepare Working Tree

Ensure all changes are committed:

```bash
git status  # Should show clean working tree
```

Verify release-related files are committed:
- `.github/workflows/release.yml`
- `CHANGELOG.md`
- `deploy/compose/docker-compose.release.yml`
- `scripts/install.sh`

### 2. Update Version References

Update version strings in these files:

| File | Field to Update |
|------|-----------------|
| `CHANGELOG.md` | Add `## [X.Y.Z] - YYYY-MM-DD` and update the tag link |
| `README.md` | Version badge + install command tag |
| `scripts/install.sh` | Default `SHANNON_VERSION` |
| `rust/agent-core/Cargo.toml` | `version = "X.Y.Z"` |
| `rust/firecracker-executor/Cargo.toml` | `version = "X.Y.Z"` |
| `python/llm-service/main.py` | FastAPI `version=` + health endpoint version |
| `python/llm-service/llm_service/__init__.py` | `__version__ = "X.Y.Z"` |
| `python/llm-service/llm_service/metrics.py` | Default `version` param |
| `desktop/package.json` | `"version": "X.Y.Z"` |
| `desktop/src-tauri/tauri.conf.json` | `"version": "X.Y.Z"` |
| `desktop/src-tauri/Cargo.toml` | `version = "X.Y.Z"` |
| `docs/agent-core-api.md` | `Current version:` |
| `docs/event-types.md` | `Shannon Version:` |

### 3. Validate Builds Locally

#### Backend Services

```bash
# Proto generation
make proto

# Formatting & linting
make fmt
make lint

# Build + compile checks
make ci

# Unit tests
make test

# Determinism replay (if histories exist)
make ci-replay

# Docker builds (verify Dockerfiles work)
docker build -f rust/agent-core/Dockerfile -t test-agent-core .
docker build -f go/orchestrator/Dockerfile -t test-orchestrator go/orchestrator
docker build -f go/orchestrator/cmd/gateway/Dockerfile -t test-gateway .
docker build -f python/llm-service/Dockerfile -t test-llm-service python/llm-service
```

#### Desktop App

```bash
cd desktop
npm install
npm run build          # Next.js static export
npm run tauri:build    # Native app (requires Rust)
```

#### Smoke Test

```bash
make dev
make smoke
```

### 4. Verify GitHub Secrets

Required secrets in repo settings (`Settings → Secrets → Actions`):

| Secret | Purpose | Required |
|--------|---------|----------|
| `DOCKER_HUB_USERNAME` | Docker Hub login | Yes |
| `DOCKER_HUB_TOKEN` | Docker Hub access token | Yes |
| `TAURI_PRIVATE_KEY` | Desktop app signing | Optional |
| `TAURI_KEY_PASSWORD` | Signing key password | Optional |

### 5. Verify Image Name Consistency

Ensure image names match across:

| Location | Expected Format |
|----------|-----------------|
| `.github/workflows/release.yml` build matrix | `agent-core`, `orchestrator`, `llm-service`, `gateway` |
| `.github/workflows/release.yml` release body | `waylandzhang/{name}:vX.Y.Z` |
| `docker-compose.release.yml` | `waylandzhang/{name}:${VERSION:-latest}` |

## Creating the Release

### 1. Tag and Push

```bash
# Ensure on main branch with clean tree
git checkout main
git pull origin main
git status  # Verify clean

# Create annotated tag
git tag -a vX.Y.Z -m "vX.Y.Z"

# Push commits and tag
git push origin main
git push origin vX.Y.Z
```

### 2. Monitor Release Workflow

1. Go to **Actions** tab on GitHub
2. Watch the **Release** workflow triggered by the tag
3. Verify all jobs pass:
   - `build-and-push` — 4 Docker images (multi-arch)
   - `build-desktop` — macOS, Windows, Linux binaries
   - `create-release` — GitHub Release with artifacts

### 3. Post-Release Verification

#### GitHub Release Assets

Verify the release page shows:
- macOS: `.dmg` and `.app.tar.gz`
- Windows: `.msi` and `.exe`
- Linux: `.AppImage` and `.deb`
- `latest.json` (Tauri auto-update manifest)

#### Docker Images

```bash
docker pull waylandzhang/gateway:vX.Y.Z
docker pull waylandzhang/orchestrator:vX.Y.Z
docker pull waylandzhang/agent-core:vX.Y.Z
docker pull waylandzhang/llm-service:vX.Y.Z
```

#### Install Script

```bash
curl -fsSL https://raw.githubusercontent.com/Kocoro-lab/Shannon/vX.Y.Z/scripts/install.sh | bash
```

## Versioning Strategy

Shannon follows [Semantic Versioning](https://semver.org/):

- **MAJOR** (`X.0.0`): Breaking API changes, incompatible database migrations
- **MINOR** (`X.Y.0`): New features, backwards-compatible changes
- **PATCH** (`X.Y.Z`): Bug fixes, security patches

## Hotfix Releases

For urgent fixes on a released version:

```bash
# Example: hotfix for v1.2.3 -> v1.2.4
git checkout -b hotfix/v1.2.4 v1.2.3

# Apply fix, commit, update version references, then tag
git tag -a v1.2.4 -m "v1.2.4"
git push origin v1.2.4

# Merge back to main
git checkout main
git merge hotfix/v1.2.4
git push origin main
```

## Rollback Procedures

If a release has critical issues:

### Docker Images

The production compose file tags images as `waylandzhang/*:${VERSION:-latest}`.
Pin to a previous release by setting `VERSION`:

```bash
# One-off rollback (repo deploy)
VERSION=vPREVIOUS docker compose -f deploy/compose/docker-compose.release.yml up -d

# One-off rollback (install.sh deploy)
VERSION=vPREVIOUS docker compose -f docker-compose.release.yml up -d

# Persistent pin: add VERSION=vPREVIOUS to the .env used by your deployment, then restart
```

### GitHub Release

1. Edit the release and mark as **pre-release** to hide from latest
2. Update install script default version if needed
3. Communicate via GitHub Discussions or issue

## Release Artifacts

| Artifact | Location | Purpose |
|----------|----------|---------|
| Docker images | Docker Hub | Backend services |
| Desktop binaries | GitHub Releases | End-user apps |
| `latest.json` | GitHub Releases | Tauri auto-update |
| `install.sh` | Repository | One-command deploy |
| `docker-compose.release.yml` | Repository | Production compose |
| `CHANGELOG.md` | Repository | Release notes |

## Troubleshooting

### Release workflow fails

1. Check Actions logs for specific error
2. Common issues:
   - Docker Hub auth expired → regenerate token
   - Tauri build fails → check Rust version compatibility
   - Proto generation fails → ensure `buf` is available

### Images won't push

```bash
# Verify credentials locally
docker login -u waylandzhang
docker push waylandzhang/gateway:test
```

### Desktop build fails on specific platform

Check platform-specific requirements:
- **macOS**: Xcode Command Line Tools, signing certificates
- **Windows**: Microsoft C++ Build Tools
- **Linux**: GTK3, webkit2gtk dependencies

## Quick Reference

```bash
# Pre-release validation
make proto && make fmt && make lint && make ci && make test && make ci-replay

# Local smoke test
make dev && make smoke

# Create and push release
git tag -a vX.Y.Z -m "vX.Y.Z"
git push origin main
git push origin vX.Y.Z

# Verify images
docker pull waylandzhang/gateway:vX.Y.Z && docker images | grep vX.Y.Z
```
