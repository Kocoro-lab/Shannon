# Embedded Single-User Mode Fix Plan

## Problem
Frontend is calling `/api/v1/auth/me` (cloud authentication endpoint) in embedded mode, causing "Failed to get current user" errors. The embedded API should work like the original single-user setup without requiring authentication endpoints.

## Analysis

### How It Used To Work (Go Gateway + Python API)
- Single user mode: No authentication required
- API keys stored in environment variables or config
- No `/api/v1/auth/me` endpoint
- No user session management needed

### Current Embedded Mode Issues
1. Frontend calls `getCurrentUser()` → `/api/v1/auth/me` (doesn't exist)
2. Settings page expects user info that isn't needed in embedded mode
3. Auth logic assumes multi-tenant cloud model

## Solution Architecture

### Option 1: Mock `/api/v1/auth/me` Endpoint (RECOMMENDED)
**Approach**: Add minimal auth endpoint that returns default embedded user

**Implementation**:
```rust
// In rust/shannon-api/src/gateway/auth.rs
pub async fn get_current_user_embedded(
    Extension(user): Extension<AuthenticatedUser>,
) -> impl IntoResponse {
    Json(MeResponse {
        user_id: user.user_id,
        tenant_id: "embedded".to_string(),
        email: None,
        username: "Embedded User".to_string(),
        name: Some("Embedded User".to_string()),
        picture: None,
        tier: "unlimited".to_string(),
        quotas: serde_json::json!({}),
        rate_limits: serde_json::json!({}),
    })
}
```

**Pros**:
- Minimal code change
- Frontend works without modification
- Backward compatible

**Cons**:
- Adds endpoint that's only used in embedded mode

### Option 2: Frontend Conditional Logic
**Approach**: Detect Tauri mode and skip `getCurrentUser()` call

**Implementation**:
```typescript
// In desktop/app/(app)/settings/page.tsx
useEffect(() => {
    const isTauri = '__TAURI__' in window;
    
    if (isTauri) {
        // Embedded mode - use default user
        setUserInfo({
            user_id: "embedded_user",
            username: "Embedded User",
            tier: "unlimited",
            // ... defaults
        });
    } else {
        // Cloud mode - fetch real user
        getCurrentUser().then(setUserInfo).catch(console.error);
    }
}, []);
```

**Pros**:
- No backend changes needed
- Clear separation of embedded vs cloud logic

**Cons**:
- Duplicates logic across components
- More complex frontend code

### Option 3: Hybrid Approach (BEST SOLUTION)
**Combine both**: Add endpoint for consistency + frontend detection for optimization

## Recommended Implementation

### Backend: Add `/api/v1/auth/me` for Embedded Mode

**File**: `rust/shannon-api/src/gateway/auth.rs`

Add route handler:
```rust
/// Get current user information (embedded mode).
pub async fn get_current_user_embedded(
    Extension(user): Extension<AuthenticatedUser>,
) -> impl IntoResponse {
    (
        StatusCode::OK,
        Json(serde_json::json!({
            "user_id": user.user_id,
            "tenant_id": "embedded",
            "username": user.user_id,
            "email": null,
            "name": "Embedded User",
            "tier": "unlimited",
            "quotas": {},
            "rate_limits": {}
        }))
    )
}
```

Register route:
```rust
// In router() function
.route("/api/v1/auth/me", get(get_current_user_embedded))
```

### Frontend: Graceful Degradation

**File**: `desktop/app/(app)/settings/page.tsx`

Wrap getCurrentUser() call:
```typescript
useEffect(() => {
    getCurrentUser()
        .then(setUserInfo)
        .catch((err) => {
            console.warn("Failed to get user, using embedded default:", err);
            // Embedded mode fallback
            setUserInfo({
                user_id: "embedded_user",
                username: "Embedded User",
                tier: "unlimited",
                email: null,
                name: null,
                picture: null,
                tenant_id: "embedded",
                quotas: {},
                rate_limits: {},
            });
        });
}, []);
```

## API Key Validation Flow

### Current Implementation
We already have this working correctly:
1. `chat-input.tsx` checks for API keys before submission
2. If no keys, shows toast and blocks submission
3. Toast message guides user to settings

### Verification Needed
Ensure the validation is working:
```typescript
// Should be in chat-input.tsx (already implemented)
const handleSubmit = async () => {
    const apiKeys = await listApiKeys();
    if (apiKeys.length === 0) {
        toast.error("No API keys configured", {
            description: "Configure at least one LLM provider",
            action: {
                label: "Go to Settings",
                onClick: () => router.push("/settings")
            }
        });
        return;
    }
    // ... proceed with submission
};
```

## Implementation Tasks

1. ✅ Add `/api/v1/auth/me` endpoint in embedded mode (5 minutes)
2. ✅ Add graceful fallback in settings page (5 minutes)
3. ✅ Verify API key validation in chat-input (already done)
4. ✅ Test end-to-end flow:
   - Start app → no keys → try to submit → see toast → go to settings → save key → submit works

## Testing Checklist

- [ ] Settings page loads without "Failed to get current user" error
- [ ] User can navigate to API keys tab
- [ ] User can save an API key (OpenAI, Anthropic, etc.)
- [ ] Trying to submit without API key shows toast
- [ ] Toast action navigates to settings
- [ ] After saving API key, task submission works
- [ ] Encrypted keys persist across app restarts

## Files to Modify

1. `rust/shannon-api/src/gateway/auth.rs` - Add get_current_user_embedded()
2. `rust/shannon-api/src/gateway/mod.rs` - Register /api/v1/auth/me route
3. `desktop/app/(app)/settings/page.tsx` - Add error fallback
4. Verify `desktop/components/chat-input.tsx` has validation (should already be there)

## Expected Result

Embedded mode works like original single-user Shannon:
- No authentication required
- Default "embedded_user" for all operations
- API keys managed through settings UI
- Validation prevents submission without keys
- Clean UX with helpful guidance

Total Implementation Time: 15-20 minutes
