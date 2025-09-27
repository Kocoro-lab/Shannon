-- Migration 006: Add tables for enhanced supervisor memory system
-- This migration creates tables for tracking decomposition patterns, strategy performance,
-- and user preferences to enable intelligent task decomposition

-- Table for decomposition patterns (tracks how queries were decomposed)
CREATE TABLE IF NOT EXISTS decomposition_patterns (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id VARCHAR(255) NOT NULL,
    user_id VARCHAR(255),
    tenant_id VARCHAR(255),
    query_pattern TEXT NOT NULL,
    subtasks JSONB NOT NULL, -- Array of subtask descriptions
    strategy VARCHAR(100), -- 'parallel', 'sequential', 'hierarchical', 'iterative'
    success_rate DOUBLE PRECISION DEFAULT 0,
    avg_duration_ms BIGINT DEFAULT 0,
    total_runs INTEGER DEFAULT 1,
    tokens_used INTEGER DEFAULT 0,
    error_message TEXT,
    metadata JSONB DEFAULT '{}',
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    last_used TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- Indexes for decomposition patterns
CREATE INDEX idx_decomposition_patterns_session_id ON decomposition_patterns(session_id);
CREATE INDEX idx_decomposition_patterns_user_id ON decomposition_patterns(user_id);
CREATE INDEX idx_decomposition_patterns_strategy ON decomposition_patterns(strategy);
CREATE INDEX idx_decomposition_patterns_success_rate ON decomposition_patterns(success_rate);
CREATE INDEX idx_decomposition_patterns_last_used ON decomposition_patterns(last_used);

-- Table for strategy performance metrics (aggregated from agent_executions)
CREATE TABLE IF NOT EXISTS strategy_performance (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id VARCHAR(255) NOT NULL,
    tenant_id VARCHAR(255),
    strategy VARCHAR(100) NOT NULL,
    total_runs INTEGER DEFAULT 0,
    successful_runs INTEGER DEFAULT 0,
    failed_runs INTEGER DEFAULT 0,
    success_rate DOUBLE PRECISION DEFAULT 0,
    avg_duration_ms BIGINT DEFAULT 0,
    avg_tokens_used INTEGER DEFAULT 0,
    avg_cost_usd DOUBLE PRECISION DEFAULT 0,
    metadata JSONB DEFAULT '{}',
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(user_id, strategy)
);

-- Indexes for strategy performance
CREATE INDEX idx_strategy_performance_user_id ON strategy_performance(user_id);
CREATE INDEX idx_strategy_performance_strategy ON strategy_performance(strategy);
CREATE INDEX idx_strategy_performance_success_rate ON strategy_performance(success_rate);

-- Table for team composition memories (which agent combinations work well)
CREATE TABLE IF NOT EXISTS team_compositions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id VARCHAR(255) NOT NULL,
    user_id VARCHAR(255),
    tenant_id VARCHAR(255),
    task_type VARCHAR(255) NOT NULL, -- e.g., 'code_review', 'data_analysis', 'research'
    agent_roles JSONB NOT NULL, -- Array of agent roles used
    coordination_mode VARCHAR(100), -- 'parallel', 'sequential', 'hybrid'
    performance_score DOUBLE PRECISION DEFAULT 0,
    tokens_used INTEGER DEFAULT 0,
    duration_ms BIGINT DEFAULT 0,
    success BOOLEAN DEFAULT TRUE,
    metadata JSONB DEFAULT '{}',
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- Indexes for team compositions
CREATE INDEX idx_team_compositions_session_id ON team_compositions(session_id);
CREATE INDEX idx_team_compositions_user_id ON team_compositions(user_id);
CREATE INDEX idx_team_compositions_task_type ON team_compositions(task_type);
CREATE INDEX idx_team_compositions_performance_score ON team_compositions(performance_score);

-- Table for failure patterns (learn from what doesn't work)
CREATE TABLE IF NOT EXISTS failure_patterns (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    pattern_name VARCHAR(255) NOT NULL,
    pattern_description TEXT,
    indicators JSONB NOT NULL, -- Array of strings that indicate this pattern
    mitigation_strategy TEXT NOT NULL,
    occurrence_count INTEGER DEFAULT 1,
    last_occurred TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    severity VARCHAR(50) DEFAULT 'medium', -- 'low', 'medium', 'high', 'critical'
    metadata JSONB DEFAULT '{}',
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(pattern_name)
);

-- Indexes for failure patterns
CREATE INDEX idx_failure_patterns_pattern_name ON failure_patterns(pattern_name);
CREATE INDEX idx_failure_patterns_severity ON failure_patterns(severity);
CREATE INDEX idx_failure_patterns_last_occurred ON failure_patterns(last_occurred);

-- Table for user preferences (inferred from interactions)
CREATE TABLE IF NOT EXISTS user_preferences (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id VARCHAR(255) NOT NULL,
    tenant_id VARCHAR(255),
    expertise_level VARCHAR(50) DEFAULT 'intermediate', -- 'beginner', 'intermediate', 'expert'
    preferred_style VARCHAR(50) DEFAULT 'balanced', -- 'detailed', 'concise', 'balanced', 'educational'
    domain_focus JSONB DEFAULT '[]', -- Array of domains like ['ml', 'web', 'data']
    speed_vs_accuracy DOUBLE PRECISION DEFAULT 0.7, -- 0.0 (speed) to 1.0 (accuracy)
    avg_query_complexity DOUBLE PRECISION DEFAULT 5.0,
    avg_response_length INTEGER DEFAULT 1000,
    interaction_count INTEGER DEFAULT 0,
    metadata JSONB DEFAULT '{}',
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(user_id)
);

-- Indexes for user preferences
CREATE INDEX idx_user_preferences_user_id ON user_preferences(user_id);
CREATE INDEX idx_user_preferences_expertise_level ON user_preferences(expertise_level);

-- Add strategy column to agent_executions if it doesn't exist
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns
                   WHERE table_name = 'agent_executions'
                   AND column_name = 'strategy') THEN
        ALTER TABLE agent_executions ADD COLUMN strategy VARCHAR(100);
        CREATE INDEX idx_agent_executions_strategy ON agent_executions(strategy);
    END IF;
END $$;

-- Insert default failure patterns
INSERT INTO failure_patterns (pattern_name, pattern_description, indicators, mitigation_strategy, severity)
VALUES
    ('rate_limit', 'Task may trigger rate limiting', '["quickly", "fast", "urgent", "asap", "immediately"]', 'Consider sequential execution to avoid rate limits', 'high'),
    ('context_overflow', 'Task may exceed context window', '["analyze", "review", "entire codebase", "all files", "everything"]', 'Break down into smaller, focused subtasks', 'high'),
    ('ambiguous_request', 'Request lacks clear requirements', '["something", "somehow", "maybe", "probably", "i think"]', 'Clarify requirements before decomposition', 'medium')
ON CONFLICT (pattern_name) DO NOTHING;

-- Create update timestamp trigger
CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = CURRENT_TIMESTAMP;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Add triggers for updated_at columns
CREATE TRIGGER update_decomposition_patterns_updated_at BEFORE UPDATE ON decomposition_patterns
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER update_strategy_performance_updated_at BEFORE UPDATE ON strategy_performance
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER update_failure_patterns_updated_at BEFORE UPDATE ON failure_patterns
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER update_user_preferences_updated_at BEFORE UPDATE ON user_preferences
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();