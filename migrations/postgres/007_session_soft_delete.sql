-- Migration: Session soft delete and dual-ID support improvements
-- Purpose: Add soft delete functionality and optimize session lookups for dual-ID pattern (UUID + external string IDs)

-- =========================================
-- PART 1: Soft delete columns for sessions
-- =========================================
ALTER TABLE sessions
  ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ DEFAULT NULL,
  ADD COLUMN IF NOT EXISTS deleted_by UUID DEFAULT NULL;

-- Partial index to speed lookups of deleted rows
CREATE INDEX IF NOT EXISTS idx_sessions_deleted_at
  ON sessions(deleted_at)
  WHERE deleted_at IS NOT NULL;

COMMENT ON COLUMN sessions.deleted_at IS 'Timestamp when session was soft-deleted';
COMMENT ON COLUMN sessions.deleted_by IS 'User who deleted the session (UUID)';

-- =========================================
-- PART 2: Dual-ID pattern support indexes
-- =========================================
-- Add functional index for external_id lookups
-- This enables efficient queries with: WHERE context->>'external_id' = $1
CREATE INDEX IF NOT EXISTS idx_sessions_external_id
  ON sessions ((context->>'external_id'))
  WHERE context->>'external_id' IS NOT NULL;

-- Add partial unique constraint to prevent duplicate external_id per user
-- This ensures one external_id can only map to one session per user
CREATE UNIQUE INDEX IF NOT EXISTS idx_sessions_user_external_id
  ON sessions (user_id, (context->>'external_id'))
  WHERE context->>'external_id' IS NOT NULL AND deleted_at IS NULL;

-- Add filtered index for non-deleted sessions to improve WHERE deleted_at IS NULL queries
CREATE INDEX IF NOT EXISTS idx_sessions_not_deleted
  ON sessions (id)
  WHERE deleted_at IS NULL;

-- Index for session_id in task_executions (improves ListSessions performance)
CREATE INDEX IF NOT EXISTS idx_task_executions_session_id
  ON task_executions(session_id);

-- =========================================
-- PART 3: Add comments for documentation
-- =========================================
COMMENT ON INDEX idx_sessions_external_id IS 'Enables efficient lookups for non-UUID session IDs stored in context->external_id';
COMMENT ON INDEX idx_sessions_user_external_id IS 'Ensures external_id uniqueness per user for active sessions';
COMMENT ON INDEX idx_sessions_not_deleted IS 'Optimizes queries filtering for active (non-deleted) sessions';
COMMENT ON INDEX idx_task_executions_session_id IS 'Improves task lookup performance by session_id';
