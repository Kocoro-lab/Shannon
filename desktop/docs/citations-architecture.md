# Citations Architecture & Consistency

## Overview

Citations in Shannon allow users to trace LLM responses back to their source documents (web pages, tool outputs, etc.). However, citation rendering has inconsistencies across different workflow types and execution paths due to varying formats produced by different agents and synthesis strategies.

## Architecture

### Citation Data Structure

Citations consist of two parts that must work together:

1. **Inline Markers**: Numbered references in the text, e.g., `[1]`, `[2]`, `[3]`
2. **Metadata Array**: Structured citation objects containing URLs, titles, snippets

```json
{
  "content": "According to the documentation [1], the API supports...",
  "metadata": {
    "citations": [
      {
        "url": "https://example.com/docs",
        "title": "API Documentation",
        "snippet": "The API supports REST endpoints..."
      }
    ]
  }
}
```

### Data Flow

#### Historical Sessions (REST API)
```
GET /api/v1/sessions/{id}/history
  → Session history with turns
    → Each turn has final_output (text with markers)
    → Each turn's task has metadata.citations
      → Frontend loads both, renders inline tooltips
```

#### Live SSE Sessions
```
POST /api/v1/tasks → workflow_id
  ↓
SSE /api/v1/tasks/{id}/stream
  ↓
1. thread.message.delta (partial text, no metadata)
2. thread.message.delta (more text, no metadata)
3. thread.message.completed (full text + metadata.citations)
   OR
   LLM_OUTPUT (full text + metadata)
  ↓
4. WORKFLOW_COMPLETED
  ↓
5. GET /api/v1/tasks/{id} → task.result + task.metadata.citations
```

**Problem**: Steps 3 and 5 may have inconsistent formats depending on workflow type.

## Root Cause: Workflow Variability

Different workflows produce citations in different formats:

### Supervisor + Synthesis (Multi-Agent)
- **Subtasks**: Individual agents use tools, produce outputs
- **Synthesis**: Combines outputs, should inject `[n]` markers and populate metadata
- **Current behavior**: Inconsistent - sometimes markers are added, sometimes not
- **SSE timing**: Citations appear on synthesis agent's `thread.message.completed`

### DAG Workflow
- **Nodes**: Each node may produce tool outputs
- **Aggregation**: Final node collects results
- **Current behavior**: May emit markers inline or structured metadata, but not always both
- **SSE timing**: Citations may appear on individual node completions or final aggregation

### React/Agent-Core (Simple Agent)
- **Single agent**: Uses tools directly, produces response
- **Current behavior**: May return plain text without markers even when tool outputs exist
- **SSE timing**: Citations appear on `LLM_OUTPUT` or `thread.message.completed`

### Forced/Bypass Paths
- **Direct tool calls**: Calculator, search-only tasks
- **Current behavior**: No synthesis step, no marker injection
- **Result**: Metadata exists but text doesn't reference it

## Problem Manifestations

### 1. **Missing Inline Markers**

**Symptom**: Citations metadata exists but no `[1]`, `[2]` in text

**Cause**: 
- Synthesis skipped or didn't inject markers
- Single-agent response with no marker injection
- Tool-only path bypassed synthesis

**Impact**: UI parser finds no markers, citations don't render

### 2. **Missing Metadata**

**Symptom**: Text has `[1]`, `[2]` markers but no `metadata.citations` array

**Cause**:
- Markers manually added by agent but citations not propagated
- Metadata lost during event transformation
- Final `task.result` doesn't include metadata

**Impact**: Clicking markers has no tooltip/link

### 3. **SSE Timing Issues**

**Symptom**: Historical sessions show citations, live SSE sessions don't

**Cause**:
- Frontend creates message from early `delta` events (no metadata yet)
- Final `thread.message.completed` metadata not attached to Redux message
- `fetchFinalOutput()` gets `task.result` without metadata

**Impact**: Live users see no citations, but refreshing page shows them

### 4. **Index Misalignment**

**Symptom**: Clicking `[2]` shows wrong citation or error

**Cause**:
- Markers are 1-indexed `[1]`, `[2]`, but array is 0-indexed
- Different agents produce different numbering schemes
- Citations array doesn't match marker numbers

**Impact**: Confusing or broken citation links

## Solution Strategy

### Phase 1: Server-Side Normalization

**Goal**: Ensure all workflows produce consistent citation format

#### 1.1 Synthesis Contract
```go
// All synthesis paths must:
// 1. Accept []Citation from subtask outputs
// 2. Inject [n] markers into response text (1-indexed)
// 3. Return CitationMetadata in response

type SynthesisInput struct {
    SubtaskResults []SubtaskResult
    Citations      []Citation
}

type SynthesisOutput struct {
    Response   string     // Text with [1], [2], etc.
    Metadata   Metadata   // Contains citations array
}
```

#### 1.2 Citation Injection
```go
// In orchestrator synthesis step
func InjectCitationMarkers(text string, citations []Citation) string {
    // If citations exist but no markers, append references section
    if len(citations) > 0 && !hasInlineMarkers(text) {
        text += "\n\n## Sources\n"
        for i, cite := range citations {
            text += fmt.Sprintf("[%d] %s\n", i+1, cite.Title)
        }
    }
    return text
}
```

#### 1.3 Force Synthesis When Citations Exist
```go
// In workflow decision logic
if len(toolOutputs) > 0 && hasCitations(toolOutputs) {
    // Always route through synthesis to inject markers
    return routeToSynthesis(toolOutputs)
}
```

#### 1.4 SSE Event Standardization
```go
// Always emit final result with both text and metadata
event := SSEEvent{
    Type:      "thread.message.completed",
    AgentID:   "synthesis", // or "simple-agent" for single-agent
    Response:  textWithMarkers,
    Metadata: Metadata{
        Citations: consolidatedCitations,
    },
}
```

### Phase 2: Frontend Normalization

**Goal**: Handle varying formats gracefully with fallbacks

#### 2.1 Metadata Capture from SSE
```typescript
// In runSlice.ts - Capture metadata from final event
if (event.type === "thread.message.completed" && 
    event.agent_id === "synthesis" || event.agent_id === "simple-agent") {
    
    // Store metadata separately for later merge
    state.pendingCitations = event.metadata?.citations;
}
```

#### 2.2 Merge on fetchFinalOutput
```typescript
const fetchFinalOutput = async () => {
    const task = await getTask(currentTaskId);
    
    // Merge SSE metadata with task metadata
    const citations = state.pendingCitations || task.metadata?.citations;
    
    dispatch(addMessage({
        content: task.result,
        metadata: {
            ...task.metadata,
            citations, // Prefer SSE metadata if available
        },
    }));
};
```

#### 2.3 Fallback Rendering
```typescript
// In run-conversation.tsx - Render citations even without markers
export function MarkdownWithCitations({ content, citations }: Props) {
    const hasMarkers = citations?.length > 0 && /\[(\d+)\]/.test(content);
    
    if (citations?.length > 0 && !hasMarkers) {
        // Fallback: append citations as list
        return (
            <>
                <ReactMarkdown>{content}</ReactMarkdown>
                <CitationsList citations={citations} />
            </>
        );
    }
    
    // Normal rendering with inline tooltips
    return <ReactMarkdown components={citationComponents}>{content}</ReactMarkdown>;
}
```

#### 2.4 Validation & Logging
```typescript
// Detect and log mismatches
useEffect(() => {
    messages.forEach(msg => {
        if (msg.metadata?.citations?.length > 0) {
            const hasMarkers = /\[(\d+)\]/.test(msg.content);
            if (!hasMarkers) {
                console.warn("[Citations] Metadata without markers:", {
                    messageId: msg.id,
                    citationCount: msg.metadata.citations.length,
                });
            }
        }
    });
}, [messages]);
```

### Phase 3: Observability

**Goal**: Track citation rendering success rate

#### 3.1 Backend Metrics
```go
// In citation metadata handler
metrics.Counter("citations.generated.total").Inc()
if hasInlineMarkers(response) {
    metrics.Counter("citations.markers.injected").Inc()
} else {
    metrics.Counter("citations.markers.missing").Inc()
}
```

#### 3.2 Frontend Telemetry
```typescript
// Track citation rendering
if (citations?.length > 0) {
    analytics.track("citations.rendered", {
        count: citations.length,
        hasMarkers: hasMarkers,
        source: "sse" | "historical",
        workflow: "supervisor" | "dag" | "react",
    });
}
```

## Testing Checklist

### Server-Side
- [ ] Supervisor workflow with web_search tool → produces markers + metadata
- [ ] DAG workflow with multiple tool nodes → produces markers + metadata
- [ ] React/simple agent with tools → produces markers + metadata
- [ ] Calculator-only task → no citations (expected)
- [ ] Task that bypasses synthesis → still gets markers if citations exist

### Frontend
- [ ] Historical session with citations → renders inline tooltips
- [ ] Live SSE session with citations → renders inline tooltips
- [ ] Historical session without markers but with metadata → renders as list
- [ ] Live SSE session without markers but with metadata → renders as list
- [ ] Click citation marker → shows correct tooltip with URL and snippet
- [ ] Multiple citations in same paragraph → all render correctly

### Integration
- [ ] Submit task via desktop → watch live SSE → citations appear
- [ ] Refresh page mid-task → citations still render after load
- [ ] Complete task → refresh page → citations render from history
- [ ] Follow-up question in session → citations from both turns render

## Migration Path

### Short-Term (Quick Win)
1. Add fallback rendering in frontend (show citations as list if no markers)
2. Log marker/metadata mismatches to identify problem workflows
3. Ensure `fetchFinalOutput()` merges SSE metadata with task metadata

### Medium-Term (Normalization)
1. Audit all synthesis paths, ensure consistent marker injection
2. Standardize SSE event format for final responses
3. Force synthesis when tool outputs contain citations

### Long-Term (Architecture)
1. Create unified CitationService that all workflows use
2. Implement citation validation in SSE event emitter
3. Add E2E tests for each workflow type with citations

## Related Documentation

- [Desktop Citation Feature](../desktop/docs/agent-trace-feature.md) - Frontend implementation
- [Desktop Citation Analysis](../desktop/docs/citation-inconsistency-analysis.md) - Detailed root cause
- [Metadata Architecture](./memory-system-architecture.md) - How metadata flows through system
- [Multi-Agent Workflow](./multi-agent-workflow-architecture.md) - Supervisor and DAG workflows

## Backend Files

- `go/orchestrator/internal/metadata/citations.go` - Citation metadata handling
- `go/orchestrator/internal/workflows/supervisor.go` - Supervisor synthesis
- `go/orchestrator/internal/workflows/dag.go` - DAG aggregation
- `rust/agent-core/src/synthesis.rs` - Agent-core synthesis (if applicable)
- `python/llm-service/llm_service/tools/builtin/web_search.py` - Citation generation

## Frontend Files

- `desktop/components/run-conversation.tsx` - Citation rendering components
- `desktop/lib/features/runSlice.ts` - SSE event handling
- `desktop/app/run-detail/page.tsx` - Historical loading and `fetchFinalOutput()`

