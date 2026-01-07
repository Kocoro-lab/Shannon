# Desktop App Configuration Guide

This guide explains how to configure the Shannon desktop application to connect to your backend services.

## Quick Start

1. **Ensure Backend is Running**
   ```bash
   # From project root
   make dev
   ```
   
   Verify services are up:
   ```bash
   make ps
   ```
   
   You should see the gateway running on port 8080.

2. **Configure Desktop App**
   ```bash
   cd desktop
   cp .env.local.example .env.local
   ```
   
   The default configuration is already set up for local development.

3. **Start Desktop App**
   ```bash
   npm install
   npm run dev
   ```
   
   Open http://localhost:3000

## Configuration File: `.env.local`

The desktop app uses environment variables prefixed with `NEXT_PUBLIC_*` to configure the connection to your Shannon backend.

### Location
```
Shannon/
├── desktop/
│   ├── .env.local          # Your local configuration (git-ignored)
│   └── .env.local.example  # Template with defaults
```

### Required Variables

#### `NEXT_PUBLIC_API_URL`
**Purpose**: Shannon Gateway API endpoint  
**Default**: `http://localhost:8080`  
**Notes**: 
- This is the main entry point for all API calls
- Must match the gateway port in `docker-compose.yml` (port 8080)
- For production, use your deployed backend URL

**Example:**
```bash
# Local development
NEXT_PUBLIC_API_URL=http://localhost:8080

# Production
NEXT_PUBLIC_API_URL=https://api.your-domain.com
```

### Authentication Variables

The desktop app supports three authentication modes:

#### 1. Development Mode (No Auth)
**Variable**: `NEXT_PUBLIC_USER_ID`  
**Purpose**: Bypass authentication for local development  
**Default**: `user_01h0000000000000000000000`

```bash
NEXT_PUBLIC_USER_ID=user_01h0000000000000000000000
```

This user ID matches the seed data in `migrations/postgres/003_authentication.sql`.

**When to use**: Local development and testing

#### 2. API Key Authentication
**Variable**: `NEXT_PUBLIC_API_KEY`  
**Purpose**: Use an API key for authentication  
**Default**: None

```bash
NEXT_PUBLIC_API_KEY=sk_test_123456
```

**When to use**: 
- Production deployments
- Automated testing
- CI/CD pipelines

**How to get an API key**:
1. Log in through the desktop app
2. Navigate to Settings
3. Generate an API key
4. Or use the test key: `sk_test_123456` (created by `make seed-api-key`)

#### 3. JWT Token Authentication
**No configuration needed** - handled automatically by the login flow.

**When to use**: Production with user accounts

### Optional Variables

#### `NEXT_PUBLIC_DEBUG`
**Purpose**: Enable debug logging in browser console  
**Default**: `false`  
**Values**: `true` | `false`

```bash
NEXT_PUBLIC_DEBUG=true
```

## Authentication Priority

The desktop app checks for authentication in this order (see `lib/shannon/api.ts`):

1. **API Key** (`X-API-Key` header)
   - From `NEXT_PUBLIC_API_KEY` environment variable
   - Or from localStorage after login

2. **JWT Token** (`Authorization: Bearer` header)
   - From localStorage after login

3. **User ID** (`X-User-Id` header)
   - From `NEXT_PUBLIC_USER_ID` environment variable
   - Fallback for local development

## Port Configuration

The desktop app connects to these backend services:

| Service | Port | Environment Variable | Notes |
|---------|------|---------------------|-------|
| Gateway | 8080 | `NEXT_PUBLIC_API_URL` | Main API endpoint |
| LLM Service | 8001 | - | Internal (via Gateway) |
| Orchestrator | 50052 | - | Internal (via Gateway) |
| Agent Core | 50051 | - | Internal (via Gateway) |
| Temporal UI | 8088 | - | Direct access (optional) |

**Important**: The desktop app only needs to know about the Gateway (port 8080). All other services are accessed through the Gateway.

## Troubleshooting

### Desktop app can't connect to backend

1. **Check backend is running:**
   ```bash
   make ps
   ```
   
   Look for `shannon-gateway-1` with status "Up" and port `0.0.0.0:8080->8080/tcp`

2. **Verify Gateway URL:**
   ```bash
   curl http://localhost:8080/health
   ```
   
   Should return: `{"status":"ok"}`

3. **Check `.env.local` configuration:**
   ```bash
   cat desktop/.env.local
   ```
   
   Verify `NEXT_PUBLIC_API_URL=http://localhost:8080`

4. **Clear Next.js cache:**
   ```bash
   cd desktop
   rm -rf .next
   npm run dev
   ```

### Authentication errors

1. **For development mode**, ensure you have:
   ```bash
   NEXT_PUBLIC_USER_ID=user_01h0000000000000000000000
   ```

2. **Check if auth is disabled in backend** (`.env` file):
   ```bash
   GATEWAY_SKIP_AUTH=1
   ```

3. **For API key auth**, seed the test API key:
   ```bash
   make seed-api-key
   ```
   
   Then use: `sk_test_123456`

### Port conflicts

If port 8080 is in use:

1. **Change Gateway port** in `docker-compose.yml`:
   ```yaml
   gateway:
     ports:
       - "8081:8080"  # Changed from 8080:8080
   ```

2. **Update desktop config** in `.env.local`:
   ```bash
   NEXT_PUBLIC_API_URL=http://localhost:8081
   ```

3. **Restart services:**
   ```bash
   make down
   make dev
   ```

## Production Deployment

For production deployment:

1. **Remove development variables:**
   ```bash
   # Remove or comment out
   # NEXT_PUBLIC_USER_ID=...
   ```

2. **Set production API URL:**
   ```bash
   NEXT_PUBLIC_API_URL=https://api.your-domain.com
   ```

3. **Disable debug mode:**
   ```bash
   NEXT_PUBLIC_DEBUG=false
   ```

4. **Build the app:**
   ```bash
   npm run build
   # or for native app
   npm run tauri:build
   ```

## Environment Variables Reference

### Complete `.env.local` Example

```bash
# Shannon Desktop App Configuration

# Backend API Configuration
NEXT_PUBLIC_API_URL=http://localhost:8080

# Authentication (choose one)
# Option 1: Development mode (no auth)
NEXT_PUBLIC_USER_ID=user_01h0000000000000000000000

# Option 2: API Key auth
# NEXT_PUBLIC_API_KEY=sk_test_123456

# Optional: Debug mode
NEXT_PUBLIC_DEBUG=true
```

## Related Documentation

- [Desktop App README](README.md) - Main documentation
- [Shannon Backend API](../docs/api-reference.md) - API documentation
- [Authentication Guide](../docs/authentication-and-multitenancy.md) - Backend auth setup
- [Docker Compose Configuration](../deploy/compose/docker-compose.yml) - Service ports

## Support

If you encounter issues:

1. Check the [Troubleshooting](#troubleshooting) section above
2. Review backend logs: `make logs`
3. Check browser console for errors (F12)
4. Verify environment variables are loaded: `console.log(process.env.NEXT_PUBLIC_API_URL)`
