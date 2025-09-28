-- Migration 005: Comprehensive memory system and persistence layer changes
-- This migration consolidates all changes needed for the memory system implementation
-- including agent/tool execution persistence fixes

BEGIN;

-- ============================================================================
-- PART 1: Fix agent_executions table
-- ============================================================================

-- Drop existing foreign key constraints that reference unused task_executions
ALTER TABLE agent_executions
    DROP CONSTRAINT IF EXISTS agent_executions_task_execution_id_fkey;

-- Add new columns needed by persistence layer
ALTER TABLE agent_executions
    ADD COLUMN IF NOT EXISTS workflow_id VARCHAR(255),
    ADD COLUMN IF NOT EXISTS task_id VARCHAR(255),
    ADD COLUMN IF NOT EXISTS error_message TEXT,
    ADD COLUMN IF NOT EXISTS metadata JSONB DEFAULT '{}',
    ADD COLUMN IF NOT EXISTS updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW();

-- Drop unused columns from old schema
ALTER TABLE agent_executions
    DROP COLUMN IF EXISTS task_execution_id,
    DROP COLUMN IF EXISTS execution_order,
    DROP COLUMN IF EXISTS mode,
    DROP COLUMN IF EXISTS cost_usd,
    DROP COLUMN IF EXISTS memory_used_mb,
    DROP COLUMN IF EXISTS completed_at;

-- Add indexes for performance
CREATE INDEX IF NOT EXISTS idx_agent_executions_workflow_id ON agent_executions(workflow_id);
CREATE INDEX IF NOT EXISTS idx_agent_executions_task_id ON agent_executions(task_id);
CREATE INDEX IF NOT EXISTS idx_agent_executions_agent_id ON agent_executions(agent_id);
CREATE INDEX IF NOT EXISTS idx_agent_executions_created_at_desc ON agent_executions(created_at DESC);

-- ============================================================================
-- PART 2: Fix tool_executions table
-- ============================================================================

-- Drop existing foreign key constraints
ALTER TABLE tool_executions
    DROP CONSTRAINT IF EXISTS tool_executions_task_execution_id_fkey,
    DROP CONSTRAINT IF EXISTS tool_executions_agent_execution_id_fkey;

-- Add new columns needed by persistence layer
ALTER TABLE tool_executions
    ADD COLUMN IF NOT EXISTS workflow_id VARCHAR(255),
    ADD COLUMN IF NOT EXISTS agent_id VARCHAR(255),
    ADD COLUMN IF NOT EXISTS metadata JSONB DEFAULT '{}';

-- Rename error_message to error to match Go struct
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.columns
               WHERE table_name='tool_executions' AND column_name='error_message') THEN
        ALTER TABLE tool_executions RENAME COLUMN error_message TO error;
    END IF;
END $$;

-- Change output type from JSONB to TEXT to match Go struct
ALTER TABLE tool_executions
    ALTER COLUMN output TYPE TEXT USING output::TEXT;

-- Drop unused columns from old schema
ALTER TABLE tool_executions
    DROP COLUMN IF EXISTS task_execution_id,
    DROP COLUMN IF EXISTS tool_version,
    DROP COLUMN IF EXISTS category,
    DROP COLUMN IF EXISTS sandboxed,
    DROP COLUMN IF EXISTS memory_used_mb,
    DROP COLUMN IF EXISTS executed_at;

-- Add created_at if missing
ALTER TABLE tool_executions
    ADD COLUMN IF NOT EXISTS created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW();

-- Add indexes for performance
CREATE INDEX IF NOT EXISTS idx_tool_executions_workflow_id ON tool_executions(workflow_id);
CREATE INDEX IF NOT EXISTS idx_tool_executions_agent_id ON tool_executions(agent_id);
CREATE INDEX IF NOT EXISTS idx_tool_executions_created_at_desc ON tool_executions(created_at DESC);

-- ============================================================================
-- PART 3: Add composite indexes for analytics queries
-- ============================================================================

-- Composite index for per-agent success rate queries (Phase 2 analytics)
CREATE INDEX IF NOT EXISTS idx_agent_executions_agent_state_created
    ON agent_executions(agent_id, state, created_at DESC);

-- Composite index for workflow-level analysis
CREATE INDEX IF NOT EXISTS idx_agent_executions_workflow_created
    ON agent_executions(workflow_id, created_at DESC);

-- Composite index for tool usage analytics
CREATE INDEX IF NOT EXISTS idx_tool_executions_tool_success_created
    ON tool_executions(tool_name, success, created_at DESC);

-- ============================================================================
-- PART 4: Add comments for documentation
-- ============================================================================

COMMENT ON TABLE agent_executions IS 'Stores individual agent execution records for observability and analytics';
COMMENT ON TABLE tool_executions IS 'Stores tool invocation records for tracking tool usage and performance';

COMMENT ON COLUMN agent_executions.workflow_id IS 'Temporal workflow ID from task_executions table';
COMMENT ON COLUMN agent_executions.task_id IS 'Reference to task_executions.id (often same as workflow_id)';
COMMENT ON COLUMN agent_executions.metadata IS 'Additional execution metadata as JSON';
COMMENT ON COLUMN agent_executions.updated_at IS 'Last update timestamp';

COMMENT ON COLUMN tool_executions.workflow_id IS 'Temporal workflow ID from task_executions table';
COMMENT ON COLUMN tool_executions.agent_id IS 'Agent that executed the tool';
COMMENT ON COLUMN tool_executions.metadata IS 'Additional tool execution metadata as JSON';
COMMENT ON COLUMN tool_executions.error IS 'Error message if tool execution failed';

COMMIT;