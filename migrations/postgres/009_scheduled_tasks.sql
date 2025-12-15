-- Migration: Add scheduled tasks support with unified task execution model
-- Description: Tables for managing scheduled/recurring task execution + unified task tracking

-- ============================================================================
-- Part 1: Extend task_executions for schedule support (unified task model)
-- ============================================================================

-- Add trigger_type column to identify how the task was initiated
-- Values: 'api' (default), 'schedule'
ALTER TABLE task_executions
    ADD COLUMN IF NOT EXISTS trigger_type VARCHAR(32) NOT NULL DEFAULT 'api';

-- Add schedule_id column to link scheduled runs to their schedule configuration
ALTER TABLE task_executions
    ADD COLUMN IF NOT EXISTS schedule_id UUID NULL;

-- Index for filtering by trigger type (e.g., GET /api/v1/tasks?trigger_type=schedule)
CREATE INDEX IF NOT EXISTS idx_task_executions_trigger_type
    ON task_executions(trigger_type);

-- Index for schedule-related queries (e.g., GET /api/v1/tasks?schedule_id=xxx)
CREATE INDEX IF NOT EXISTS idx_task_executions_schedule_id
    ON task_executions(schedule_id)
    WHERE schedule_id IS NOT NULL;

-- ============================================================================
-- Part 2: Scheduled tasks configuration table
-- ============================================================================

CREATE TABLE IF NOT EXISTS scheduled_tasks (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    -- Ownership
    user_id UUID NOT NULL,
    tenant_id UUID,

    -- Schedule configuration
    name VARCHAR(255) NOT NULL,
    description TEXT,
    cron_expression VARCHAR(100) NOT NULL,
    timezone VARCHAR(50) DEFAULT 'UTC',

    -- Task template (what to execute)
    -- task_context supports workflow routing via:
    --   template: "ads_research"     -> TemplateWorkflow
    --   force_research: true         -> ResearchWorkflow
    --   cognitive_strategy: "react"  -> Strategy workflows
    --   role: "data_analytics"       -> Role-based execution
    task_query TEXT NOT NULL,
    task_context JSONB DEFAULT '{}'::jsonb,

    -- Resource limits
    max_budget_per_run_usd DECIMAL(10, 4),
    timeout_seconds INTEGER DEFAULT 3600,

    -- Temporal integration
    temporal_schedule_id VARCHAR(255) UNIQUE NOT NULL,

    -- Status
    status VARCHAR(20) NOT NULL DEFAULT 'ACTIVE', -- ACTIVE, PAUSED, DELETED

    -- Audit trail
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    last_run_at TIMESTAMP WITH TIME ZONE,
    next_run_at TIMESTAMP WITH TIME ZONE,

    -- Statistics (aggregated from task_executions)
    total_runs INTEGER DEFAULT 0,
    successful_runs INTEGER DEFAULT 0,
    failed_runs INTEGER DEFAULT 0,

    CONSTRAINT fk_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);

-- Indexes for scheduled_tasks
CREATE INDEX IF NOT EXISTS idx_scheduled_tasks_user_id ON scheduled_tasks(user_id);
CREATE INDEX IF NOT EXISTS idx_scheduled_tasks_tenant_id ON scheduled_tasks(tenant_id) WHERE tenant_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_scheduled_tasks_status ON scheduled_tasks(status) WHERE status != 'DELETED';
CREATE INDEX IF NOT EXISTS idx_scheduled_tasks_temporal_id ON scheduled_tasks(temporal_schedule_id);
CREATE INDEX IF NOT EXISTS idx_scheduled_tasks_next_run ON scheduled_tasks(next_run_at) WHERE status = 'ACTIVE';

-- ============================================================================
-- Part 3: Schedule execution linking table (thin reference only)
-- ============================================================================
-- This table links schedule runs to task_executions for quick lookups.
-- All execution details (status, cost, tokens, result) are in task_executions.
-- Query execution history via: JOIN task_executions ON workflow_id = task_id

CREATE TABLE IF NOT EXISTS scheduled_task_executions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    -- Links to schedule configuration
    schedule_id UUID NOT NULL REFERENCES scheduled_tasks(id) ON DELETE CASCADE,

    -- Links to unified task execution record (workflow_id in task_executions)
    task_id VARCHAR(255) NOT NULL,

    -- Timestamp for ordering
    triggered_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),

    CONSTRAINT uq_schedule_task_execution UNIQUE (schedule_id, task_id)
);

-- Indexes for scheduled_task_executions
CREATE INDEX IF NOT EXISTS idx_scheduled_executions_schedule_id ON scheduled_task_executions(schedule_id);
CREATE INDEX IF NOT EXISTS idx_scheduled_executions_task_id ON scheduled_task_executions(task_id);
CREATE INDEX IF NOT EXISTS idx_scheduled_executions_triggered_at ON scheduled_task_executions(triggered_at DESC);

-- ============================================================================
-- Part 4: Add FK from task_executions to scheduled_tasks
-- ============================================================================

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.table_constraints
        WHERE constraint_name = 'fk_task_executions_schedule'
    ) THEN
        ALTER TABLE task_executions
            ADD CONSTRAINT fk_task_executions_schedule
            FOREIGN KEY (schedule_id) REFERENCES scheduled_tasks(id) ON DELETE SET NULL;
    END IF;
END $$;

-- ============================================================================
-- Part 5: Convenience view for schedule execution history
-- ============================================================================

CREATE OR REPLACE VIEW v_schedule_execution_history AS
SELECT
    ste.schedule_id,
    st.name AS schedule_name,
    st.user_id,
    st.tenant_id,
    te.workflow_id,
    te.session_id,
    te.query,
    te.status,
    te.result,
    te.error_message,
    te.total_tokens,
    te.total_cost_usd,
    te.model_used,
    te.provider,
    te.started_at,
    te.completed_at,
    te.duration_ms,
    te.trigger_type,
    ste.triggered_at
FROM scheduled_task_executions ste
JOIN scheduled_tasks st ON st.id = ste.schedule_id
LEFT JOIN task_executions te ON te.workflow_id = ste.task_id
ORDER BY ste.triggered_at DESC;
