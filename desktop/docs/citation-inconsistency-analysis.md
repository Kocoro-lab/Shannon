# Citation Inconsistency Root Cause Analysis

## Problem Statement

Citations are not consistently rendered across different workflows and execution paths. Historical sessions show citations correctly, but live SSE sessions may or may not display them depending on the workflow type and response format.

## Root Causes

### 1. **Different Workflows Produce Citations Differently**

Different execution patterns generate citations in incompatible formats:

- **DAG/React workflows**: May emit inline markers like `[1]`, `[2]` baked into text
- **Supervisor/Synthesis workflows**: May return structured `citations` metadata without inline markers
- **Simple/forced tool paths**: May return plain text with no markers even when tool outputs exist
- **Mixed formats**: Some synthesizers use `[n]` markers, others return JSON blocks or prose without any references

### 2. **SSE Timing Issues**

Citations metadata only arrives on the **final** SSE event:

- `thread.message.delta` events contain partial text but **no metadata/citations**
- `thread.message.completed` or `LLM_OUTPUT` events contain the full response with citations metadata
- If the UI binds to earlier delta events or intermediate child-agent messages, citations aren't attached yet
- Current implementation creates messages from deltas, then the final result from `task.result` may or may not have markers

### 3. **Bypass Paths Skip Marker Injection**

Single-agent or forced-tool paths may bypass synthesis:

- When synthesis is skipped (e.g., calculator-only task, single-agent response), inline `[n]` markers are never injected
- Metadata may exist in `task.metadata.citations`, but the text doesn't reference them
- UI citation parser requires both markers in text AND citations array to work

### 4. **Text-Metadata Mismatch**

The UI citation rendering logic requires:
- **Inline markers**: Text must contain `[1]`, `[2]`, etc. (detected by regex `/\[(\d+)\]/g`)
- **Citations array**: `message.metadata.citations` must be present
- If either is missing, citations don't render (no auto-fallback to "show as list")

## Current Behavior

### ✅ **Historical Sessions (REST API)**
- Citations loaded from `task.metadata.citations`
- Text already has markers baked in from synthesis
- Citations render correctly as clickable tooltips

### ❌ **Live SSE Sessions**
- Citations arrive on `thread.message.completed` event
- But we create intermediate agent trace messages from these events
- Final answer comes from `fetchFinalOutput()` → `task.result`
- Inconsistent: sometimes has markers, sometimes doesn't, depending on workflow

### ❌ **Simple/Forced Tool Tasks**
- Bypass synthesis entirely
- No markers injected into text
- Metadata may exist but is unused

## Recommended Solutions

### 1. **Server-Side Normalization**

**Ensure final assistant response always includes:**
- Structured `citations` array in metadata
- Inline `[n]` markers injected into text when citations exist
- Deterministic mapping between markers and citations array indices

**Actions:**
- Synthesis step should always run when tool outputs with citations exist
- Single-agent/forced-tool paths should inject markers if citations are present
- Standardize on `[n]` format (1-indexed) for all workflows

### 2. **Frontend Normalization**

**Current implementation:**
```typescript
// ✅ Works for messages with both markers and metadata
<MarkdownWithCitations content={text} citations={metadata?.citations} />

// ❌ Fails silently if markers are missing (even if metadata exists)
```

**Improvements needed:**
- Attach citations from final `thread.message.completed`/`LLM_OUTPUT` event (not deltas)
- Fall back to `task.metadata.citations` after completion
- If citations exist but no markers present, render citations as a list below the content
- Consider adding a "Referenced Sources" section for marker-less responses

### 3. **SSE Event Handling**

**Current flow (problematic):**
1. `thread.message.delta` → Create streaming message (no metadata)
2. `thread.message.completed` → Update with full text + metadata (but might be agent trace)
3. `WORKFLOW_COMPLETED` → Trigger `fetchFinalOutput()`
4. `GET /tasks/{id}` → Get `task.result` (might not have markers)

**Recommended flow:**
1. `thread.message.delta` → Accumulate text for preview (no message creation for final answer)
2. `thread.message.completed` (from synthesis/assistant) → Capture **metadata.citations**
3. `WORKFLOW_COMPLETED` → Fetch `task.result` + ensure metadata is merged
4. Create final message with both text (with markers) and citations metadata

### 4. **Validation & Debugging**

Add logging to detect mismatches:
```typescript
if (metadata?.citations?.length > 0) {
  const hasMarkers = /\[(\d+)\]/.test(content);
  if (!hasMarkers) {
    console.warn("[Citations] Metadata exists but no inline markers found");
    // Fallback: render citations as a list
  }
}
```

## Testing Matrix

| Workflow Type | Has Markers | Has Metadata | Current Result | Expected Result |
|---------------|-------------|--------------|----------------|-----------------|
| Supervisor + Synthesis (historical) | ✅ | ✅ | ✅ Renders | ✅ Renders |
| Supervisor + Synthesis (SSE) | ✅ | ⚠️ Maybe | ⚠️ Inconsistent | ✅ Renders |
| DAG with tools | ⚠️ Maybe | ✅ | ⚠️ Inconsistent | ✅ Renders |
| Simple agent | ❌ | ✅ | ❌ Silent fail | ✅ Renders (as list) |
| Calculator only | ❌ | ❌ | ✅ No citations | ✅ No citations |

## Action Items

**Priority 1 (Server-Side):**
- [ ] Ensure synthesis always runs when tool outputs exist
- [ ] Inject `[n]` markers into final response text when citations metadata is present
- [ ] Standardize citation format across all workflow types

**Priority 2 (Frontend):**
- [ ] Add fallback rendering for citations without markers (show as list)
- [ ] Ensure metadata is properly captured from final SSE event
- [ ] Merge metadata from `thread.message.completed` with `task.result`

**Priority 3 (Observability):**
- [ ] Add warnings when metadata/marker mismatch detected
- [ ] Add telemetry to track citation rendering success rate

## Related Files

- `desktop/components/run-conversation.tsx` - Citation rendering logic
- `desktop/lib/features/runSlice.ts` - SSE event handling, message creation
- `desktop/app/run-detail/page.tsx` - Historical session loading, `fetchFinalOutput()`
- `go/orchestrator/internal/metadata/citations.go` - Server-side citation handling

