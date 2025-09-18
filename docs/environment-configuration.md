# Environment Configuration Guide

This guide explains how to properly configure environment variables for Shannon's Docker Compose deployment.

## Table of Contents
- [Overview](#overview)
- [Environment Variable Loading](#environment-variable-loading)
- [Required Configuration](#required-configuration)
- [Common Issues and Solutions](#common-issues-and-solutions)
- [Best Practices](#best-practices)

## Overview

Shannon uses environment variables for sensitive configuration like API keys, service endpoints, and feature flags. Proper configuration is essential for the platform to function correctly.

## Environment Variable Loading

### How Docker Compose Loads Environment Variables

Docker Compose loads environment variables in this order of precedence:

1. **Shell environment variables** (highest priority)
2. **`.env` file in the docker-compose directory**
3. **`env_file` directive in docker-compose.yml**
4. **`environment` section in docker-compose.yml** (lowest priority)

### Critical Setup Steps

#### 1. Create the Root `.env` File

Create `.env` in the project root by copying the example:
```bash
cp .env.example .env
# Edit .env with your actual API keys
```

#### 2. Create Symlink for Docker Compose

Docker Compose looks for `.env` in the same directory as the `docker-compose.yml` file:
```bash
cd deploy/compose
ln -sf ../../.env .env
```

This symlink ensures Docker Compose can find your environment variables.

#### 3. Verify Configuration

Test that your environment variables are loaded correctly:
```bash
# From project root
docker compose -f deploy/compose/docker-compose.yml config | grep EXA_API_KEY

# Check inside running container
docker compose -f deploy/compose/docker-compose.yml exec llm-service env | grep EXA
```

## Required Configuration

### Essential API Keys

```bash
# LLM Provider Keys (at least one required)
OPENAI_API_KEY=sk-...
ANTHROPIC_API_KEY=sk-ant-...

# Web Search (optional but recommended)
EXA_API_KEY=your-exa-key-here
FIRECRAWL_API_KEY=your-firecrawl-key-here

# Model Selection (optional)
COMPLEXITY_MODEL_ID=gpt-4o-mini
DECOMPOSITION_MODEL_ID=gpt-4o
```

### Service Configuration

```bash
# Token Budget (optional)
WORKFLOW_SYNTH_BYPASS_SINGLE=true

# Rate Limiting (optional)
RATE_LIMIT_REQUESTS=100
RATE_LIMIT_INTERVAL_MS=60000
```

## Common Issues and Solutions

### Issue: "No web search provider configured"

**Symptom**: Error message in agent execution:
```
Error: No web search provider configured. Please set one of:
- EXA_API_KEY environment variable for Exa search
- FIRECRAWL_API_KEY environment variable for Firecrawl search
```

**Solution**:
1. Ensure `.env` file contains the API keys
2. Create the symlink: `cd deploy/compose && ln -sf ../../.env .env`
3. Restart services: `docker compose -f deploy/compose/docker-compose.yml up -d`

### Issue: Docker Compose warnings about missing variables

**Symptom**: Warnings like:
```
level=warning msg="The \"EXA_API_KEY\" variable is not set. Defaulting to a blank string."
```

**Solution**:
These warnings appear during the build phase when Docker Compose evaluates the docker-compose.yml file. They can be safely ignored if:
1. The symlink exists (`deploy/compose/.env -> ../../.env`)
2. The variables are correctly set inside the container (verify with `docker compose exec llm-service env`)

### Issue: Rate limit errors in multi-agent execution

**Symptom**: "Rate limit exceeded" errors when multiple agents run in parallel

**Solution**: This is now fixed in the codebase (rate limiting is per-session, not global). If you still see issues:
1. Check the rate limit configuration in your tools
2. Ensure you're using the latest code version

## Best Practices

### 1. Never Commit Secrets
- Keep `.env` in `.gitignore`
- Use `.env.example` as a template with placeholder values
- Store production secrets in a secure vault

### 2. Use Consistent Naming
- Use UPPER_CASE_WITH_UNDERSCORES for environment variables
- Prefix service-specific variables (e.g., `LLM_SERVICE_URL`)

### 3. Document Required Variables
- Maintain `.env.example` with all required variables
- Include descriptions as comments
- Specify which variables are optional vs required

### 4. Validate on Startup
Services should validate required environment variables on startup:
```python
# Example in Python service
import os
import sys

required_vars = ["OPENAI_API_KEY", "REDIS_HOST"]
missing = [var for var in required_vars if not os.getenv(var)]
if missing:
    print(f"Missing required environment variables: {missing}")
    sys.exit(1)
```

### 5. Use Environment-Specific Files
For different environments:
```bash
.env.development
.env.staging  
.env.production
```

Load the appropriate file:
```bash
# Development
ln -sf ../../.env.development deploy/compose/.env

# Production
ln -sf ../../.env.production deploy/compose/.env
```

## Makefile Integration

Add these helpful commands to your Makefile:

```makefile
# Setup environment
.PHONY: setup-env
setup-env:
	@if [ ! -f .env ]; then \
		cp .env.example .env; \
		echo "Created .env file - please edit with your API keys"; \
	fi
	@cd deploy/compose && ln -sf ../../.env .env
	@echo "Environment setup complete"

# Validate environment
.PHONY: check-env
check-env:
	@echo "Checking environment configuration..."
	@docker compose -f deploy/compose/docker-compose.yml config > /dev/null 2>&1
	@docker compose -f deploy/compose/docker-compose.yml exec llm-service env | grep -q EXA_API_KEY || \
		echo "Warning: EXA_API_KEY not set in container"
	@echo "Environment check complete"
```

## Troubleshooting Commands

```bash
# Check if .env file exists
ls -la .env deploy/compose/.env

# View current environment in container
docker compose -f deploy/compose/docker-compose.yml exec llm-service env | sort

# Test with explicit env file
docker compose --env-file .env -f deploy/compose/docker-compose.yml up -d

# Export shell variables (temporary, for testing)
export $(grep -v '^#' .env | xargs)
docker compose -f deploy/compose/docker-compose.yml up -d
```

## Summary

1. **Always create the symlink**: `cd deploy/compose && ln -sf ../../.env .env`
2. **Verify variables are loaded**: Check inside the container, not just docker-compose output
3. **Keep secrets secure**: Never commit actual API keys to the repository
4. **Document requirements**: Maintain clear documentation of required variables

Following these guidelines will prevent environment configuration issues and ensure smooth deployment of the Shannon platform.