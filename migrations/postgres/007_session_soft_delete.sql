-- Soft delete columns for sessions
ALTER TABLE sessions
  ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ DEFAULT NULL,
  ADD COLUMN IF NOT EXISTS deleted_by UUID DEFAULT NULL;

-- Partial index to speed lookups of deleted rows
CREATE INDEX IF NOT EXISTS idx_sessions_deleted_at
  ON sessions(deleted_at)
  WHERE deleted_at IS NOT NULL;

-- Index for session_id in task_executions (improves ListSessions performance)
CREATE INDEX IF NOT EXISTS idx_task_executions_session_id
  ON task_executions(session_id);

COMMENT ON COLUMN sessions.deleted_at IS 'Timestamp when session was soft-deleted';
COMMENT ON COLUMN sessions.deleted_by IS 'User who deleted the session (UUID)';

