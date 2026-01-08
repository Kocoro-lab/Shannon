# .env Configuration Update - Summary & Recommendations

## Executive Summary

Shannon has migrated from a Python LLM service + Go gateway architecture to a unified Rust `shannon-api` service. The `.env` file requires minimal but critical updates to align with this new architecture.

## Critical Changes Required

### 1. Update LLM Service URL (REQUIRED)
**Current (Line 92):**
```bash
LLM_SERVICE_URL=http://llm-service:8000
```

**Updated:**
```bash
LLM_SERVICE_URL=http://shannon-api:8080
```

**Why:** The `agent-core` service uses this URL to call tool execution endpoints. It must point to the new shannon-api service.

**Impact:** Without this change, agent-core cannot execute tools and workflows will fail.

### 2. Remove Deprecated Variables (RECOMMENDED)
**Lines to remove or comment out:**
- Line 14: `SERVICE_NAME=shannon-llm-service` (Python service identifier)
- Line 173: `OTEL_SERVICE_NAME=shannon-llm-service` (Python service telemetry)

**Why:** These variables were specific to the deprecated Python LLM service and are no longer used.

**Impact:** No functional impact, but keeping them causes confusion.

### 3. Add Shannon API Documentation (OPTIONAL)
Add a new section after the LLM Provider API Keys section (around line 43):

```bash
# ----------------------------------------------------------------------------
# Shannon API (Unified Rust Gateway + LLM Service)
# ----------------------------------------------------------------------------
# The shannon-api service combines gateway and LLM functionality in a single
# high-performance Rust service. It runs on port 8080 (main API) and 8082 (admin).
# These variables are optional - defaults are shown below.

# SHANNON_API_HOST=0.0.0.0               # Host to bind to (default: 0.0.0.0)
# SHANNON_API_PORT=8080                  # Main API port (default: 8080)
# SHANNON_API_ADMIN_PORT=8081            # Admin/metrics port (default: 8081, mapped to 8082)
# RUST_LOG=info                          # Logging level (default: info)
```

**Why:** Documents the new service and its configuration options.

**Impact:** Improves clarity for users and developers.

## Desktop App Configuration

✅ **No changes needed** - Both desktop configuration files are already correct:
- [`desktop/.env.local`](../desktop/.env.local): `NEXT_PUBLIC_API_URL=http://localhost:8080`
- [`desktop/.env.production`](../desktop/.env.production): `NEXT_PUBLIC_API_URL=http://localhost:8080`

They correctly point to port 8080, which is now served by shannon-api.

## Architecture Comparison

### Before (Deprecated)
```
Client → Go Gateway (8080) → Python LLM Service (8000) → LLM Providers
                           ↓
                    Go Orchestrator (50052)
                           ↓
                    Rust Agent Core (50051) → Python LLM Service (8000)
```

### After (Current)
```
Client → Rust Shannon API (8080) → LLM Providers
                ↓
         Go Orchestrator (50052)
                ↓
         Rust Agent Core (50051) → Rust Shannon API (8080)
```

## Service Port Reference

| Service | Port | Purpose | Status |
|---------|------|---------|--------|
| shannon-api | 8080 | Main API (Gateway + LLM) | ✅ Active |
| shannon-api | 8082 | Admin/Metrics | ✅ Active |
| orchestrator | 50052 | gRPC | ✅ Active |
| orchestrator | 8081 | Admin/Events | ✅ Active |
| agent-core | 50051 | gRPC | ✅ Active |
| agent-core | 2113 | Metrics | ✅ Active |
| llm-service | 8001 | Legacy Python service | ⚠️ Deprecated |
| gateway | 8083 | Legacy Go gateway | ⚠️ Deprecated |

## Implementation Plan

### Minimal Changes (Required for Tauri Build)
1. Update `LLM_SERVICE_URL` to point to `shannon-api:8080`
2. Verify desktop apps still work (they should, no changes needed)
3. Test that services start correctly

### Recommended Changes (Clean Configuration)
1. Update `LLM_SERVICE_URL` to point to `shannon-api:8080`
2. Remove or comment out `SERVICE_NAME` and `OTEL_SERVICE_NAME`
3. Add Shannon API documentation section
4. Add architecture notes at the top of the file
5. Add legacy service support section at the bottom

### Full Cleanup (Best Practice)
1. All recommended changes above
2. Reorganize sections for better clarity
3. Update comments to reflect new architecture
4. Add migration notes for upgrading users
5. Document which services use which variables

## Testing Checklist

After applying changes:

- [ ] Services start successfully: `make dev` or `docker compose up -d`
- [ ] Shannon-api health check passes: `curl http://localhost:8080/health`
- [ ] Agent-core can reach shannon-api: Check logs for connection errors
- [ ] Desktop app connects successfully: Open app and submit a test task
- [ ] Task execution works: Submit a simple task like "What is 2+2?"
- [ ] Tool execution works: Submit a task that requires web search or calculation

## Rollback Plan

If issues occur after updating:

1. **Revert LLM_SERVICE_URL:**
   ```bash
   LLM_SERVICE_URL=http://llm-service:8000
   ```

2. **Start legacy services:**
   ```bash
   docker compose --profile legacy up -d
   ```

3. **Update desktop app (if needed):**
   ```bash
   # desktop/.env.local
   NEXT_PUBLIC_API_URL=http://localhost:8083
   ```

## Files Provided

1. **[`.env-update-analysis.md`](./plans/.env-update-analysis.md)** - Detailed technical analysis
2. **[`.env-migration-guide.md`](./plans/.env-migration-guide.md)** - Step-by-step migration guide
3. **[`.env-updated-content.md`](./plans/.env-updated-content.md)** - Complete updated .env file content

## Next Steps

### Option 1: Manual Update (Quick)
1. Open `.env` file
2. Change line 92: `LLM_SERVICE_URL=http://shannon-api:8080`
3. Comment out lines 14 and 173
4. Save and restart services

### Option 2: Code Mode (Comprehensive)
1. Switch to Code mode
2. Request to apply the full updated `.env` configuration
3. Code mode can directly edit the file with all improvements

### Option 3: Review First
1. Review the provided documentation
2. Ask clarifying questions
3. Decide on minimal vs. comprehensive update
4. Proceed with chosen approach

## Recommendation

For **Tauri app build purposes**, the minimal change is sufficient:
- Update `LLM_SERVICE_URL=http://shannon-api:8080`

For **production deployments and long-term maintainability**, implement the full cleanup with all documentation improvements.

## Questions to Consider

1. **Do you want to keep legacy service support?**
   - If yes: Keep the legacy section with clear deprecation notices
   - If no: Remove all legacy-related variables

2. **Do you need backward compatibility?**
   - If yes: Keep deprecated variables commented out with migration notes
   - If no: Remove them entirely

3. **What's your deployment timeline?**
   - Immediate: Apply minimal changes only
   - Planned migration: Apply comprehensive updates with documentation

## Support Resources

- **Architecture Documentation**: [`docs/rust-architecture.md`](../docs/rust-architecture.md)
- **Docker Compose**: [`deploy/compose/docker-compose.yml`](../deploy/compose/docker-compose.yml)
- **Shannon API Source**: [`rust/shannon-api/`](../rust/shannon-api/)
- **Agent Core Source**: [`rust/agent-core/`](../rust/agent-core/)

---

**Ready to proceed?** Let me know if you'd like me to switch to Code mode to apply these changes directly to the `.env` file.
