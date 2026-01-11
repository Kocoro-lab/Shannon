# API Completion & Settings Management Plan

## Overview
This plan addresses critical missing features in the Shannon desktop application:
1. Control-state endpoint implementation
2. Button functionality verification (Retry Stream, Fetch Final Output)
3. API key management system
4. Settings infrastructure

## 1. Control-State Endpoint

### Purpose
The `/api/v1/tasks/{task_id}/control-state` endpoint provides real-time workflow control state information, essential for tracking pause/cancel status independently of the database workflow status.

### Expected Response Structure
```rust
pub struct ControlStateResponse {
    pub is_paused: bool,
    pub is_cancelled: bool,
    pub paused_at: Option<String>,  // ISO 8601 timestamp
    pub pause_reason: Option<String>,
    pub paused_by: Option<String>,
    pub cancel_reason: Option<String>,
    pub cancelled_by: Option<String>,
}
```

### Implementation Strategy

#### For Embedded Mode (Durable Workflows)
Since embedded mode uses the Durable workflow engine (not Temporal), we need to:

1. **Add control state to workflow execution context**
   - Store in `durable-shannon` workflow state
   - Update on pause/resume/cancel operations

2. **Implement in-memory state tracking**
   - Map of `workflow_id -> ControlState`
   - Updated by workflow engine during execution
   - Queried by REST API endpoint

#### For Cloud Mode (Temporal)
- Query Temporal's workflow control state via gRPC
- Use `control_state_v1` query handler

### Database Schema Addition
```sql
CREATE TABLE IF NOT EXISTS workflow_control_state (
    workflow_id TEXT PRIMARY KEY,
    is_paused BOOLEAN NOT NULL DEFAULT false,
    is_cancelled BOOLEAN NOT NULL DEFAULT false,
    paused_at TEXT,
    pause_reason TEXT,
    paused_by TEXT,
    cancel_reason TEXT,
    cancelled_by TEXT,
    updated_at TEXT NOT NULL
);
```

### API Route Implementation
Location: `rust/shannon-api/src/gateway/tasks.rs`

```rust
pub async fn get_task_control_state(
    State(state): State<AppState>,
    Path(task_id): Path<String>,
) -> Result<Json<ControlStateResponse>, (StatusCode, Json<ErrorResponse>)>
```

## 2. Button Functionality Verification

### Retry Stream Button
**Current Implementation**: `desktop/app/(app)/run-detail/page.tsx`

Expected behavior:
1. Close existing EventSource connection
2. Increment `streamRestartKey` state
3. Trigger `useRunStream` hook to reconnect
4. Clear any error states

**Verification Steps**:
- Check that `handleRetryStream` properly increments restart key
- Ensure `useRunStream` dependency array includes restart key
- Confirm EventSource cleanup in effect cleanup

### Fetch Final Output Button
**Current Implementation**: `desktop/app/(app)/run-detail/page.tsx`

Expected behavior:
1. Call `/api/v1/tasks/{task_id}/output` endpoint
2. Update Redux state with final output
3. Mark run as complete
4. Close stream connection

**Verification Steps**:
- Check endpoint exists in Shannon API
- Verify Redux action dispatching
- Confirm UI updates after fetch

### Missing Endpoint: Task Output
Need to implement: `GET /api/v1/tasks/{task_id}/output`

Response:
```rust
pub struct TaskOutputResponse {
    pub task_id: String,
    pub output: String,
    pub status: String,
    pub tokens_used: i64,
    pub cost_usd: f64,
    pub completed_at: Option<String>,
}
```

## 3. Settings Infrastructure

### Database Schema (Hybrid Backend)

#### User Settings Table
```sql
CREATE TABLE IF NOT EXISTS user_settings (
    user_id TEXT NOT NULL DEFAULT 'embedded_user',
    setting_key TEXT NOT NULL,
    setting_value TEXT NOT NULL,
    setting_type TEXT NOT NULL, -- 'string', 'number', 'boolean', 'json'
    encrypted BOOLEAN NOT NULL DEFAULT false,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    PRIMARY KEY (user_id, setting_key)
);

CREATE INDEX IF NOT EXISTS idx_user_settings_user_id 
    ON user_settings(user_id);
```

#### API Keys Table (Encrypted Storage)
```sql
CREATE TABLE IF NOT EXISTS api_keys (
    user_id TEXT NOT NULL DEFAULT 'embedded_user',
    provider TEXT NOT NULL, -- 'openai', 'anthropic', 'google', etc.
    api_key TEXT NOT NULL, -- Encrypted
    is_active BOOLEAN NOT NULL DEFAULT true,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    last_used_at TEXT,
    PRIMARY KEY (user_id, provider)
);

CREATE INDEX IF NOT EXISTS idx_api_keys_user_id 
    ON api_keys(user_id);
CREATE INDEX IF NOT EXISTS idx_api_keys_active 
    ON api_keys(user_id, is_active);
```

### API Routes

#### Settings Management
```
GET    /api/v1/settings              - Get all settings
GET    /api/v1/settings/{key}        - Get specific setting
POST   /api/v1/settings              - Create/update setting
DELETE /api/v1/settings/{key}        - Delete setting
```

#### API Key Management
```
GET    /api/v1/settings/api-keys              - List configured providers
GET    /api/v1/settings/api-keys/{provider}   - Check if key exists (masked)
POST   /api/v1/settings/api-keys/{provider}   - Set/update API key
DELETE /api/v1/settings/api-keys/{provider}   - Delete API key
POST   /api/v1/settings/api-keys/validate     - Validate API key with provider
```

### Security Considerations

#### Encryption
For embedded mode, we'll use a simple encryption scheme:
1. Generate or load a local encryption key (stored in app data directory)
2. Use AES-256-GCM for API key encryption
3. Store only encrypted values in SQLite

Implementation crate: `aes-gcm = "0.10"`

#### API Key Masking
When returning API key status to frontend:
- Never return the full key
- Return masked version: `sk-...xyz` (first 3 + last 3 chars)
- Include `is_configured: bool` flag

## 4. Task Submission Flow with API Key Validation

### Frontend Flow
Location: `desktop/app/(app)/run-detail/page.tsx`

```typescript
async function handleSubmitTask(prompt: string) {
  // 1. Check if any API key is configured
  const apiKeysStatus = await fetch('/api/v1/settings/api-keys');
  const { providers } = await apiKeysStatus.json();
  
  if (providers.length === 0) {
    // 2. Show toast notification
    toast.error('No API keys configured', {
      description: 'Please configure at least one LLM provider API key',
      action: {
        label: 'Go to Settings',
        onClick: () => router.push('/settings')
      }
    });
    return;
  }
  
  // 3. Proceed with task submission
  const response = await submitTask({ prompt, session_id });
  // ...
}
```

### Toast Component
Use `sonner` library (already in dependencies):
```typescript
import { toast } from 'sonner';
```

### Settings Page Enhancement
Location: `desktop/app/(app)/settings/page.tsx`

Add sections for:
1. **API Keys Tab**
   - OpenAI API Key
   - Anthropic API Key
   - Google API Key
   - Groq API Key
   - xAI API Key

2. **General Settings Tab**
   - Default model
   - Max tokens
   - Temperature
   - Theme preferences

## 5. Implementation Order

### Phase 1: Database & Backend (Rust)
1. ✅ Add control_state table migration to hybrid backend
2. ✅ Add user_settings and api_keys tables
3. ✅ Implement encryption utilities for API keys
4. ✅ Implement settings repository in `rust/shannon-api/src/database/`
5. ✅ Create settings API routes
6. ✅ Implement control-state endpoint
7. ✅ Implement task output endpoint

### Phase 2: Frontend (TypeScript)
1. ✅ Verify Retry Stream button functionality
2. ✅ Verify Fetch Final Output button functionality
3. ✅ Implement API key status check before task submission
4. ✅ Add toast notification for missing API keys
5. ✅ Enhance settings page with API key management
6. ✅ Test end-to-end flow

### Phase 3: Testing & Validation
1. ✅ Test control-state endpoint in embedded mode
2. ✅ Test API key encryption/decryption
3. ✅ Test task submission flow with/without keys
4. ✅ Test settings persistence across app restarts
5. ✅ Test button functionality (retry/fetch)

## 6. Files to Modify/Create

### Rust Backend
```
rust/durable-shannon/src/backends/
  └── control_state.rs          [CREATE]

rust/shannon-api/src/database/
  ├── hybrid.rs                  [MODIFY] - Add migrations
  ├── settings.rs                [CREATE] - Settings repository
  └── encryption.rs              [CREATE] - API key encryption

rust/shannon-api/src/gateway/
  ├── tasks.rs                   [MODIFY] - Add endpoints
  └── settings.rs                [CREATE] - Settings routes

rust/shannon-api/src/domain/
  └── settings.rs                [CREATE] - Domain types
```

### Frontend
```
desktop/lib/shannon/
  ├── api.ts                     [MODIFY] - Add settings endpoints
  └── settings.ts                [CREATE] - Settings API calls

desktop/app/(app)/
  ├── run-detail/page.tsx        [MODIFY] - Add validation
  └── settings/
      └── api-keys/page.tsx      [CREATE] - API key management

desktop/components/
  └── api-key-input.tsx          [CREATE] - Secure input component
```

## 7. Configuration

### Environment Variables
For local encryption key:
```bash
# Auto-generated on first run if not present
SHANNON_ENCRYPTION_KEY=<base64-encoded-32-byte-key>
```

### App Config Addition
In `rust/shannon-api/src/config/mod.rs`:
```rust
pub struct SecurityConfig {
    pub encryption_key_path: PathBuf,
    pub api_key_mask_length: usize,  // Default: 3
}
```

## Success Criteria

1. ✅ Control-state endpoint returns correct pause/cancel status
2. ✅ Retry Stream button reconnects SSE and clears errors
3. ✅ Fetch Final Output button retrieves and displays final result
4. ✅ API keys stored encrypted in SQLite
5. ✅ Task submission blocked without API keys
6. ✅ Toast notification shown with navigation to settings
7. ✅ Settings persist across app restarts
8. ✅ API key validation with provider APIs works

## Timeline Estimate

- Phase 1 (Backend): 4-6 hours
- Phase 2 (Frontend): 3-4 hours
- Phase 3 (Testing): 2-3 hours
- **Total**: ~10-13 hours of focused development

## Notes

- For embedded mode, we don't have Temporal's query system, so control state must be tracked in our database
- Encryption key should be generated once and stored securely in app data directory
- API key validation should be async and non-blocking
- Consider rate limiting for API key validation endpoints to prevent abuse
