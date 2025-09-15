package shannon.task

# Security policies - these can override base allow rules

# Production restrictions
deny[{"reason": "production environment requires explicit approval for complex tasks"}] {
	input.environment == "prod"
	input.mode == "complex"
	not approved_complex_task
}

deny[{"reason": "high complexity score requires approval"}] {
	input.complexity_score > 0.9
	not approved_high_complexity
}

deny[{"reason": "suspicious session pattern detected"}] {
	suspicious_session
}

# Security checks
approved_complex_task {
	# This would be set by human intervention workflow
	input.context.approved_by_human == true
}

approved_high_complexity {
	# This would be set by human intervention workflow  
	input.context.complexity_approved == true
}

suspicious_session {
	# Example: too many requests in short time
	input.context.request_count > 100
	input.context.time_window_minutes < 5
}

suspicious_session {
	# Example: requests from blocked IP ranges
	startswith(input.ip_address, "192.168.100.") # blocked internal range
}

# Token budget restrictions
deny[{"reason": "token budget exceeded"}] {
	input.token_budget > 10000
	input.environment == "prod"
	not budget_approved
}

budget_approved {
	input.context.budget_approved == true
}