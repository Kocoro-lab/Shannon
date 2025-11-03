package shannon.task

import future.keywords.if
import future.keywords.in

# Customer Support team policy rules

# Only allow mini models for support team
allow_model(model) if {
    input.context.team == "support"
    model in ["gpt-5-mini-2025-08-07", "claude-haiku-4-5-20251001"]
}

# Limited token budget for support team
max_tokens := 5000 if {
    input.context.team == "support"
}

# Deny dangerous tools for support team
deny_tool(tool) if {
    input.context.team == "support"
    tool in ["database_write", "code_execution", "system_command"]
}

# Allow safe tools only
allow_tool(tool) if {
    input.context.team == "support"
    not deny_tool(tool)
    tool in ["web_search", "database_read", "knowledge_base"]
}

# Decision for support team
decision := {
    "allow": true,
    "reason": "Support team has limited access",
    "obligations": {
        "max_tokens": 5000,
        "allowed_models": ["gpt-5-mini-2025-08-07", "claude-haiku-4-5-20251001"],
        "tool_restrictions": ["database_write", "code_execution", "system_command"]
    }
} if {
    input.context.team == "support"
    input.mode in ["simple", "standard"]
}
