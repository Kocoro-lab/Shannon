-- Shannon Authentication System Migration
-- Version: 003
-- Description: Adds user authentication, API keys, and multi-tenancy support

-- Enable required extensions
CREATE EXTENSION IF NOT EXISTS "pgcrypto"; -- For gen_random_uuid()

-- Create authentication schema
CREATE SCHEMA IF NOT EXISTS auth;

-- Multi-tenancy support
CREATE TABLE IF NOT EXISTS auth.tenants (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(255) NOT NULL,
    slug VARCHAR(100) UNIQUE NOT NULL,
    plan VARCHAR(50) DEFAULT 'free', -- free, pro, enterprise
    token_limit INTEGER DEFAULT 10000,
    monthly_token_usage INTEGER DEFAULT 0,
    rate_limit_per_hour INTEGER DEFAULT 1000,
    is_active BOOLEAN DEFAULT true,
    created_at TIMESTAMP DEFAULT NOW(),
    updated_at TIMESTAMP DEFAULT NOW(),
    metadata JSONB DEFAULT '{}'::jsonb
);

-- Core user management
CREATE TABLE IF NOT EXISTS auth.users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email VARCHAR(255) UNIQUE NOT NULL,
    username VARCHAR(100) UNIQUE NOT NULL,
    password_hash VARCHAR(255) NOT NULL,
    full_name VARCHAR(255),
    tenant_id UUID NOT NULL REFERENCES auth.tenants(id) ON DELETE CASCADE,
    role VARCHAR(50) DEFAULT 'user', -- user, admin, owner
    is_active BOOLEAN DEFAULT true,
    is_verified BOOLEAN DEFAULT false,
    email_verified_at TIMESTAMP,
    created_at TIMESTAMP DEFAULT NOW(),
    updated_at TIMESTAMP DEFAULT NOW(),
    last_login TIMESTAMP,
    metadata JSONB DEFAULT '{}'::jsonb
);

-- API Keys for programmatic access
CREATE TABLE IF NOT EXISTS auth.api_keys (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    key_hash VARCHAR(255) UNIQUE NOT NULL, -- Store hashed version
    key_prefix VARCHAR(20) NOT NULL, -- First 8 chars for identification
    user_id UUID REFERENCES auth.users(id) ON DELETE CASCADE,
    tenant_id UUID NOT NULL REFERENCES auth.tenants(id) ON DELETE CASCADE,
    name VARCHAR(100) NOT NULL,
    description TEXT,
    scopes TEXT[] DEFAULT ARRAY['workflows:read', 'workflows:write', 'agents:execute'],
    rate_limit_per_hour INTEGER DEFAULT 1000,
    last_used TIMESTAMP,
    expires_at TIMESTAMP,
    is_active BOOLEAN DEFAULT true,
    created_at TIMESTAMP DEFAULT NOW(),
    metadata JSONB DEFAULT '{}'::jsonb
);

-- JWT refresh tokens for revocation
CREATE TABLE IF NOT EXISTS auth.refresh_tokens (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    token_hash VARCHAR(255) UNIQUE NOT NULL,
    user_id UUID REFERENCES auth.users(id) ON DELETE CASCADE,
    tenant_id UUID NOT NULL REFERENCES auth.tenants(id) ON DELETE CASCADE,
    expires_at TIMESTAMP NOT NULL,
    revoked BOOLEAN DEFAULT false,
    revoked_at TIMESTAMP,
    ip_address INET,
    user_agent TEXT,
    created_at TIMESTAMP DEFAULT NOW()
);

-- Audit log for security events
CREATE TABLE IF NOT EXISTS auth.audit_logs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    event_type VARCHAR(100) NOT NULL, -- login, logout, api_key_created, permission_changed, etc.
    user_id UUID REFERENCES auth.users(id) ON DELETE SET NULL,
    tenant_id UUID REFERENCES auth.tenants(id) ON DELETE SET NULL,
    ip_address INET,
    user_agent TEXT,
    details JSONB DEFAULT '{}'::jsonb,
    created_at TIMESTAMP DEFAULT NOW()
);

-- Update existing tables to include tenant_id
ALTER TABLE task_executions ADD COLUMN IF NOT EXISTS tenant_id UUID;
ALTER TABLE sessions ADD COLUMN IF NOT EXISTS tenant_id UUID;

-- Create indexes for performance
CREATE INDEX IF NOT EXISTS idx_users_tenant_id ON auth.users(tenant_id);
CREATE INDEX IF NOT EXISTS idx_users_email ON auth.users(email);
CREATE INDEX IF NOT EXISTS idx_api_keys_tenant_id ON auth.api_keys(tenant_id);
CREATE INDEX IF NOT EXISTS idx_api_keys_key_prefix ON auth.api_keys(key_prefix);
CREATE INDEX IF NOT EXISTS idx_refresh_tokens_user_id ON auth.refresh_tokens(user_id);
CREATE INDEX IF NOT EXISTS idx_refresh_tokens_token_hash ON auth.refresh_tokens(token_hash);
CREATE INDEX IF NOT EXISTS idx_audit_logs_user_id ON auth.audit_logs(user_id);
CREATE INDEX IF NOT EXISTS idx_audit_logs_tenant_id ON auth.audit_logs(tenant_id);
CREATE INDEX IF NOT EXISTS idx_audit_logs_event_type ON auth.audit_logs(event_type);
CREATE INDEX IF NOT EXISTS idx_audit_logs_created_at ON auth.audit_logs(created_at);
CREATE INDEX IF NOT EXISTS idx_task_executions_tenant_id ON task_executions(tenant_id);
CREATE INDEX IF NOT EXISTS idx_sessions_tenant_id ON sessions(tenant_id);

-- Default tenant for development
INSERT INTO auth.tenants (id, name, slug, plan, token_limit, rate_limit_per_hour) 
VALUES ('00000000-0000-0000-0000-000000000001', 'Development', 'dev', 'enterprise', 1000000, 10000)
ON CONFLICT (slug) DO NOTHING;

-- Default admin user (password: changeme123!)
-- Note: In production, this should be created via secure CLI tool
INSERT INTO auth.users (
    id, 
    email, 
    username, 
    password_hash, 
    full_name, 
    tenant_id,
    role,
    is_active,
    is_verified
) VALUES (
    '00000000-0000-0000-0000-000000000002',
    'admin@shannon.local',
    'admin',
    '$2a$10$YKqPuFGEs.8EEQgRXzPKPuHj9vJHGNFtWwYoI6QQHBwUJ7cRwMkYG', -- bcrypt hash of "changeme123!"
    'Shannon Admin',
    '00000000-0000-0000-0000-000000000001',
    'owner',
    true,
    true
) ON CONFLICT (email) DO NOTHING;

-- Create trigger to update updated_at timestamp
CREATE OR REPLACE FUNCTION auth.update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ language 'plpgsql';

CREATE TRIGGER update_tenants_updated_at BEFORE UPDATE ON auth.tenants
    FOR EACH ROW EXECUTE FUNCTION auth.update_updated_at_column();

CREATE TRIGGER update_users_updated_at BEFORE UPDATE ON auth.users
    FOR EACH ROW EXECUTE FUNCTION auth.update_updated_at_column();

-- Grant necessary permissions
GRANT USAGE ON SCHEMA auth TO shannon;
GRANT ALL PRIVILEGES ON ALL TABLES IN SCHEMA auth TO shannon;
GRANT ALL PRIVILEGES ON ALL SEQUENCES IN SCHEMA auth TO shannon;