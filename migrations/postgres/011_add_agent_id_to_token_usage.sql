-- Add agent_id to token_usage for per-agent attribution

ALTER TABLE token_usage
    ADD COLUMN IF NOT EXISTS agent_id VARCHAR(255);

CREATE INDEX IF NOT EXISTS idx_token_usage_agent_id ON token_usage(agent_id);
