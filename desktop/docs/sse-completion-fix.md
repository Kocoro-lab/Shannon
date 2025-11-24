# SSE Stream Completion Bug Fix

## Problem

During live SSE streaming, when a task completed, the **final assistant response did not appear** until the page was manually refreshed. This affected all responses, including those with citations.

### Symptoms

1. Timeline showed events like "Synthesizing results", "All done", "Stream end" ✅
2. Redux state showed events arriving ✅  
3. **But no assistant message appeared in the conversation** ❌
4. After page refresh, the response appeared (loaded via REST API) ✅

## Root Cause

The backend can send **two different event types** to signal stream completion:

- `"done"` event
- `"STREAM_END"` event

However, the Redux slice (`runSlice.ts`) was only handling the `"done"` event type:

```typescript
if (event.type === "done") {
    state.status = "completed";
    ...
}
```

When the backend sent a `"STREAM_END"` event instead, the frontend never set `runStatus` to `"completed"`, which meant the `fetchFinalOutput()` function in `page.tsx` was never triggered to fetch and display `task.result`.

## Solution

### 1. Handle Both Event Types

Updated `desktop/lib/features/runSlice.ts` to handle both `"done"` and `"STREAM_END"` events:

```typescript
if (event.type === "done" || event.type === "STREAM_END") {
    state.status = "completed";
    console.log("[Redux] Stream ended (event type:", event.type, ")");
    ...
}
```

### 2. Improved Logging

Added detailed logging with emoji markers to make debugging easier:

**In `runSlice.ts`:**
- Log the actual event type received
- Clarify that `fetchFinalOutput` should trigger when no assistant message is found

**In `page.tsx`:**
- Added 🔍 ✓ ➕ ⚠️ ❌ emoji markers to track the flow
- Log message count during completion check
- Log task status, result length, and message dispatch
- Add warning logs for edge cases

## Verification

The fix ensures that:

1. ✅ Both `"done"` and `"STREAM_END"` events trigger completion
2. ✅ `runStatus` is set to `"completed"` reliably  
3. ✅ The `fetchFinalOutput()` useEffect is triggered
4. ✅ `task.result` is fetched from the backend
5. ✅ The final assistant message (with citations and metadata) is added to Redux
6. ✅ The message appears in the conversation immediately

## Testing

To test the fix:

1. Create a new task (especially with Deep Research or web search that includes citations)
2. Watch the console logs during streaming:
   - Should see: `[Redux] Stream ended (event type: STREAM_END)` or `(event type: done)`
   - Should see: `[RunDetail] ✓ Task completed! Fetching authoritative result from task API`
   - Should see: `[RunDetail] ✓ Task fetched - status: TASK_STATUS_COMPLETED`
   - Should see: `[RunDetail] ➕ Adding authoritative result to messages`
   - Should see: `[RunDetail] ✓ Message dispatched to Redux`
3. **The final assistant response should appear immediately without needing to refresh**
4. Citations should be clickable and show tooltips

## Related Files

- `desktop/lib/features/runSlice.ts` - Redux state management for streaming events
- `desktop/app/run-detail/page.tsx` - Main run detail page with completion handling
- `desktop/components/run-conversation.tsx` - Conversation display with citation rendering
- `desktop/docs/desktop-behavior.md` - Documentation of SSE event types

## Background

Per `desktop-behavior.md`:

> Treat `STREAM_END` (SSE name `done`/`STREAM_END`) as the definitive end of the round; `WORKFLOW_COMPLETED` precedes it. On either, stop spinners and optionally poll `GET /api/v1/tasks/{id}` to hydrate final result/metadata.

The backend can send either event type depending on the workflow pattern and configuration.

