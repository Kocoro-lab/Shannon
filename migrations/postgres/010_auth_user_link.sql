-- Migration: Add explicit link from public.users to auth.users
-- Version: 010
-- Description: Adds auth_user_id foreign key, backfills from matching IDs where IDs are intentionally aligned,
--              and enforces uniqueness. WARNING: This assumes deployments that create auth.users rows also
--              use the same UUID when creating public.users rows (current orchestrator behavior).

ALTER TABLE users
    ADD COLUMN IF NOT EXISTS auth_user_id UUID REFERENCES auth.users(id);

-- Backfill for users that already share the same UUID between auth.users and users
UPDATE users
SET auth_user_id = id
WHERE auth_user_id IS NULL
  AND id IN (SELECT id FROM auth.users);

-- Ensure one-to-one mapping for authenticated users while allowing anonymous rows
CREATE UNIQUE INDEX IF NOT EXISTS idx_users_auth_user_id
    ON users(auth_user_id)
    WHERE auth_user_id IS NOT NULL;
