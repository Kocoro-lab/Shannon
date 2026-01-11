# Cloud to Embedded API Migration Guide

**Version**: 1.0  
**Last Updated**: 2026-01-11  
**Audience**: Developers migrating from Shannon Cloud API to Embedded API

---

## Overview

This guide helps you migrate from the Shannon Cloud API (Docker/Kubernetes deployment) to the Shannon Embedded API (Tauri desktop/mobile application).

### Migration Goals

✅ **Zero Code Changes** - Same API interface  
✅ **Local-First** - All data stored locally  
✅ **Encrypted Credentials** - Secure API key storage  
✅ **Full Feature Parity** - Core features identical

---

## Quick Migration Checklist

- [ ] Review [feature parity matrix](#feature-parity-matrix)
- [ ] Install Tauri desktop application
- [ ] Migrate API keys to encrypted local storage
- [ ] Update base URL from cloud to `localhost:8765`
- [ ] Test streaming connections (SSE/WebSocket)
- [ ] Migrate session data (if needed)
- [ ] Update authentication (JWT optional in embedded mode)
- [ ] Test control signals (pause/resume/cancel)
- [ ] Validate event types match cloud API
- [ ] Monitor local resource usage

---

## Feature Parity Matrix

| Feature | Cloud API | Embedded API | Migration Notes |
|---------|-----------|--------------|-----------------|
| **Core Features** ||||
| Task Submission | ✅ | ✅ | Identical API |
| SSE Streaming | ✅ | ✅ | Same event types |
| WebSocket Streaming | ✅ | ✅ | Full support |
| Session Management | ✅ | ✅ | Local SQLite storage |
| Settings API | ✅ | ✅ | Local storage |
| API Key Management | ✅ | ✅ | AES-256-GCM encrypted |
| Control Signals | ✅ | ✅ | Pause/resume/cancel |
| **Authentication** ||||
| JWT Authentication | ✅ Required | ✅ Optional | Defaults to `embedded_user` |
| API Key Auth | ✅ | ✅ | Same mechanism |
| Multi-Tenancy | ✅ | ✅ | Via JWT |
| RBAC | ✅ | ⏳ Planned | Single-user focus |
| **Events** ||||
| LLM Events | ✅ 3 types | ✅ 3 types | Identical |
| Tool Events | ✅ 4 types | ✅ 4 types | Identical |
| Workflow Events | ✅ 3 types | ✅ 3 types | Identical |
| Agent Events | ✅ 3 types | ✅ 3 types | Identical |
| Control Events | ✅ 5 types | ✅ 5 types | Identical |
| Multi-Agent Events | ✅ 6 types | ✅ 6 types | Identical |
| Advanced Events | ✅ 5 types | ✅ 5 types | New in both |
| **Storage** ||||
| Task History | ✅ PostgreSQL | ✅ SQLite | Schema compatible |
| Session Data | ✅ PostgreSQL | ✅ SQLite | Local only |
| API Keys | ✅ Vault/Secrets | ✅ Encrypted DB | AES-256-GCM |
| Vector Memory | ✅ Qdrant | ⏳ Planned | Future feature |
| Cache | ✅ Redis | ✅ In-memory | TTL-based |
| **Advanced Features** ||||
| Scheduled Tasks | ✅ | ⏳ Planned | Cloud-only currently |
| Team Collaboration | ✅ | ❌ | Single-user model |
| Usage Analytics | ✅ Cloud | ✅ Local | No cloud sync |
| Audit Logs | ✅ | ✅ Local | Local file only |
| Custom Tools | ✅ | ✅ | Full support |
| **Deployment** ||||
| Platform | K8s/Docker | Tauri App | Desktop/mobile |
| Networking | Public HTTPS | Localhost | 127.0.0.1 only |
| Scaling | Horizontal | Single instance | N/A |
| Updates | Rolling | App store | Native updates |

---

## Step-by-Step Migration

### Step 1: Install Embedded API

**Download Tauri Application**:

```bash
# macOS
curl -LO https://releases.shannon.ai/shannon-desktop-latest-macos.dmg

# Windows
curl -LO https://releases.shannon.ai/shannon-desktop-latest-windows.exe

# Linux
curl -LO https://releases.shannon.ai/shannon-desktop-latest-linux.AppImage
```

**Or build from source**:

```bash
git clone https://github.com/prometheus/shannon
cd shannon/desktop
npm install
npm run tauri:build
```

---

### Step 2: Update Base URL

**Before (Cloud)**:
```typescript
const BASE_URL = 'https://api.shannon.ai/api/v1';
```

**After (Embedded)**:
```typescript
const BASE_URL = 'http://localhost:8765/api/v1';
```

**Environment-based**:
```typescript
const BASE_URL = process.env.SHANNON_EMBEDDED 
  ? 'http://localhost:8765/api/v1'
  : 'https://api.shannon.ai/api/v1';
```

---

### Step 3: Migrate API Keys

**Cloud Storage** (Vault):
```bash
# Export from cloud
curl -H "Authorization: Bearer $TOKEN" \
  https://api.shannon.ai/api/v1/settings/api-keys
```

**Import to Embedded**:
```typescript
// Set each provider's key
const providers = ['openai', 'anthropic', 'google', 'groq', 'xai'];

for (const provider of providers) {
  await fetch('http://localhost:8765/api/v1/settings/api-keys/' + provider, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      api_key: API_KEYS[provider]
    })
  });
}
```

**Verify encryption**:
```bash
# Check encryption key exists
ls ~/.shannon/encryption.key

# Verify permissions (should be 0600)
ls -l ~/.shannon/encryption.key
```

---

### Step 4: Update Authentication

**Cloud** (JWT Required):
```typescript
const headers = {
  'Authorization': `Bearer ${jwt_token}`,
  'Content-Type': 'application/json'
};
```

**Embedded** (JWT Optional):
```typescript
// Option 1: No auth (defaults to embedded_user)
const headers = {
  'Content-Type': 'application/json'
};

// Option 2: Use JWT for multi-user support
const headers = {
  'Authorization': `Bearer ${jwt_token}`,
  'Content-Type': 'application/json'
};
```

---

### Step 5: Migrate Sessions

**Export from Cloud**:
```bash
curl -H "Authorization: Bearer $TOKEN" \
  https://api.shannon.ai/api/v1/sessions \
  > sessions.json
```

**Import to Embedded**:
```typescript
const sessions = JSON.parse(await fs.readFile('sessions.json'));

for (const session of sessions.sessions) {
  await fetch('http://localhost:8765/api/v1/sessions', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      name: session.name,
      metadata: session.metadata
    })
  });
}
```

---

### Step 6: Update Streaming Code

**No code changes needed!** Both use identical SSE/WebSocket APIs.

**SSE Example** (works on both):
```typescript
const eventSource = new EventSource(
  `${BASE_URL}/tasks/${taskId}/stream`
);

eventSource.addEventListener('thread.message.delta', (event) => {
  const data = JSON.parse(event.data);
  console.log('Delta:', data.content);
});
```

**WebSocket Example** (works on both):
```typescript
const ws = new WebSocket(
  `ws://localhost:8765/api/v1/tasks/${taskId}/stream/ws`
);

ws.onmessage = (event) => {
  const data = JSON.parse(event.data);
  console.log('Event:', data.type, data);
};
```

---

### Step 7: Validate Control Signals

**Test pause/resume/cancel** (identical API):

```typescript
// Pause
await fetch(`${BASE_URL}/tasks/${taskId}/pause`, { method: 'POST' });

// Resume
await fetch(`${BASE_URL}/tasks/${taskId}/resume`, { method: 'POST' });

// Cancel
await fetch(`${BASE_URL}/tasks/${taskId}/cancel`, { method: 'POST' });

// Check state
const state = await fetch(`${BASE_URL}/tasks/${taskId}/control-state`);
const { paused, cancelled } = await state.json();
```

---

## Code Migration Examples

### Example 1: Task Submission

**Before (Cloud)**:
```typescript
const response = await fetch('https://api.shannon.ai/api/v1/tasks', {
  method: 'POST',
  headers: {
    'Authorization': `Bearer ${token}`,
    'Content-Type': 'application/json'
  },
  body: JSON.stringify({
    prompt: 'Explain quantum computing',
    context: {
      model_tier: 'premium',
      research_strategy: 'deep'
    }
  })
});
```

**After (Embedded)**:
```typescript
const response = await fetch('http://localhost:8765/api/v1/tasks', {
  method: 'POST',
  headers: {
    // Authorization optional
    'Content-Type': 'application/json'
  },
  body: JSON.stringify({
    prompt: 'Explain quantum computing',
    context: {
      model_tier: 'premium',
      research_strategy: 'deep'
    }
  })
});
```

**Universal Version**:
```typescript
const BASE_URL = process.env.SHANNON_EMBEDDED 
  ? 'http://localhost:8765/api/v1'
  : 'https://api.shannon.ai/api/v1';

const headers: HeadersInit = {
  'Content-Type': 'application/json'
};

// Add auth only for cloud
if (!process.env.SHANNON_EMBEDDED) {
  headers['Authorization'] = `Bearer ${token}`;
}

const response = await fetch(`${BASE_URL}/tasks`, {
  method: 'POST',
  headers,
  body: JSON.stringify({ /* ... */ })
});
```

---

### Example 2: Settings Management

**Before (Cloud)**:
```typescript
// Set setting (cloud)
await fetch('https://api.shannon.ai/api/v1/settings', {
  method: 'POST',
  headers: {
    'Authorization': `Bearer ${token}`,
    'Content-Type': 'application/json'
  },
  body: JSON.stringify({
    key: 'theme',
    value: 'dark'
  })
});
```

**After (Embedded)** - Identical except URL:
```typescript
// Set setting (embedded)
await fetch('http://localhost:8765/api/v1/settings', {
  method: 'POST',
  headers: {
    'Content-Type': 'application/json'
  },
  body: JSON.stringify({
    key: 'theme',
    value: 'dark'
  })
});
```

---

### Example 3: Event Filtering

**Both Cloud and Embedded** - Identical API:
```typescript
const eventTypes = [
  'thread.message.delta',
  'thread.message.completed',
  'tool.result'
].join(',');

const eventSource = new EventSource(
  `${BASE_URL}/tasks/${taskId}/stream?event_types=${eventTypes}`
);

eventSource.addEventListener('thread.message.delta', handleDelta);
eventSource.addEventListener('thread.message.completed', handleComplete);
eventSource.addEventListener('tool.result', handleToolResult);
```

---

## Data Migration

### Export Cloud Data

```bash
#!/bin/bash
# export_cloud_data.sh

TOKEN="your-jwt-token"
CLOUD_URL="https://api.shannon.ai/api/v1"

# Export sessions
curl -H "Authorization: Bearer $TOKEN" \
  $CLOUD_URL/sessions > sessions.json

# Export settings
curl -H "Authorization: Bearer $TOKEN" \
  $CLOUD_URL/settings > settings.json

# Note: API keys cannot be exported (encrypted in Vault)
# You must re-enter them in embedded mode
```

### Import to Embedded

```typescript
// import_to_embedded.ts
import { readFile } from 'fs/promises';

const BASE_URL = 'http://localhost:8765/api/v1';

async function importData() {
  // Import sessions
  const sessions = JSON.parse(await readFile('sessions.json', 'utf-8'));
  for (const session of sessions.sessions) {
    await fetch(`${BASE_URL}/sessions`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        name: session.name,
        metadata: session.metadata
      })
    });
  }

  // Import settings
  const settings = JSON.parse(await readFile('settings.json', 'utf-8'));
  for (const setting of settings.settings) {
    await fetch(`${BASE_URL}/settings`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        key: setting.key,
        value: setting.value
      })
    });
  }

  console.log('Data import complete!');
}

importData();
```

---

## Configuration Differences

### Cloud Configuration

```yaml
# config/shannon.yaml (Cloud)
deployment:
  mode: cloud
  database:
    url: postgresql://user:pass@postgres:5432/shannon
  redis:
    url: redis://redis:6379
  qdrant:
    url: http://qdrant:6333

gateway:
  host: 0.0.0.0
  port: 8080
  jwt_secret: ${JWT_SECRET}
  require_auth: true

providers:
  openai:
    api_key: ${OPENAI_API_KEY}
```

### Embedded Configuration

```yaml
# config/shannon.yaml (Embedded)
deployment:
  mode: embedded
  database:
    path: ~/.shannon/shannon.db
  cache:
    type: in_memory
    ttl_seconds: 3600

gateway:
  host: 127.0.0.1
  port: 8765
  jwt_secret: ${JWT_SECRET}  # Optional
  require_auth: false  # Optional for single-user

providers:
  # API keys stored encrypted in database
  # Configure via /api/v1/settings/api-keys endpoint
```

---

## Performance Considerations

### Cloud API Performance

- **Latency**: 50-200ms (network + processing)
- **Throughput**: 1000+ req/sec (horizontal scaling)
- **Memory**: 200MB+ per pod
- **Storage**: Unlimited (PostgreSQL)

### Embedded API Performance

- **Latency**: 5-20ms (local processing only)
- **Throughput**: 10,000+ req/sec (single instance)
- **Memory**: ~50MB (optimized Rust binary)
- **Storage**: Limited by disk (SQLite)

### Optimization Tips

1. **Reduce network calls**: Batch operations where possible
2. **Use local cache**: In-memory caching for frequent reads
3. **Optimize queries**: SQLite indexes for common queries
4. **Prune old data**: Implement retention policy for sessions
5. **Monitor memory**: Watch event buffer sizes

---

## Security Differences

### Cloud Security

- ✅ TLS/HTTPS required
- ✅ JWT with RBAC
- ✅ API keys in Vault
- ✅ Network isolation
- ✅ DDoS protection
- ✅ Audit logs to SIEM

### Embedded Security

- ✅ Localhost only (127.0.0.1)
- ✅ Optional JWT
- ✅ AES-256-GCM encrypted API keys
- ✅ Secure key storage (0600 permissions)
- ✅ Local audit logs
- ⚠️ Physical access = full access

### Best Practices

1. **Encrypt disk**: Use full-disk encryption (FileVault, BitLocker, LUKS)
2. **Screen lock**: Enable auto-lock after inactivity
3. **Backups**: Encrypt backups of `~/.shannon/`
4. **Updates**: Keep Tauri app updated
5. **API key rotation**: Rotate provider keys periodically

---

## Troubleshooting

### Connection Issues

**Problem**: Cannot connect to embedded API

**Solutions**:
```bash
# Check if API is running
curl http://localhost:8765/health

# Check port is not in use
lsof -i :8765  # macOS/Linux
netstat -ano | findstr :8765  # Windows

# Restart Tauri app
# Kill and restart the application
```

---

### Data Migration Issues

**Problem**: Sessions not importing correctly

**Solutions**:
1. Check JSON format matches expected schema
2. Verify embedded API is running
3. Check for duplicate session IDs
4. Review error logs

```bash
# View embedded API logs (Tauri)
tail -f ~/.shannon/logs/shannon-api.log
```

---

### Performance Issues

**Problem**: Embedded API slower than expected

**Solutions**:
1. Check SQLite database size: `ls -lh ~/.shannon/shannon.db`
2. Vacuum database: `sqlite3 ~/.shannon/shannon.db "VACUUM;"`
3. Optimize indexes: Review query plans
4. Clear old sessions: Implement retention policy
5. Monitor memory: Use Activity Monitor / Task Manager

---

## Testing Migration

### Validation Checklist

- [ ] Health check responds: `GET /health`
- [ ] API info correct: `GET /api/v1/info`
- [ ] Capabilities accurate: `GET /api/v1/capabilities`
- [ ] API keys encrypted: Check `~/.shannon/encryption.key`
- [ ] Settings CRUD works: Test all endpoints
- [ ] Task submission works: Submit test task
- [ ] SSE streaming works: Stream task events
- [ ] WebSocket works: Connect and receive events
- [ ] Control signals work: Pause/resume/cancel
- [ ] Sessions work: Create/list/get
- [ ] Multi-agent events: Verify event types
- [ ] Advanced events: Check all 5 types

### Integration Test

```typescript
// test_migration.ts
async function testMigration() {
  const BASE_URL = 'http://localhost:8765/api/v1';
  
  // 1. Health check
  const health = await fetch('http://localhost:8765/health');
  console.assert((await health.json()).status === 'healthy');
  
  // 2. Set API key
  const setKey = await fetch(`${BASE_URL}/settings/api-keys/openai`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ api_key: 'sk-test123' })
  });
  console.assert(setKey.ok);
  
  // 3. Submit task
  const task = await fetch(`${BASE_URL}/tasks`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      prompt: 'Test task',
      context: { model_tier: 'basic' }
    })
  });
  const { task_id } = await task.json();
  console.assert(task_id);
  
  // 4. Stream events
  const eventSource = new EventSource(`${BASE_URL}/tasks/${task_id}/stream`);
  let receivedEvent = false;
  
  eventSource.addEventListener('workflow.started', () => {
    receivedEvent = true;
    eventSource.close();
  });
  
  setTimeout(() => {
    console.assert(receivedEvent, 'Did not receive workflow.started event');
    console.log('✅ Migration test passed!');
  }, 5000);
}

testMigration();
```

---

## Rollback Plan

If migration fails, you can continue using cloud API:

1. **Keep cloud API running**: No need to decommission immediately
2. **Dual mode**: Support both embedded and cloud in your app
3. **Feature flags**: Toggle between embedded and cloud
4. **Gradual migration**: Migrate users in batches

```typescript
// Dual-mode support
const config = {
  embedded: {
    enabled: process.env.ENABLE_EMBEDDED === 'true',
    baseUrl: 'http://localhost:8765/api/v1'
  },
  cloud: {
    enabled: true,
    baseUrl: 'https://api.shannon.ai/api/v1'
  }
};

const baseUrl = config.embedded.enabled 
  ? config.embedded.baseUrl 
  : config.cloud.baseUrl;
```

---

## FAQ

**Q: Can I run both cloud and embedded simultaneously?**  
A: Yes! They're independent. Use different task IDs to avoid conflicts.

**Q: Will my cloud data auto-sync to embedded?**  
A: No. You must manually export/import. No automatic sync.

**Q: Are event types identical between cloud and embedded?**  
A: Yes! All 26+ event types are identical.

**Q: Can I migrate back to cloud later?**  
A: Yes. Export from embedded and import to cloud.

**Q: What about scheduled tasks?**  
A: Not yet supported in embedded. Coming in future release.

**Q: How do I update the embedded API?**  
A: Update the Tauri app via app store or download new version.

**Q: Is embedded mode production-ready?**  
A: Yes! Full feature parity with cloud for core features.

**Q: What's the performance difference?**  
A: Embedded is 10-20x lower latency (local vs network).

---

## Next Steps

1. ✅ Read [Embedded API Reference](./embedded-api-reference.md)
2. ✅ Review [Feature Parity Spec](../specs/embedded-feature-parity-spec.md)
3. ✅ Test migration in development environment
4. ✅ Migrate API keys to encrypted storage
5. ✅ Update application code (mostly BASE_URL changes)
6. ✅ Deploy to production users

---

## Support

Need help with migration?

- **Documentation**: <https://docs.shannon.ai/embedded>
- **Migration Guide**: This document
- **API Reference**: [`embedded-api-reference.md`](./embedded-api-reference.md)
- **Issues**: <https://github.com/prometheus/shannon/issues>
- **Discord**: <https://discord.gg/shannon> (#migration channel)

---

**Last Updated**: 2026-01-11 | **Version**: 1.0 | **Status**: Production Ready
