# Tauri DevTools Menu & API Fix

## Summary

Fixed the API mismatch between the frontend and backend that was causing the "Failed to deserialize JSON body: missing field 'prompt'" error, and added a Developer Tools menu item to the Tauri desktop app for easier debugging.

## Changes Made

### 1. Added Developer Tools Menu (Rust/Tauri)

**File: `desktop/src-tauri/src/lib.rs`**

Added a complete application menu with:
- **File Menu**: Quit option
- **Edit Menu**: Standard shortcuts (Undo, Redo, Cut, Copy, Paste, Select All)
- **View Menu**: 
  - **Developer Tools** (Cmd/Ctrl+Shift+I) - Opens browser DevTools
  - Fullscreen toggle

The menu is created using Tauri's menu API and includes keyboard shortcuts. The Developer Tools option calls `window.open_devtools()` when clicked.

### 2. Fixed API Field Mismatch (TypeScript)

**File: `desktop/lib/shannon/api.ts`**

Changed the `TaskSubmitRequest` interface from:
```typescript
export interface TaskSubmitRequest {
    query: string;  // ❌ Backend doesn't recognize this
    session_id?: string;
    context?: Record<string, any>;
    research_strategy?: "quick" | "standard" | "deep" | "academic";
    max_concurrent_agents?: number;
}
```

To:
```typescript
export interface TaskSubmitRequest {
    prompt: string;  // ✅ Matches backend API
    session_id?: string;
    task_type?: string;
    model?: string;
    max_tokens?: number;
    temperature?: number;
    system_prompt?: string;
    tools?: any[];
    metadata?: Record<string, any>;
}
```

### 3. Updated Component API Calls

**File: `desktop/components/chat-input.tsx`**

Changed from:
```typescript
const response = await submitTask({
    query: query.trim(),
    session_id: sessionId,
    context: Object.keys(context).length ? context : undefined,
    research_strategy,
});
```

To:
```typescript
const response = await submitTask({
    prompt: query.trim(),
    session_id: sessionId,
    task_type: selectedAgent === "deep_research" ? "research" : "chat",
    metadata: Object.keys(context).length ? context : undefined,
});
```

**File: `desktop/components/run-dialog.tsx`**

Changed from:
```typescript
const response = await submitTask({
    query: query.trim(),
    research_strategy: "standard",
});
```

To:
```typescript
const response = await submitTask({
    prompt: query.trim(),
    task_type: "chat",
});
```

## Backend API Reference

The backend expects the following structure (from `rust/shannon-api/src/gateway/tasks.rs`):

```rust
pub struct SubmitTaskRequest {
    pub prompt: String,              // Required: Task description
    pub session_id: Option<String>,  // Optional: Session to associate with
    pub task_type: String,           // Default: "chat"
    pub model: Option<String>,       // Optional: Model override
    pub max_tokens: Option<u32>,     // Optional: Token limit
    pub temperature: Option<f32>,    // Optional: Sampling temperature
    pub system_prompt: Option<String>, // Optional: System prompt override
    pub tools: Option<Vec<serde_json::Value>>, // Optional: Available tools
    pub metadata: Option<serde_json::Value>,   // Optional: Additional metadata
}
```

## Testing

1. **Build the Tauri app:**
   ```bash
   cd desktop
   npm run tauri:dev
   ```

2. **Open Developer Tools:**
   - Use menu: View → Developer Tools
   - Or keyboard shortcut: Cmd+Shift+I (macOS) / Ctrl+Shift+I (Windows/Linux)

3. **Test task submission:**
   - Submit a task through the UI
   - Check the Network tab in DevTools to verify the request payload
   - Confirm that `prompt` field is being sent instead of `query`

## Benefits

1. **Developer Tools Access**: Easy debugging of frontend issues with full browser DevTools
2. **API Compatibility**: Frontend now matches backend API contract exactly
3. **Better Error Messages**: Proper field names make debugging easier
4. **Metadata Support**: Research strategy and other context now properly passed via `metadata` field

## Future Improvements

- Add more menu items (Help, Window management, etc.)
- Add keyboard shortcuts for common actions
- Consider adding a "Reload" menu item for development
- Add menu items to toggle different debug modes
