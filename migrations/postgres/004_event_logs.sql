-- Shannon Platform - Event Logs for Audit Trail
-- Migration: 004_event_logs.sql
-- Purpose: Store streaming events permanently for audit and historical replay

CREATE TABLE IF NOT EXISTS event_logs (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    workflow_id VARCHAR(255) NOT NULL,
    task_id UUID,  -- Nullable, no foreign key as it's not used in the application
    type VARCHAR(100) NOT NULL,
    agent_id VARCHAR(255),
    message TEXT,
    payload JSONB DEFAULT '{}',
    timestamp TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    seq BIGINT,
    stream_id VARCHAR(64),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Indexes for efficient querying
CREATE INDEX IF NOT EXISTS idx_event_logs_workflow_id ON event_logs(workflow_id);
CREATE INDEX IF NOT EXISTS idx_event_logs_task_id ON event_logs(task_id);
CREATE INDEX IF NOT EXISTS idx_event_logs_type ON event_logs(type);
CREATE INDEX IF NOT EXISTS idx_event_logs_ts ON event_logs(timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_event_logs_seq ON event_logs(workflow_id, seq);
CREATE INDEX IF NOT EXISTS idx_event_logs_workflow_ts ON event_logs(workflow_id, timestamp DESC);

-- Add response column to tasks table if it doesn't exist
-- This stores the final task response for easy retrieval
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'tasks' AND column_name = 'response'
    ) THEN
        ALTER TABLE tasks ADD COLUMN response JSONB DEFAULT '{}';
        CREATE INDEX idx_tasks_response ON tasks USING GIN (response);
    END IF;
END $$;

COMMENT ON TABLE event_logs IS 'Persistent copy of streaming events for audit/history and UI replay';
COMMENT ON COLUMN event_logs.workflow_id IS 'Temporal workflow ID or task identifier';
COMMENT ON COLUMN event_logs.type IS 'Event type (e.g., TASK_STARTED, AGENT_ASSIGNED, TOOL_CALLED)';
COMMENT ON COLUMN event_logs.seq IS 'Sequential number for event ordering within a workflow';
COMMENT ON COLUMN event_logs.stream_id IS 'Redis stream message ID for correlation';

-- Prevent duplicate timeline inserts when seq (Temporal event id) is present
CREATE UNIQUE INDEX IF NOT EXISTS uq_event_logs_wf_type_seq
  ON event_logs (workflow_id, type, seq)
  WHERE seq IS NOT NULL;
