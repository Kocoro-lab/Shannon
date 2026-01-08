# Deep Research Agent Job Failure Analysis

**Date:** 2026-01-07  
**Task ID:** `task-00000000-0000-0000-0000-000000000002-1767794308`  
**Session ID:** `12376a7d-3d75-4fad-9bf4-8c02dd0d6d55`

## Executive Summary

The deep research agent job failed due to **repeated timeout errors** when the orchestrator attempted to call the LLM service's decomposition endpoint (`http://llm-service:8000/agent/decompose`). The service failed to respond within the configured timeout period, causing the workflow to fail after 3 retry attempts.

---

## Failure Timeline

1. **11:26:20 UTC** - Research workflow started successfully with query refinement
2. **11:26:51 UTC** - Query successfully refined (8,062 tokens used)
3. **11:27:21 UTC** - Attempted task decomposition
4. **11:27:51 UTC** - **First timeout** (30s): `Client.Timeout exceeded while awaiting headers`
5. **11:28:22 UTC** - **Second timeout** (31s): `context deadline exceeded`
6. **11:28:54 UTC** - **Third timeout** (32s): `request canceled`
7. **11:28:54 UTC** - **Workflow failed** with terminal state `FAILED`

---

## Root Cause Analysis

### Primary Issue: LLM Service Timeout

**Error Pattern:**
```
failed to call LLM decomposition service: Post "http://llm-service:8000/agent/decompose": 
net/http: request canceled (Client.Timeout exceeded while awaiting headers)
```

**Affected Services:**
- **Orchestrator** (`orchestrator-1`) - Making the HTTP request
- **LLM Service** (`llm-service-1`) - Not responding in time

### Contributing Factors

1. **HTTP Client Timeout Too Aggressive**
   - The orchestrator's HTTP client timeout appears to be ~30 seconds
   - Complex decomposition tasks (especially for deep research) may require more time
   - The LLM service is likely processing but not responding within the timeout window

2. **LLM Service Performance Issues**
   - The `/agent/decompose` endpoint may be:
     - Overloaded with the complex research query
     - Waiting on external LLM provider API calls
     - Experiencing internal processing delays
     - Not properly streaming or chunking responses

3. **No Observability into LLM Service**
   - The only errors visible are OpenTelemetry export warnings (unrelated)
   - No application-level logs showing the decompose request being received
   - Cannot determine if the request reached the service or where it's stuck

4. **Retry Strategy Ineffective**
   - 3 retries with similar timeouts won't help if the underlying issue is processing time
   - Each retry attempt takes ~30s, compounding the delay

---

## Services Involved

### 1. **Orchestrator Service** ❌ (Failed)
- **Role:** Workflow orchestration via Temporal
- **Issue:** HTTP client timeout when calling LLM service
- **Location:** `go/orchestrator/internal/activities/`
- **Error:** Activity `DecomposeTask` failed after 3 attempts

### 2. **LLM Service** ⚠️ (Unresponsive)
- **Role:** Provides LLM-powered decomposition, refinement, and generation
- **Issue:** `/agent/decompose` endpoint not responding within 30s
- **Location:** `python/llm-service/`
- **Status:** Service is running (health checks passing) but decompose endpoint timing out

### 3. **Agent Core** ✅ (Healthy)
- **Role:** Rust-based agent execution
- **Status:** Running normally, only OpenTelemetry export warnings (non-critical)

### 4. **Gateway** ✅ (Healthy)
- **Role:** API gateway
- **Status:** Running normally, authentication bypassed in dev mode

---

## Proposed Solutions

### Solution 1: Increase HTTP Client Timeout (Quick Fix) ⭐ **RECOMMENDED**

**What:** Increase the orchestrator's HTTP client timeout for LLM service calls

**Where:** `go/orchestrator/internal/activities/` or HTTP client configuration

**Changes:**
```go
// Current (estimated): 30s timeout
// Proposed: 120s for decomposition, 60s for other LLM calls

client := &http.Client{
    Timeout: 120 * time.Second, // For decomposition
}
```

**Pros:**
- Quick to implement
- Addresses immediate symptom
- Allows complex research queries to complete

**Cons:**
- Doesn't address underlying performance issue
- May mask deeper problems

**Implementation Priority:** HIGH (1-2 hours)

---

### Solution 2: Add Streaming/Chunked Response Support (Medium-term)

**What:** Modify the decomposition endpoint to stream responses or send progress updates

**Where:** 
- `python/llm-service/app/routers/agent.py` (decompose endpoint)
- `go/orchestrator/internal/activities/` (client handling)

**Changes:**
- Implement Server-Sent Events (SSE) or chunked transfer encoding
- Send periodic heartbeat/progress updates
- Allow orchestrator to know the service is working

**Pros:**
- Better user experience with progress updates
- Prevents timeout on long-running operations
- Aligns with existing streaming patterns in the codebase

**Cons:**
- More complex implementation
- Requires changes in both services

**Implementation Priority:** MEDIUM (1-2 days)

---

### Solution 3: Optimize LLM Service Performance (Long-term)

**What:** Profile and optimize the decomposition logic

**Where:** `python/llm-service/app/services/decomposition.py`

**Investigation Areas:**
1. **LLM Provider Latency**
   - Check if external API calls are the bottleneck
   - Consider caching or parallel requests

2. **Prompt Engineering**
   - Reduce token count in decomposition prompts
   - Use faster models for decomposition (if using slow models)

3. **Resource Constraints**
   - Check CPU/memory usage during decomposition
   - Scale service if needed

4. **Add Instrumentation**
   - Add detailed logging to decompose endpoint
   - Track time spent in each phase
   - Identify specific bottleneck

**Pros:**
- Addresses root cause
- Improves overall system performance
- Benefits all workflows

**Cons:**
- Requires investigation time
- May need architectural changes

**Implementation Priority:** MEDIUM-LOW (3-5 days)

---

### Solution 4: Implement Async Decomposition Pattern

**What:** Make decomposition asynchronous with polling

**Where:**
- `python/llm-service/` - Add async job queue
- `go/orchestrator/` - Poll for completion

**Pattern:**
1. Orchestrator submits decomposition job → receives job ID
2. LLM service processes in background
3. Orchestrator polls for completion
4. Retrieve results when ready

**Pros:**
- Eliminates timeout issues entirely
- Allows for very long-running operations
- Better scalability

**Cons:**
- Significant architectural change
- Adds complexity
- Requires job queue infrastructure

**Implementation Priority:** LOW (1-2 weeks)

---

## Immediate Action Plan

### Step 1: Increase Timeout (Today)
```bash
# File: go/orchestrator/internal/activities/llm_client.go or similar
# Increase timeout from 30s to 120s for decomposition calls
```

### Step 2: Add Logging (Today)
```python
# File: python/llm-service/app/routers/agent.py
# Add detailed logging to decompose endpoint:
# - Request received
# - LLM call started
# - LLM call completed
# - Response sent
```

### Step 3: Monitor (Ongoing)
- Watch for timeout errors in logs
- Track decomposition endpoint latency
- Set up alerts for >60s response times

### Step 4: Optimize (This Week)
- Profile the decomposition endpoint
- Identify specific bottleneck
- Implement targeted optimization

---

## Configuration Changes Needed

### 1. Orchestrator HTTP Client Timeout

**File:** `go/orchestrator/internal/activities/llm_client.go` (or wherever HTTP client is configured)

```go
// Recommended timeout values
const (
    DecompositionTimeout = 120 * time.Second  // Complex analysis
    RefinementTimeout    = 60 * time.Second   // Query refinement
    GenerationTimeout    = 90 * time.Second   // Content generation
    DefaultTimeout       = 30 * time.Second   // Other calls
)
```

### 2. Temporal Activity Timeout

**File:** `go/orchestrator/internal/workflows/strategies/research.go`

```go
// Ensure Temporal activity timeout is higher than HTTP timeout
activityOptions := workflow.ActivityOptions{
    StartToCloseTimeout: 180 * time.Second, // 3 minutes
    RetryPolicy: &temporal.RetryPolicy{
        MaximumAttempts: 3,
        InitialInterval: 5 * time.Second,
    },
}
```

### 3. LLM Service Logging

**File:** `python/llm-service/app/routers/agent.py`

```python
@router.post("/decompose")
async def decompose_task(request: DecomposeRequest):
    logger.info(f"Decompose request received: {len(request.query)} chars")
    start_time = time.time()
    
    try:
        # ... existing logic ...
        logger.info(f"Decompose completed in {time.time() - start_time:.2f}s")
        return result
    except Exception as e:
        logger.error(f"Decompose failed after {time.time() - start_time:.2f}s: {e}")
        raise
```

---

## Testing Plan

### 1. Verify Fix
```bash
# After increasing timeout, test with the same research query
# Expected: Decomposition completes successfully
```

### 2. Load Testing
```bash
# Test with various query complexities
# Measure actual decomposition times
# Ensure timeout is appropriate
```

### 3. Monitoring
```bash
# Watch logs for:
docker compose -f deploy/compose/docker-compose.yml logs -f llm-service | grep decompose
docker compose -f deploy/compose/docker-compose.yml logs -f orchestrator | grep DecomposeTask
```

---

## Prevention Measures

1. **Add Timeout Monitoring**
   - Alert when decomposition takes >60s
   - Track P95/P99 latencies

2. **Add Circuit Breaker**
   - Fail fast if LLM service is consistently slow
   - Provide better error messages to users

3. **Add Request Size Limits**
   - Limit query length for decomposition
   - Reject overly complex requests early

4. **Improve Observability**
   - Add distributed tracing
   - Track request flow across services
   - Identify bottlenecks proactively

---

## Related Issues

1. **OpenTelemetry Export Warnings** (Non-critical)
   - Both `llm-service` and `agent-core` show OTLP export errors
   - Indicates observability collector is not running
   - Does not affect functionality but reduces monitoring capability

2. **Qdrant Collection Error** (Earlier workflow)
   - `qdrant upsert status 404` for `decomposition_patterns` collection
   - Indicates Qdrant collection may not be initialized
   - May affect pattern learning but not immediate execution

---

## Conclusion

The deep research job failed due to **HTTP timeout when calling the LLM service's decomposition endpoint**. The immediate fix is to **increase the HTTP client timeout from ~30s to 120s** for decomposition calls. Medium-term improvements should focus on **adding streaming support** and **optimizing the decomposition logic**. Long-term, consider an **async job pattern** for very long-running operations.

**Estimated Time to Fix:** 1-2 hours for immediate timeout increase  
**Estimated Time to Full Solution:** 3-5 days including optimization and monitoring
