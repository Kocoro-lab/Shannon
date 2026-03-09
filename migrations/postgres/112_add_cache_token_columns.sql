-- Migration 112: Add prompt cache token tracking columns
-- Tracks Anthropic prompt caching (cache_read_tokens = cache hits, cache_creation_tokens = cache writes)

-- token_usage table (per-LLM-call detail rows written by BudgetManager)
ALTER TABLE token_usage ADD COLUMN IF NOT EXISTS cache_read_tokens INTEGER DEFAULT 0;
ALTER TABLE token_usage ADD COLUMN IF NOT EXISTS cache_creation_tokens INTEGER DEFAULT 0;

-- task_executions table (task-level aggregates)
ALTER TABLE task_executions ADD COLUMN IF NOT EXISTS cache_read_tokens INTEGER DEFAULT 0;
ALTER TABLE task_executions ADD COLUMN IF NOT EXISTS cache_creation_tokens INTEGER DEFAULT 0;
