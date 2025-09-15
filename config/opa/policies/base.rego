package shannon.task

# Shannon Task Execution Policy - Phase 0.5 Basic Allowlist
# This implements basic security controls for Shannon AI agent task execution
# Mode: allowlist-first approach with environment-specific rules

import future.keywords.in

# Default decision when no specific allow rules match
default decision := {
    "allow": false,
    "reason": "default deny - no matching allow rule",
    "require_approval": false
}

# === DENY PRECEDENCE RULES ===
# Deny rules always take precedence over allow rules
# If any deny rule matches, the request is denied regardless of allows

decision := {
    "allow": false,
    "reason": reason,
    "require_approval": false
} {
    some reason
    deny[reason]
}

# === DEVELOPMENT ENVIRONMENT RULES ===
# Allow all operations in development environment (for testing and development)
decision := {
    "allow": true,
    "reason": "development environment - all operations allowed",
    "require_approval": false
} {
    input.environment == "dev"
    input.token_budget <= 10000  # Still enforce reasonable budget limits
}

# === PRODUCTION SAFETY RULES ===

# Simple mode operations - generally safe, minimal resources
decision := {
    "allow": true,
    "reason": "simple mode operation - low risk",
    "require_approval": false
} {
    input.mode == "simple"
    input.token_budget <= 1000
    safe_query_check
}

# Standard operations for authenticated users
decision := {
    "allow": true,
    "reason": "standard operation for authorized user",
    "require_approval": false
} {
    input.mode == "standard"
    input.user_id != ""
    allowed_users[input.user_id]
    input.token_budget <= 5000
    not suspicious_query
}

# Complex operations - require approval in production
decision := {
    "allow": true,
    "reason": "complex operation approved for privileged user",
    "require_approval": input.environment == "prod"
} {
    input.mode == "complex"
    privileged_users[input.user_id]
    input.token_budget <= 15000
    not dangerous_query
}

# === USER ALLOWLISTS ===

# Basic approved users for standard operations
# TODO: Customize these user IDs for your organization
allowed_users := {
    "admin",
    "test_user", 
    "orchestrator",
    "shannon_system",
    "api_user",
    "wayland",           # Add your username
    "dev_team_lead",
    "data_scientist",
    "product_manager"
}

# Privileged users who can perform complex operations
# TODO: Restrict to actual senior staff in production
privileged_users := {
    "admin",
    "shannon_system", 
    "wayland",           # Add your username
    "senior_engineer",
    "team_lead",
    "security_admin"
}

# === QUERY PATTERN CONTROLS ===

# Safe query check - contains safe patterns
safe_query_check {
    count([pattern | 
        safe_patterns[pattern]
        contains(lower(input.query), pattern)
    ]) > 0
}

# Check for suspicious queries
suspicious_query {
    count([pattern | 
        suspicious_patterns[pattern]
        contains(lower(input.query), pattern)
    ]) > 0
}

# Check for dangerous queries
dangerous_query {
    count([pattern | 
        dangerous_patterns[pattern]
        contains(lower(input.query), pattern)
    ]) > 0
}

# Safe query patterns that are always allowed
safe_patterns := {
    "what is",
    "how to",
    "explain",
    "help me understand",
    "summarize",
    "translate",
    "calculate",
    "convert"
}

# Suspicious patterns that require extra scrutiny
suspicious_patterns := {
    "delete",
    "remove",
    "hack",
    "bypass",
    "override",
    "admin",
    "root",
    "sudo"
}

# Dangerous patterns that are blocked in production
# TODO: Add your organization's sensitive patterns
dangerous_patterns := {
    # System commands
    "rm -rf",
    "format disk", 
    "drop table",
    "truncate table",
    "delete from",
    "shutdown",
    "reboot",
    "kill -9",
    
    # File system access
    "/etc/passwd",
    "/etc/shadow", 
    "~/.ssh",
    "id_rsa",
    "private key",
    
    # Sensitive data patterns
    "credit card",
    "social security",
    "ssn",
    "password",
    "api key",
    "secret token",
    "database connection",
    
    # Company-specific (customize for your org)
    "ptmind internal",
    "company database",
    "production server",
    "admin panel"
}

# === BUDGET ENFORCEMENT ===

# Block requests that exceed maximum token budgets per mode
decision := {
    "allow": false,
    "reason": sprintf("token budget %d exceeds maximum %d for mode %s", [input.token_budget, max_budgets[input.mode], input.mode])
} {
    max_budgets[input.mode] < input.token_budget
}

# Maximum token budgets per execution mode
max_budgets := {
    "simple": 1000,
    "standard": 5000,
    "complex": 15000
}

# === AGENT ALLOWLIST ===

# Allow specific agent IDs
decision := {
    "allow": true,
    "reason": "approved agent ID",
    "require_approval": false
} {
    allowed_agent_ids[input.agent_id]
    input.token_budget <= max_budgets[input.mode]
}

# Approved agent IDs
allowed_agent_ids := {
    "agent-core",
    "llm-agent",
    "synthesis-agent", 
    "complexity-agent",
    "orchestrator-agent"
}

# === DENY RULES (OVERRIDE ALL ALLOWS) ===

# Deny dangerous query patterns regardless of other rules
deny[sprintf("dangerous pattern detected: %s", [pattern])] {
    some pattern
    dangerous_patterns[pattern]
    contains(lower(input.query), pattern)
}

# Deny excessive token budgets
deny[sprintf("token budget %d exceeds system maximum %d", [input.token_budget, system_limits.max_tokens])] {
    input.token_budget > system_limits.max_tokens
}

# Deny blocked users
deny[sprintf("user %s is blocked", [input.user_id])] {
    blocked_users[input.user_id]
}

# Deny production operations from unknown users
deny["unknown user in production environment"] {
    input.environment == "prod"
    input.user_id != ""
    not allowed_users[input.user_id]
    not privileged_users[input.user_id]
}

# === SYSTEM LIMITS ===

system_limits := {
    "max_tokens": 50000,
    "max_concurrent_requests": 20,
    "max_session_duration": 7200  # 2 hours
}

# Blocked users (security enforcement)
blocked_users := {
    "blocked_user",
    "suspended_account",
    "test_blocked"
}# Test comment to trigger .rego reload

# Another test change Mon Sep  8 23:49:03 JST 2025
