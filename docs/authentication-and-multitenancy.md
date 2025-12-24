# Shannon Authentication & Multi-Tenancy System

## Overview

Shannon implements a **production-ready authentication and multi-tenancy system** that provides enterprise-grade security with complete data isolation between organizations. The system uses JWT tokens for user sessions and API keys for programmatic access, with full tenant isolation across all data stores.

## Architecture

### Authentication Flow
```
User Registration → Tenant Creation → JWT Generation → Authenticated Requests
                          ↓
                    Organization Isolation (Tenant ID)
                          ↓
            Sessions, Tasks, Workflows, Vector Memory
```

### Key Components

1. **JWT Authentication**: Short-lived access tokens (30 min) with refresh tokens (7 days)
2. **API Keys**: Long-lived tokens for programmatic access with configurable scopes
3. **Multi-Tenancy**: Complete isolation between organizations at every layer
4. **Role-Based Access**: Three-tier system (Owner, Admin, User)
5. **Audit Logging**: Comprehensive activity tracking for compliance

## Implementation Status ✅

### Core Authentication
- ✅ Database schema with tenant support
- ✅ JWT token generation and validation
- ✅ API key management system (create, list, revoke, rotate)
- ✅ Password hashing with bcrypt
- ✅ HTTP endpoints (register, login, refresh, me)
- ✅ gRPC middleware enforcement
- ✅ Configuration via shannon.yaml
- ✅ Rate limiting (per-IP sliding window)

### Multi-Tenancy Enforcement
- ✅ **Sessions**: Redis and PostgreSQL filtering by tenant_id
- ✅ **Tasks**: Workflow memo checks prevent cross-tenant access
- ✅ **Vector Memory**: Qdrant queries filtered by tenant metadata
- ✅ **Workflows**: Tenant context threaded through all operations
- ✅ **Database**: tenant_id columns with query filtering

## Quick Start

### 1. Enable Authentication

```yaml
# config/shannon.yaml
auth:
  enabled: true
  skip_auth: false
  jwt_secret: "your-secure-32-character-minimum-secret"
  access_token_expiry: "30m"
  refresh_token_expiry: "168h"
```

Or via environment variables:
```bash
export GATEWAY_SKIP_AUTH=0  # Enable auth (1 = skip, 0 = enforce)
```

### 2. Register a User

```bash
curl -X POST http://localhost:8080/api/v1/auth/register \
  -H "Content-Type: application/json" \
  -d '{
    "email": "user@example.com",
    "username": "johndoe",
    "password": "SecurePass123!",
    "full_name": "John Doe"
  }'
```

Response:
```json
{
  "user_id": "550e8400-e29b-41d4-a716-446655440000",
  "tenant_id": "550e8400-e29b-41d4-a716-446655440001",
  "access_token": "eyJhbGciOiJIUzI1NiIs...",
  "refresh_token": "refresh_token_string",
  "expires_in": 1800,
  "api_key": "sk_live_abc123...",
  "tier": "free",
  "is_new_user": true,
  "quotas": {
    "monthly_tokens": 100000,
    "rate_limit_minute": 60,
    "rate_limit_hour": 1000
  },
  "user": {
    "email": "user@example.com",
    "username": "johndoe",
    "name": "John Doe"
  }
}
```

**Important**: The `api_key` is only returned on registration. Store it securely - it won't be shown again.

### 3. Login

```bash
curl -X POST http://localhost:8080/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{
    "email": "user@example.com",
    "password": "SecurePass123!"
  }'
```

Response:
```json
{
  "user_id": "550e8400-e29b-41d4-a716-446655440000",
  "tenant_id": "550e8400-e29b-41d4-a716-446655440001",
  "access_token": "eyJhbGciOiJIUzI1NiIs...",
  "refresh_token": "refresh_token_string",
  "expires_in": 1800,
  "api_key": "",
  "tier": "free",
  "is_new_user": false,
  "quotas": {...},
  "user": {...}
}
```

### 4. Refresh Access Token

```bash
curl -X POST http://localhost:8080/api/v1/auth/refresh \
  -H "Content-Type: application/json" \
  -d '{
    "refresh_token": "your_refresh_token_here"
  }'
```

Response:
```json
{
  "access_token": "eyJhbGciOiJIUzI1NiIs...",
  "refresh_token": "new_refresh_token",
  "expires_in": 1800
}
```

### 5. Make Authenticated Requests

#### HTTP with API Key (Recommended)
```bash
curl -X POST http://localhost:8080/api/v1/tasks \
  -H "X-API-Key: sk_live_abc123..." \
  -H "Content-Type: application/json" \
  -d '{"query":"Hello Shannon"}'
```

#### HTTP with Bearer Token (API Key)
```bash
curl -X POST http://localhost:8080/api/v1/tasks \
  -H "Authorization: Bearer sk_live_abc123..." \
  -H "Content-Type: application/json" \
  -d '{"query":"Hello Shannon"}'
```

#### gRPC with API Key
```bash
grpcurl -plaintext \
  -H "x-api-key: sk_live_abc123..." \
  -d '{"query":"Hello Shannon"}' \
  localhost:50052 shannon.orchestrator.OrchestratorService/SubmitTask
```

### 6. Get Current User Info

```bash
curl http://localhost:8080/api/v1/auth/me \
  -H "X-API-Key: sk_live_abc123..."
```

Response:
```json
{
  "user_id": "550e8400-e29b-41d4-a716-446655440000",
  "tenant_id": "550e8400-e29b-41d4-a716-446655440001",
  "email": "user@example.com",
  "username": "johndoe",
  "name": "John Doe",
  "tier": "free",
  "quotas": {
    "monthly_tokens": 100000,
    "monthly_usage": 1234,
    "rate_limit_minute": 60,
    "rate_limit_hour": 1000
  },
  "rate_limits": {
    "minute": {"limit": 60, "remaining": 60},
    "hour": {"limit": 1000, "remaining": 1000}
  }
}
```

## Multi-Tenancy Design

### Tenant Isolation Layers

1. **Authentication Layer**
   - Each user belongs to exactly one tenant
   - Tenant ID embedded in JWT claims
   - API keys scoped to tenant

2. **Session Layer**
   - Sessions tagged with tenant_id
   - GetSession enforces tenant match
   - Session queries filtered by tenant
   - Soft delete supported via `deleted_at`/`deleted_by`; gateway filters with `deleted_at IS NULL`

3. **Workflow Layer**
   - Tenant ID passed through workflow input
   - Stored in workflow memo for persistence
   - GetTaskStatus validates tenant ownership

4. **Vector Database Layer**
   - All vectors include tenant_id metadata
   - Queries filtered with Qdrant must conditions
   - Complete memory isolation between tenants

5. **Database Layer**
   - tenant_id columns in tasks and sessions tables
   - WHERE clauses enforce tenant filtering
   - No cross-tenant data access possible

### Security Guarantees

- **No Data Leakage**: Users cannot access other tenants' data even with known IDs
- **Silent Failures**: Cross-tenant access attempts return "not found" (no existence leaks)
- **Complete Isolation**: Every data operation is tenant-scoped
- **Audit Trail**: All access attempts logged for compliance

## Database Schema

```sql
-- Tenants (Organizations)
CREATE TABLE auth.tenants (
    id UUID PRIMARY KEY,
    name VARCHAR(255),
    slug VARCHAR(100) UNIQUE,
    plan VARCHAR(50), -- free, pro, enterprise
    token_limit INTEGER,
    is_active BOOLEAN
);

-- Users
CREATE TABLE auth.users (
    id UUID PRIMARY KEY,
    email VARCHAR(255) UNIQUE,
    username VARCHAR(100) UNIQUE,
    password_hash VARCHAR(255),
    tenant_id UUID REFERENCES auth.tenants(id),
    role VARCHAR(50), -- owner, admin, user
    is_active BOOLEAN
);

-- Sessions with tenant isolation
CREATE TABLE sessions (
    id VARCHAR(255) PRIMARY KEY,
    user_id VARCHAR(255),
    tenant_id UUID,
    created_at TIMESTAMP,
    metadata JSONB
);

-- Task executions with tenant isolation
CREATE TABLE task_executions (
    id UUID PRIMARY KEY,
    workflow_id VARCHAR(255) UNIQUE NOT NULL,
    user_id UUID,
    tenant_id UUID,
    status VARCHAR(50),
    created_at TIMESTAMP
);
```

## API Reference

### Authentication Endpoints (No Auth Required)

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/api/v1/auth/register` | POST | Register new user with email/password |
| `/api/v1/auth/login` | POST | Login and get tokens |
| `/api/v1/auth/refresh` | POST | Refresh access token |

### Protected Endpoints (Auth Required)

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/api/v1/auth/me` | GET | Get current user info |
| `/api/v1/auth/api-keys` | GET | List all API keys for user |
| `/api/v1/auth/api-keys` | POST | Create new API key |
| `/api/v1/auth/api-keys/{id}` | DELETE | Revoke an API key |
| `/api/v1/auth/refresh-key` | POST | Rotate current API key |

### API Key Management

#### List API Keys

```bash
curl http://localhost:8080/api/v1/auth/api-keys \
  -H "X-API-Key: sk_live_abc123..."
```

Response:
```json
{
  "keys": [
    {
      "id": "550e8400-e29b-41d4-a716-446655440000",
      "name": "Default API Key",
      "key_prefix": "sk_live_",
      "description": "Auto-generated on registration",
      "created_at": "2024-01-15T10:30:00Z",
      "last_used_at": "2024-01-15T12:00:00Z",
      "is_active": true
    }
  ],
  "total": 1
}
```

#### Create API Key

```bash
curl -X POST http://localhost:8080/api/v1/auth/api-keys \
  -H "X-API-Key: sk_live_abc123..." \
  -H "Content-Type: application/json" \
  -d '{
    "name": "CI/CD Pipeline",
    "description": "For GitHub Actions"
  }'
```

Response:
```json
{
  "id": "550e8400-e29b-41d4-a716-446655440002",
  "name": "CI/CD Pipeline",
  "api_key": "sk_live_xyz789...",
  "key_prefix": "sk_live_",
  "created_at": "2024-01-15T14:00:00Z",
  "warning": "Store this API key securely. It will not be shown again."
}
```

**Important**: The full API key is only returned once. Store it securely.

#### Revoke API Key

```bash
curl -X DELETE http://localhost:8080/api/v1/auth/api-keys/550e8400-e29b-41d4-a716-446655440002 \
  -H "X-API-Key: sk_live_abc123..."
```

Response:
```json
{
  "success": true,
  "message": "API key revoked successfully"
}
```

#### Rotate Current API Key

Revokes the current API key and generates a new one:

```bash
curl -X POST http://localhost:8080/api/v1/auth/refresh-key \
  -H "X-API-Key: sk_live_abc123..."
```

Response:
```json
{
  "api_key": "sk_live_new123...",
  "previous_key_revoked": true
}
```

### Required Headers

| Service | Header | Format |
|---------|--------|--------|
| gRPC | `x-api-key` | `<api_key>` |
| HTTP | `X-API-Key` | `<api_key>` |
| HTTP | `Authorization` | `Bearer <api_key>` |

**Note**: API keys start with `sk_live_` (production) or `sk_test_` (development).

## Configuration

### Full Configuration Options

```yaml
auth:
  enabled: true                    # Enable authentication system
  skip_auth: false                 # Development mode bypass
  jwt_secret: "32-char-minimum"    # JWT signing secret
  access_token_expiry: "30m"       # Access token lifetime
  refresh_token_expiry: "168h"     # Refresh token lifetime (7 days)
  api_key_rate_limit: 1000         # Requests per hour per API key
  default_tenant_limit: 10000      # Default token limit for new tenants
  enable_registration: true        # Allow self-registration
  require_email_verification: false # Email verification (future)
```

### Environment Variables

```bash
# Override config file settings
export AUTH_ENABLED=true
export AUTH_JWT_SECRET="your-production-secret"
export AUTH_SKIP_AUTH=false
```

## Development & Testing

### Development Mode

For development without authentication:
```yaml
auth:
  skip_auth: true  # Bypasses all auth checks
```

### Testing Multi-Tenancy

```bash
# Register users in different tenants
./scripts/test-multitenancy.sh

# Verify isolation
# 1. User A creates data
# 2. User B cannot access User A's data
# 3. Both users have isolated sessions and memory
```

## Migration Guide

### From Unauthenticated to Authenticated

1. **Run database migration**
```bash
docker exec shannon-postgres-1 psql -U shannon -d shannon \
  -f /migrations/postgres/003_authentication.sql
```

2. **Update configuration**
```yaml
auth:
  enabled: true
  skip_auth: false
  jwt_secret: "production-secret-min-32-chars"
```

3. **Restart services**
```bash
docker compose -f deploy/compose/docker-compose.yml restart orchestrator
```

4. **Create initial admin**
```bash
# Register via API
curl -X POST http://localhost:8080/api/v1/auth/register \
  -H "Content-Type: application/json" \
  -d '{
    "email": "admin@example.com",
    "username": "admin",
    "password": "SecurePass123!",
    "full_name": "Admin User"
  }'
```

## Security Best Practices

1. **JWT Secret Management**
   - Use a cryptographically secure 32+ character secret
   - Store in environment variables or secret manager
   - Rotate periodically

2. **API Key Management**
   - Generate unique keys per application/service
   - Set expiration dates for temporary access
   - Monitor usage through audit logs

3. **Tenant Isolation**
   - Never expose internal tenant IDs to users
   - Use tenant slugs for user-facing identifiers
   - Regular audit of cross-tenant access attempts

4. **Production Deployment**
   - Always use HTTPS/TLS
   - Enable rate limiting
   - Configure CORS appropriately
   - Implement request signing for critical operations

## Troubleshooting

### Common Issues

**"Invalid token" errors**
- Verify JWT secret matches across services
- Check token expiration
- Ensure "Bearer " prefix in header

**"Task not found" for valid task**
- Likely tenant mismatch
- Verify user's tenant_id matches task's tenant_id
- Check workflow memo for tenant context

**Session not persisting**
- Verify Redis connection
- Check session TTL configuration
- Ensure tenant_id is being passed

### Debug Logging

Enable auth debug logging:
```yaml
logging:
  level: debug
  components:
    auth: debug
    session: debug
```

## Comparison with Other Platforms

| Feature | Shannon | LangGraph | Dify | Flowise |
|---------|---------|-----------|------|---------|
| JWT Auth | ✅ | ✅ | ✅ | ✅ |
| API Keys | ✅ | ✅ | ✅ | ✅ |
| Multi-Tenancy | ✅ Full | ⚠️ Partial | ✅ | ⚠️ UI Only |
| Session Isolation | ✅ | ✅ | ✅ | ❌ |
| Vector Isolation | ✅ | ❌ | ⚠️ | ❌ |
| Audit Logging | ✅ | ⚠️ | ✅ | ❌ |

## Rate Limiting

Authentication endpoints are rate-limited to prevent abuse:

| Endpoint | Limit |
|----------|-------|
| `/api/v1/auth/register` | 30 requests/minute per IP |
| `/api/v1/auth/login` | 30 requests/minute per IP |
| `/api/v1/auth/refresh` | 60 requests/minute per IP |

When rate limited, the API returns HTTP 429:
```json
{
  "error": "rate_limit_exceeded",
  "message": "Too many login attempts. Please try again later."
}
```

## Future Enhancements

- [ ] OAuth2 providers (Google, GitHub, Microsoft) - Enterprise only
- [ ] SAML 2.0 for enterprise SSO
- [ ] Two-factor authentication (2FA)
- [x] Refresh token endpoint ✅ Implemented
- [x] API key management (create, list, revoke) ✅ Implemented
- [x] API key rotation ✅ Implemented
- [ ] Admin CLI for user management
- [ ] Tenant usage analytics dashboard
- [ ] Fine-grained permissions (resource-level)
- [ ] Email verification

## Support

For issues or questions:
1. Check audit logs: `docker logs shannon-orchestrator-1`
2. Verify configuration: `cat config/shannon.yaml`
3. Test with curl/grpcurl examples above
4. Open issue on GitHub with error details
