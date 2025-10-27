# P2P Agent Coordination in Shannon

## Overview

Shannon now supports **Peer-to-Peer (P2P) Agent Coordination**, enabling autonomous agents to coordinate task execution based on data dependencies. This feature allows agents to wait for required data from other agents before proceeding, creating efficient pipelines without manual orchestration.

## How It Works

### 1. Automatic Dependency Detection
When you submit a query with sequential or dependent steps, Shannon's decomposition service automatically:
- Identifies what data each subtask **produces**
- Determines what data each subtask **consumes** (needs from other tasks)
- Routes to SupervisorWorkflow when dependencies are detected

### 2. Coordination Mechanism
Agents use a topic-based publish-subscribe pattern:
- **Producer agents** publish results to semantic topics (e.g., "analysis-results", "metrics")
- **Consumer agents** wait for required topics before starting execution
- Workspace storage (Redis-based) facilitates data exchange between agents

### 3. Workflow Routing
The system automatically selects the appropriate workflow:
- **No dependencies** → SimpleTaskWorkflow or DAGWorkflow (parallel)
- **With dependencies** → SupervisorWorkflow with P2P coordination
- **Forced P2P** → Always use SupervisorWorkflow

## Configuration

### Enable P2P Coordination

Configure via `config/features.yaml`:

```yaml
# config/features.yaml
workflows:
  p2p:
    enabled: true           # Master switch for P2P coordination
    timeout_seconds: 360    # Maximum wait time for dependencies
```

Note: P2P is not toggled via environment variables; the orchestrator reads these YAML settings at runtime.

## Usage Examples

### Example 1: Sequential Pipeline
```python
# Query: "Analyze sales data and then create a report based on the analysis"

# Shannon automatically detects:
# - Task 1: Analyze → produces: ["sales-analysis", "insights"]
# - Task 2: Report → consumes: ["sales-analysis", "insights"]
#
# Task 2 waits for Task 1 to complete before starting
```

### Example 2: Complex Data Pipeline
```python
# Query: "Load CSV, process the data, create visualizations, and generate PDF report"

# Dependency chain detected:
# - Load CSV → produces: ["raw-data"]
# - Process → consumes: ["raw-data"], produces: ["processed-data", "statistics"]
# - Visualize → consumes: ["statistics"], produces: ["charts"]
# - PDF → consumes: ["processed-data", "charts"]
```

### Example 3: Force P2P Mode
```python
# Force P2P coordination even for simple tasks
grpcurl -d '{
  "query": "What is 2+2?",
  "context": {"force_p2p": true}
}' localhost:50052 shannon.orchestrator.OrchestratorService/SubmitTask
```

## API Usage

### Via gRPC
```protobuf
// Normal usage - P2P activates automatically when needed
SubmitTaskRequest {
  query: "Analyze data then create report"
  context: {}
}

// Force P2P mode
SubmitTaskRequest {
  query: "Simple calculation"
  context: {
    "force_p2p": true
  }
}
```

### Via Python Client
```python
from shannon_client import ShannonClient

client = ShannonClient()

# Automatic P2P for dependent tasks
response = client.submit_task(
    "Research the topic, validate findings, and write article"
)

# Force P2P mode
response = client.submit_task(
    "Simple query",
    context={"force_p2p": True}
)
```

## Dependency Detection Rules

The system detects dependencies based on:

1. **Sequential indicators**: "then", "after", "based on", "using the results"
2. **Data flow analysis**: What each task produces and what it needs
3. **Tool outputs**: Tasks using tools automatically produce the tool's output type
4. **Semantic understanding**: LLM analyzes the logical flow of tasks

## Benefits

1. **Automatic Orchestration**: No need to manually specify task order
2. **Efficient Execution**: Tasks run as soon as dependencies are satisfied
3. **Parallel When Possible**: Independent tasks still run in parallel
4. **Robust Coordination**: Timeout protection and error handling built-in
5. **Transparent**: Logs show P2P coordination decisions

## Monitoring

Check P2P coordination in logs:

```bash
# View P2P detection in decomposition
docker compose logs llm-service | grep "P2P coordination detected"

# View workflow routing decisions
docker compose logs orchestrator | grep "SupervisorWorkflow"

# View dependency waiting
docker compose logs orchestrator | grep "Dependency wait"
```

## Architecture Details

### Components Involved

1. **LLM Service**: Detects and populates `produces`/`consumes` fields during decomposition
2. **Orchestrator Router**: Routes tasks with dependencies to SupervisorWorkflow
3. **SupervisorWorkflow**: Manages P2P coordination and dependency waiting
4. **Workspace Activities**: Handle data exchange via Redis

### Data Flow

```
User Query → Decomposition (detects dependencies) → Router (selects workflow)
    ↓                                                   ↓
If dependencies exist                            SupervisorWorkflow
    ↓                                                   ↓
Agent 1 executes → Publishes to workspace → Agent 2 waits → Agent 2 executes
```

## Comparison with Other Frameworks

| Framework | Coordination Method | Shannon's Advantage |
|-----------|-------------------|-------------------|
| LangGraph | Static graph edges | Dynamic dependency detection |
| CrewAI | Role-based sequence | Automatic data flow analysis |
| AutoGen | Conversation-based | Structured P2P with timeouts |
| OpenAI SDK | No built-in P2P | Native P2P infrastructure |

## Limitations

- Maximum timeout is configurable (default 6 minutes)
- Circular dependencies are not currently detected (planned for future)
- P2P adds overhead for simple tasks (use force flag judiciously)

## Future Enhancements

Planned improvements:
- Circular dependency detection
- Cross-session data sharing
- Priority-based task scheduling
- Dynamic timeout adjustment
- Visual dependency graph generation

## Testing

Run the P2P coordination test suite:

```bash
./tests/e2e/p2p_coordination_test.sh
```

This tests:
1. Automatic P2P activation for dependent tasks
2. Force P2P mode functionality
3. Complex pipeline coordination

## Troubleshooting

### P2P Not Activating
- Check if `produces`/`consumes` fields are populated in decomposition
- Verify P2P is enabled in configuration
- Ensure SupervisorWorkflow is being selected

### Tasks Not Waiting for Dependencies
- Check workspace connectivity (Redis)
- Verify topic names match between producer/consumer
- Check timeout settings

### Force P2P Not Working
- Ensure context contains `"force_p2p": true`
- Check orchestrator logs for "forced" message
- Verify latest code is deployed
