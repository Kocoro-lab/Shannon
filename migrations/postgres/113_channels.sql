-- Migration 113: Channels registry for daemon command channel.
-- Supports routing external messages (Slack, LINE) to CLI daemons.

CREATE TABLE IF NOT EXISTS channels (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         UUID,
    type            TEXT NOT NULL,
    name            TEXT NOT NULL,
    credentials     JSONB NOT NULL DEFAULT '{}',
    config          JSONB NOT NULL DEFAULT '{}',
    enabled         BOOLEAN NOT NULL DEFAULT true,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Unique constraint: per-user, per-type, per-name
CREATE UNIQUE INDEX IF NOT EXISTS idx_channels_unique
    ON channels (COALESCE(user_id, '00000000-0000-0000-0000-000000000000'), type, name);

CREATE INDEX IF NOT EXISTS idx_channels_user ON channels (user_id) WHERE user_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_channels_type ON channels (type);

COMMENT ON TABLE channels IS 'Channel registry for daemon command channel — stores bot credentials';
COMMENT ON COLUMN channels.credentials IS 'Bot credentials (signing secrets, tokens). Never exposed via API.';
COMMENT ON COLUMN channels.config IS 'Channel config: agent_name, claim_timeout_seconds, etc.';
