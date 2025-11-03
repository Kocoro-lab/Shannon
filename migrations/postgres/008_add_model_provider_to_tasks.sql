-- Migration: Add model_used and provider columns to task_executions
-- Version: 008
-- Description: Add model and provider tracking to task-level execution records

-- Add model_used column to track which model was primarily used for the task
ALTER TABLE task_executions
    ADD COLUMN IF NOT EXISTS model_used VARCHAR(100);

-- Add provider column to track which provider (openai, anthropic, etc.) was used
ALTER TABLE task_executions
    ADD COLUMN IF NOT EXISTS provider VARCHAR(50);

-- Create index for model-based queries (analytics, cost tracking)
CREATE INDEX IF NOT EXISTS idx_task_executions_model ON task_executions(model_used);
CREATE INDEX IF NOT EXISTS idx_task_executions_provider ON task_executions(provider);

-- Add comment for documentation
COMMENT ON COLUMN task_executions.model_used IS 'Primary model used for task execution (e.g., gpt-5-nano-2025-08-07, claude-sonnet-4-5-20250929)';
COMMENT ON COLUMN task_executions.provider IS 'Provider name (e.g., openai, anthropic)';
