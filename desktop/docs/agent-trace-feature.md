# Agent Trace Feature

## Overview

The agent trace feature allows users to toggle visibility of intermediate agent thinking steps in the conversation view. By default, only final answers are shown for a clean user experience.

## Behavior

### Default State
- Agent trace messages are **hidden by default**
- Only final answer messages are visible
- Button displays "Show Agent Trace"

### When Enabled
- All intermediate agent thinking steps become visible
- Button changes to "Hide Agent Trace"
- Messages appear in collapsible format with agent ID labels

## Message Classification

### Final Answer Agents (Always Visible)
Messages from these agents are **always shown**, regardless of trace setting:
- `synthesis` - Multi-agent synthesis results
- `simple-agent` - Simple task responses
- `assistant` - Direct assistant responses
- Messages with no `sender` property

### Intermediate Agents (Trace Only)
Messages from these agents are **only shown when trace is enabled**:
- `generalist` - General reasoning steps
- `reasoner-*` - Reasoning agent steps
- `actor-*` - Actor agent steps
- `react-synthesizer` - React workflow internal steps
- `agent-task-*` - Subtask agent outputs
- Tool call JSON messages (with `selected_tools`)

## Implementation Details

### Redux State (`runSlice.ts`)
- Processes `thread.message.delta`, `thread.message.completed`, and `LLM_OUTPUT` events
- Creates messages with `sender` property set to `event.agent_id`
- Messages are stored with metadata for filtering

### Historical Sessions (`run-detail/page.tsx`)
- Loads intermediate messages from `turn.events`
- Filters events by type: `LLM_OUTPUT` or `thread.message.completed`
- Excludes title generation and final answer agents
- Reconstructs conversation with proper sender attribution

### UI Component (`run-conversation.tsx`)
- `isIntermediateMessage()` determines message visibility
- Filters messages based on `showAgentTrace` prop
- Renders intermediate messages in `CollapsibleMessage` component

## Usage for Developers

### When Adding New Agent Types

1. **Final Answer Agent**: No changes needed - messages visible by default
2. **Intermediate Agent**: Messages automatically hidden by default
3. **Custom Classification**: Update `isIntermediateMessage()` in `run-conversation.tsx`

### Testing Checklist

- [ ] Historical sessions load correctly with trace toggled
- [ ] Live streaming shows trace messages when enabled
- [ ] Simple tasks work (no unnecessary trace messages)
- [ ] Multi-agent workflows show intermediate steps when toggled
- [ ] Button state persists during session
- [ ] Final answer always visible regardless of setting

## Design Rationale

**Hide by Default**: Most users want clean conversation without internal reasoning steps.

**Toggleable**: Power users and developers need visibility into agent behavior for debugging and understanding.

**Per-Session**: Each conversation can have trace independently toggled based on user needs.

