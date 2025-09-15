-- Migration: Add persistence tables for task, agent, tool executions and analytics
-- Version: 002
-- Description: Core persistence tables for workflow tracking and analytics

-- Enable UUID extension if not already enabled
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- Task execution history (from Orchestrator)
CREATE TABLE IF NOT EXISTS task_executions (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    workflow_id VARCHAR(255) UNIQUE NOT NULL,
    user_id UUID REFERENCES users(id),
    session_id VARCHAR(255),
    query TEXT NOT NULL,
    mode VARCHAR(50), -- SIMPLE, STANDARD, COMPLEX
    status VARCHAR(50) NOT NULL, -- RUNNING, COMPLETED, FAILED, CANCELLED
    started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at TIMESTAMPTZ,
    
    -- Results
    result TEXT,
    error_message TEXT,
    
    -- Token metrics (detailed)
    total_tokens INTEGER DEFAULT 0,
    prompt_tokens INTEGER DEFAULT 0,
    completion_tokens INTEGER DEFAULT 0,
    total_cost_usd DECIMAL(10,6) DEFAULT 0,
    
    -- Performance metrics
    duration_ms INTEGER,
    agents_used INTEGER DEFAULT 0,
    tools_invoked INTEGER DEFAULT 0,
    cache_hits INTEGER DEFAULT 0,
    complexity_score DECIMAL(3,2),
    
    -- Metadata
    metadata JSONB,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

-- Indexes for task_executions
CREATE INDEX idx_task_user_session ON task_executions(user_id, session_id);
CREATE INDEX idx_task_created_at ON task_executions(created_at DESC);
CREATE INDEX idx_task_status ON task_executions(status);
CREATE INDEX idx_task_workflow_id ON task_executions(workflow_id);

-- Agent execution details (from Agent-Core)
CREATE TABLE IF NOT EXISTS agent_executions (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    task_execution_id UUID REFERENCES task_executions(id) ON DELETE CASCADE,
    agent_id VARCHAR(255) NOT NULL,
    execution_order INTEGER NOT NULL,
    
    -- Execution details
    input TEXT NOT NULL,
    output TEXT,
    mode VARCHAR(50),
    state VARCHAR(50), -- ANALYZING, PLANNING, EXECUTING, COMPLETED, FAILED
    
    -- Token usage
    tokens_used INTEGER DEFAULT 0,
    cost_usd DECIMAL(10,6) DEFAULT 0,
    model_used VARCHAR(100),
    
    -- Performance
    duration_ms INTEGER,
    memory_used_mb INTEGER,
    
    created_at TIMESTAMPTZ DEFAULT NOW(),
    completed_at TIMESTAMPTZ
);

-- Indexes for agent_executions
CREATE INDEX idx_agent_task_execution ON agent_executions(task_execution_id);
CREATE INDEX idx_agent_created_at ON agent_executions(created_at DESC);
CREATE INDEX idx_agent_state ON agent_executions(state);

-- Tool execution history
CREATE TABLE IF NOT EXISTS tool_executions (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    agent_execution_id UUID REFERENCES agent_executions(id) ON DELETE CASCADE,
    task_execution_id UUID REFERENCES task_executions(id) ON DELETE CASCADE,
    
    tool_name VARCHAR(255) NOT NULL,
    tool_version VARCHAR(50),
    category VARCHAR(100), -- search, calculation, file, etc.
    
    -- Execution details
    input_params JSONB,
    output JSONB,
    success BOOLEAN DEFAULT true,
    error_message TEXT,
    
    -- Performance
    duration_ms INTEGER,
    tokens_consumed INTEGER DEFAULT 0,
    
    -- Sandbox info
    sandboxed BOOLEAN DEFAULT true,
    memory_used_mb INTEGER,
    
    executed_at TIMESTAMPTZ DEFAULT NOW()
);

-- Indexes for tool_executions
CREATE INDEX idx_tool_name ON tool_executions(tool_name);
CREATE INDEX idx_tool_executed_at ON tool_executions(executed_at DESC);
CREATE INDEX idx_tool_task_execution ON tool_executions(task_execution_id);
CREATE INDEX idx_tool_success ON tool_executions(success);

-- Session archives (periodic snapshots from Redis)
CREATE TABLE IF NOT EXISTS session_archives (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    session_id VARCHAR(255) NOT NULL,
    user_id UUID REFERENCES users(id),
    
    -- Snapshot data
    snapshot_data JSONB, -- Full session state from Redis
    message_count INTEGER,
    total_tokens INTEGER,
    total_cost_usd DECIMAL(10,6),
    
    -- Timing
    session_started_at TIMESTAMPTZ,
    snapshot_taken_at TIMESTAMPTZ DEFAULT NOW(),
    ttl_expires_at TIMESTAMPTZ
);

-- Indexes for session_archives
CREATE INDEX idx_session_archive_session_id ON session_archives(session_id);
CREATE INDEX idx_session_archive_user_id ON session_archives(user_id);
CREATE INDEX idx_session_archive_taken_at ON session_archives(snapshot_taken_at DESC);

-- Daily aggregations for analytics
CREATE TABLE IF NOT EXISTS usage_daily_aggregates (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id UUID REFERENCES users(id),
    date DATE NOT NULL,
    
    -- Aggregated metrics
    total_tasks INTEGER DEFAULT 0,
    successful_tasks INTEGER DEFAULT 0,
    failed_tasks INTEGER DEFAULT 0,
    
    -- Token usage
    total_tokens INTEGER DEFAULT 0,
    total_cost_usd DECIMAL(10,6) DEFAULT 0,
    
    -- Model distribution
    model_usage JSONB, -- {"gpt-4": 100, "gpt-3.5": 500}
    
    -- Tool usage
    tools_invoked INTEGER DEFAULT 0,
    tool_distribution JSONB, -- {"calculator": 10, "web_search": 5}
    
    -- Performance
    avg_duration_ms INTEGER,
    cache_hit_rate DECIMAL(3,2),
    
    created_at TIMESTAMPTZ DEFAULT NOW(),
    
    UNIQUE(user_id, date)
);

-- Indexes for usage_daily_aggregates
CREATE INDEX idx_usage_daily_user_date ON usage_daily_aggregates(user_id, date);
CREATE INDEX idx_usage_daily_date ON usage_daily_aggregates(date DESC);

-- Function to update daily aggregates (can be called after task completion)
CREATE OR REPLACE FUNCTION update_daily_aggregate(p_user_id UUID, p_date DATE)
RETURNS VOID AS $$
BEGIN
    INSERT INTO usage_daily_aggregates (
        user_id, date, 
        total_tasks, successful_tasks, failed_tasks,
        total_tokens, total_cost_usd,
        avg_duration_ms
    )
    SELECT 
        p_user_id, 
        p_date,
        COUNT(*) as total_tasks,
        COUNT(CASE WHEN status = 'COMPLETED' THEN 1 END) as successful_tasks,
        COUNT(CASE WHEN status = 'FAILED' THEN 1 END) as failed_tasks,
        COALESCE(SUM(total_tokens), 0) as total_tokens,
        COALESCE(SUM(total_cost_usd), 0) as total_cost_usd,
        COALESCE(AVG(duration_ms), 0)::INTEGER as avg_duration_ms
    FROM task_executions
    WHERE user_id = p_user_id 
        AND DATE(started_at) = p_date
    ON CONFLICT (user_id, date) DO UPDATE SET
        total_tasks = EXCLUDED.total_tasks,
        successful_tasks = EXCLUDED.successful_tasks,
        failed_tasks = EXCLUDED.failed_tasks,
        total_tokens = EXCLUDED.total_tokens,
        total_cost_usd = EXCLUDED.total_cost_usd,
        avg_duration_ms = EXCLUDED.avg_duration_ms,
        created_at = NOW();
END;
$$ LANGUAGE plpgsql;

-- Trigger to auto-update aggregates when task completes
CREATE OR REPLACE FUNCTION trigger_update_daily_aggregate()
RETURNS TRIGGER AS $$
BEGIN
    IF NEW.status IN ('COMPLETED', 'FAILED') AND NEW.user_id IS NOT NULL THEN
        PERFORM update_daily_aggregate(NEW.user_id, DATE(NEW.started_at));
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER task_completion_aggregate
    AFTER INSERT OR UPDATE OF status ON task_executions
    FOR EACH ROW
    EXECUTE FUNCTION trigger_update_daily_aggregate();

-- Partitioning for task_executions by month (for scale)
-- Note: This is optional and can be applied when data volume justifies it
-- Example for creating monthly partitions:
-- CREATE TABLE task_executions_2024_01 PARTITION OF task_executions
--     FOR VALUES FROM ('2024-01-01') TO ('2024-02-01');

-- Add comments for documentation
COMMENT ON TABLE task_executions IS 'Stores all task/workflow execution history with token usage and performance metrics';
COMMENT ON TABLE agent_executions IS 'Stores individual agent execution details within a task';
COMMENT ON TABLE tool_executions IS 'Stores tool invocation history with sandbox and performance data';
COMMENT ON TABLE session_archives IS 'Periodic snapshots of Redis sessions for long-term storage';
COMMENT ON TABLE usage_daily_aggregates IS 'Pre-aggregated daily usage statistics for analytics';

COMMENT ON COLUMN task_executions.workflow_id IS 'Unique identifier from Temporal, used for idempotency';
COMMENT ON COLUMN task_executions.complexity_score IS 'Complexity score from 0.0 to 1.0';
COMMENT ON COLUMN agent_executions.state IS 'FSM state: IDLE, ANALYZING, PLANNING, RETRIEVING, EXECUTING, VALIDATING, SYNTHESIZING, COMPLETED, FAILED';
COMMENT ON COLUMN tool_executions.sandboxed IS 'Whether tool was executed in WASM sandbox';

-- Update tool_calls table from 001 to properly reference agent_executions
-- Now that agent_executions table exists with the correct schema
ALTER TABLE tool_calls
    ADD COLUMN agent_execution_id UUID REFERENCES agent_executions(id) ON DELETE CASCADE,
    DROP COLUMN task_id;

CREATE INDEX idx_tool_calls_agent_execution_id ON tool_calls(agent_execution_id);
DROP INDEX IF EXISTS idx_tool_calls_task_id;