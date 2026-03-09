-- Migration: Add indexes for session context JSON fields
-- These indexes improve query performance when filtering sessions by context fields

-- Index for external_id lookups (used when mapping external session IDs)
CREATE INDEX IF NOT EXISTS idx_sessions_context_external_id 
ON sessions ((context->>'external_id')) 
WHERE context->>'external_id' IS NOT NULL;

-- Index for role-based session queries
CREATE INDEX IF NOT EXISTS idx_sessions_context_role 
ON sessions ((context->>'role')) 
WHERE context->>'role' IS NOT NULL;

-- Index for research session filtering
CREATE INDEX IF NOT EXISTS idx_sessions_context_force_research 
ON sessions ((context->>'force_research')) 
WHERE context->>'force_research' IS NOT NULL;

-- Index for first_task_mode queries (used in session listings)
CREATE INDEX IF NOT EXISTS idx_sessions_context_first_task_mode 
ON sessions ((context->>'first_task_mode')) 
WHERE context->>'first_task_mode' IS NOT NULL;

-- GIN index for general JSONB containment queries on context
-- This is useful for flexible queries like: WHERE context @> '{"role": "ads_researcher"}'
CREATE INDEX IF NOT EXISTS idx_sessions_context_gin 
ON sessions USING GIN (context jsonb_path_ops);

