-- Create test API key for gateway authentication
-- This script sets up a test API key 'sk_test_123456' using the default admin user

-- First, ensure default tenant and admin user exist (from migrations)
DO $$
BEGIN
    -- Check if default tenant exists
    IF NOT EXISTS (SELECT 1 FROM auth.tenants WHERE id = '00000000-0000-0000-0000-000000000001') THEN
        RAISE EXCEPTION 'Default tenant not found. Run migrations first.';
    END IF;

    -- Check if default admin user exists
    IF NOT EXISTS (SELECT 1 FROM auth.users WHERE id = '00000000-0000-0000-0000-000000000002') THEN
        RAISE EXCEPTION 'Default admin user not found. Run migrations first.';
    END IF;
END $$;

-- Delete any existing test API keys
DELETE FROM auth.api_keys WHERE key_prefix = 'sk_test_';

-- Create the test API key
-- Key: sk_test_123456
-- SHA256 Hash: 58a5e18e57cb4cf83cf8e4e1d420958e9297c3502468d2e33b5052b0f46cb640
INSERT INTO auth.api_keys (
    id,
    key_hash,
    key_prefix,
    user_id,
    tenant_id,
    name,
    description,
    scopes,
    rate_limit_per_hour,
    is_active,
    created_at
) VALUES (
    gen_random_uuid(),
    '58a5e18e57cb4cf83cf8e4e1d420958e9297c3502468d2e33b5052b0f46cb640',
    'sk_test_',
    '00000000-0000-0000-0000-000000000002'::uuid,  -- Default admin user
    '00000000-0000-0000-0000-000000000001'::uuid,  -- Default tenant
    'Test API Key',
    'API key for testing gateway authentication',
    ARRAY['workflows:read', 'workflows:write', 'agents:execute', 'sessions:read', 'sessions:write']::text[],
    1000,
    true,
    NOW()
);

-- Verify the setup
SELECT
    '=== API Key Created ===' as info
UNION ALL
SELECT
    'Key to use: sk_test_123456'
UNION ALL
SELECT
    'Prefix: ' || key_prefix
FROM auth.api_keys
WHERE key_prefix = 'sk_test_'
UNION ALL
SELECT
    'User: ' || u.email
FROM auth.api_keys k
JOIN auth.users u ON k.user_id = u.id
WHERE k.key_prefix = 'sk_test_'
UNION ALL
SELECT
    'Active: ' || is_active::text
FROM auth.api_keys
WHERE key_prefix = 'sk_test_';

-- Detailed verification
\echo '\nDetailed API Key Info:'
SELECT
    key_prefix,
    substring(key_hash, 1, 20) as hash_preview,
    name,
    is_active,
    array_to_string(scopes, ', ') as scopes
FROM auth.api_keys
WHERE key_prefix = 'sk_test_';

-- Verify hash matches what we expect
SELECT
    CASE
        WHEN key_hash = '58a5e18e57cb4cf83cf8e4e1d420958e9297c3502468d2e33b5052b0f46cb640'
        THEN '✓ Hash verification: PASS'
        ELSE '✗ Hash verification: FAIL'
    END as verification
FROM auth.api_keys
WHERE key_prefix = 'sk_test_';