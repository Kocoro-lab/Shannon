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
- ✅ API key management system
- ✅ Password hashing with bcrypt
- ✅ HTTP endpoints (register/login)
- ✅ gRPC middleware enforcement
- ✅ Configuration via shannon.yaml

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

### 2. Register a User

```bash
curl -X POST http://localhost:8081/api/auth/register \
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
  "user": {
    "id": "user-uuid",
    "email": "user@example.com",
    "username": "johndoe",
    "tenant_id": "tenant-uuid",
    "role": "user"
  }
}
```

### 3. Login

```bash
curl -X POST http://localhost:8081/api/auth/login \
  -H "Content-Type: application/json" \
  -d '{
    "email": "user@example.com",
    "password": "SecurePass123!"
  }'
```

Response:
```json
{
  "access_token": "eyJhbGciOiJIUzI1NiIs...",
  "refresh_token": "refresh_token_string",
  "token_type": "Bearer",
  "expires_in": 1800
}
```

### 4. Make Authenticated Requests

#### gRPC with JWT
```bash
grpcurl -plaintext \
  -H "authorization: Bearer <access_token>" \
  -d '{"query":"Hello Shannon"}' \
  localhost:50052 shannon.orchestrator.OrchestratorService/SubmitTask
```

#### HTTP with API Key (future)
```bash
curl -H "X-API-Key: sk_your_api_key" \
  http://localhost:8081/api/tasks
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

-- Tasks with tenant isolation
CREATE TABLE tasks (
    workflow_id VARCHAR(255) PRIMARY KEY,
    user_id VARCHAR(255),
    tenant_id UUID,
    status VARCHAR(50),
    created_at TIMESTAMP
);
```

## API Reference

### Authentication Endpoints

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/api/auth/register` | POST | Register new user |
| `/api/auth/login` | POST | Login and get tokens |
| `/api/auth/refresh` | POST | Refresh access token (TODO) |
| `/api/auth/logout` | POST | Revoke tokens (TODO) |

### Required Headers

| Service | Header | Format |
|---------|--------|--------|
| gRPC | `authorization` | `Bearer <jwt_token>` |
| gRPC | `x-api-key` | `<api_key>` |
| HTTP | `Authorization` | `Bearer <jwt_token>` |
| HTTP | `X-API-Key` | `<api_key>` |

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
  -f /docker-entrypoint-initdb.d/003_authentication.sql
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
docker compose -f deploy/compose/compose.yml restart orchestrator
```

4. **Create initial admin**
```bash
# Use default admin or create via API
curl -X POST http://localhost:8081/api/auth/register ...
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

## Future Enhancements

- [ ] OAuth2 providers (Google, GitHub, Microsoft)
- [ ] SAML 2.0 for enterprise SSO
- [ ] Two-factor authentication (2FA)
- [ ] Refresh token endpoint
- [ ] Admin CLI for user management
- [ ] Tenant usage analytics dashboard
- [ ] Fine-grained permissions (resource-level)
- [ ] API key rotation policies

## Support

For issues or questions:
1. Check audit logs: `docker logs shannon-orchestrator-1`
2. Verify configuration: `cat config/shannon.yaml`
3. Test with curl/grpcurl examples above
4. Open issue on GitHub with error details