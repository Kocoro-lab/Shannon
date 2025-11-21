# Streaming SSE Implementation - Remaining Enhancements

## High Priority

### 1. Add Usage Metadata Streaming for Non-OpenAI Providers
**Status**: Pending
**Impact**: High - Usage metadata won't appear in SSE for xAI/Anthropic/other providers

**Tasks**:
- [ ] `python/llm-service/llm_provider/xai_provider.py`: Add usage dict emission in `stream_complete()`
  ```python
  # After yielding content deltas, check for usage and yield dict
  if chunk.usage:
      yield {
          "usage": {
              "total_tokens": chunk.usage.total_tokens,
              "input_tokens": chunk.usage.prompt_tokens,
              "output_tokens": chunk.usage.completion_tokens,
          },
          "model": chunk.model,
          "provider": "xai",
      }
  ```

- [ ] `python/llm-service/llm_provider/anthropic_provider.py`: Add usage dict emission in `stream_complete()`
- [ ] `python/llm-service/llm_provider/groq_provider.py`: Add usage dict emission if supported
- [ ] `python/llm-service/llm_provider/google_provider.py`: Add usage dict emission if supported
- [ ] Test each provider's streaming with usage metadata verification

---

## Medium Priority

### 2. Fix Fallback Streaming to Pass Dict Chunks
**Status**: Pending
**Impact**: Medium - Usage metadata lost when primary provider fails and fallback is used

**Location**: `python/llm-service/llm_provider/manager.py:736-738`

**Fix**:
```python
# Current (drops dicts):
async for chunk in fallback[1].stream_complete(request):
    if isinstance(chunk, str) and chunk:  # ❌
        yield chunk

# Fixed:
async for chunk in fallback[1].stream_complete(request):
    if isinstance(chunk, (str, dict)) and chunk:  # ✅
        yield chunk
```

---

### 3. Make Tool Observation Truncation UTF-8 Safe
**Status**: Pending
**Impact**: Medium - Can cause garbled text for non-ASCII tool outputs

**Location**: `go/orchestrator/internal/activities/agent.go:1347-1349`

**Fix**:
```go
// Current (byte truncation - unsafe):
if len(msg) > 2000 {
    msg = msg[:2000]  // ❌ Can split UTF-8 multi-byte chars
}

// Fixed (rune truncation - safe):
if len(msg) > 2000 {
    runes := []rune(msg)
    if len(runes) > 2000 {
        msg = string(runes[:2000])
    }
}
```

**Alternative**: Extract to helper function like `truncateQuery()` which already does UTF-8 safe truncation.

---

## Low Priority

### 4. Code Quality Improvements
- [ ] Extract provider/model fallback logic (duplicated in streaming/unary paths)
- [ ] Extract tool deduplication logic to helper function
- [ ] Consider extracting flexible parsing helpers to separate util package

### 5. Add Unit Tests
- [ ] Test flexible parsing functions with various JSON types
- [ ] Test TOOL_OBSERVATION event emission
- [ ] Test MCP cost-to-token bump calculation
- [ ] Test usage metadata propagation in streaming

---

## Architecture Notes

### Python Tool Usage Capture
**Note**: This requires architectural design discussion

**Challenge**: Python-only tools (calculator, vendor tools) execute via internal function calling and don't emit usage through the streaming path.

**Current Behavior**:
- Tools execute in Python llm-service
- No `ToolResults` returned to agent-core
- No SSE TOOL_OBSERVATION events emitted (by design)
- Results embedded in LLM response text
- Token usage not captured in streaming metadata

**Potential Solutions**:
1. Capture usage from Python tool execution context
2. Add post-execution usage reconciliation
3. Consider making Python tools report usage back through a separate channel
4. Document as known limitation if architectural change not feasible

---

## Testing Matrix

| Provider | Streaming | Usage Metadata | Tool Events | Status |
|----------|-----------|----------------|-------------|--------|
| OpenAI | ✅ | ✅ | ✅ (agent-core tools) | **Complete** |
| xAI | ✅ | ❌ | ✅ (agent-core tools) | **Pending** |
| Anthropic | ✅ | ❌ | ✅ (agent-core tools) | **Pending** |
| Groq | ✅ | ❌ | ✅ (agent-core tools) | **Pending** |
| Google | ✅ | ❌ | ✅ (agent-core tools) | **Pending** |

**Note**: Python-only tools don't emit TOOL_OBSERVATION events for any provider (architectural design).

---

## Related Documentation

- `docs/streaming-api.md` - Streaming API specification
- `docs/adding-custom-tools.md` - Tool execution architecture
- `CLAUDE.md` - Two-tier tool execution paths explained
- `go/orchestrator/internal/activities/agent.go` - Streaming implementation
