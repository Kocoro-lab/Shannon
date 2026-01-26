# Shannon Skills System

## Overview

Skills are markdown-based workflow definitions that provide structured prompts, tool configurations, and execution constraints for common tasks. They're compatible with Anthropic's Agent Skills specification.

When a task uses a skill, the skill's markdown content becomes the system prompt, guiding the AI agent through a structured workflow. This enables consistent, repeatable task execution patterns.

## Directory Structure

```
config/skills/
├── core/           # Built-in skills (committed to repo)
│   ├── code-review.md
│   ├── debugging.md
│   └── test-driven-dev.md
├── user/           # User custom skills (gitignored)
└── vendor/         # Vendor-specific skills (gitignored)
```

| Directory | Purpose | Git Status |
|-----------|---------|------------|
| `core/` | Built-in skills shipped with Shannon | Committed |
| `user/` | Personal/team custom skills | Gitignored |
| `vendor/` | Third-party or vendor-specific skills | Gitignored |

## Skill File Format

Skills are markdown files with YAML frontmatter followed by markdown content:

```markdown
---
name: my-skill
version: 1.0.0
author: Your Name
category: development
description: Brief description of what this skill does
requires_tools:
  - file_read
  - file_write
  - bash
requires_role: generalist
budget_max: 5000
dangerous: false
enabled: true
metadata:
  complexity: medium
  estimated_duration: 10min
---

# Skill Title

Your skill instructions in markdown format...

## Step 1: Gather Information
- Use `file_list` to discover files
- Read relevant files with `file_read`

## Step 2: Perform Analysis
...

## Output Format
Provide findings in this structure:
- Summary
- Details
- Recommendations
```

### Frontmatter Fields

| Field | Required | Type | Default | Description |
|-------|----------|------|---------|-------------|
| `name` | Yes | string | - | Unique identifier (lowercase, hyphens, underscores only) |
| `version` | No | string | `1.0.0` | Semantic version |
| `author` | No | string | - | Skill author |
| `category` | No | string | - | Category for grouping (e.g., development, research) |
| `description` | No | string | - | Brief description (required if `dangerous: true`) |
| `requires_tools` | No | list | `[]` | List of tools this skill needs |
| `requires_role` | No | string | - | Role preset to use (bypasses task decomposition) |
| `budget_max` | No | int | - | Maximum token budget for execution |
| `dangerous` | No | bool | `false` | Whether skill performs dangerous operations |
| `enabled` | No | bool | `true` | Whether skill is active |
| `metadata` | No | object | `{}` | Additional key-value metadata |

### Name Validation

Skill names must contain only:
- Lowercase letters (a-z)
- Uppercase letters (A-Z)
- Numbers (0-9)
- Hyphens (-)
- Underscores (_)

## Using Skills via API

### Execute a Task with a Skill

```bash
curl -X POST http://localhost:8080/api/v1/tasks \
  -H "Content-Type: application/json" \
  -d '{
    "query": "Review the authentication module for security issues",
    "skill": "code-review",
    "session_id": "my-session-123"
  }'
```

When a `skill` is specified:
1. The skill's markdown content becomes the system prompt
2. If `requires_role` is set, it's applied (bypasses decomposition)
3. The task runs as single-agent execution with the skill's guidance

### Using Versioned Skills

Request a specific version with `name@version`:

```bash
curl -X POST http://localhost:8080/api/v1/tasks \
  -H "Content-Type: application/json" \
  -d '{
    "query": "Debug the login failure",
    "skill": "debugging@1.0.0",
    "session_id": "my-session-123"
  }'
```

## API Endpoints

### List All Skills

```bash
GET /api/v1/skills
```

Response:
```json
{
  "skills": [
    {
      "name": "code-review",
      "version": "1.0.0",
      "category": "development",
      "description": "Systematic code review workflow",
      "requires_tools": ["file_read", "file_list", "bash"],
      "dangerous": false,
      "enabled": true
    }
  ],
  "count": 3,
  "categories": ["development"]
}
```

### Filter by Category

```bash
GET /api/v1/skills?category=development
```

### Get Skill Details

```bash
GET /api/v1/skills/{name}
```

Response:
```json
{
  "skill": {
    "name": "code-review",
    "version": "1.0.0",
    "author": "Shannon",
    "category": "development",
    "description": "Systematic code review workflow",
    "requires_tools": ["file_read", "file_list", "bash"],
    "requires_role": "critic",
    "budget_max": 5000,
    "dangerous": false,
    "enabled": true,
    "content": "# Code Review Skill\n\nYou are performing..."
  },
  "metadata": {
    "source_path": "/app/config/skills/core/code-review.md",
    "content_hash": "abc123...",
    "loaded_at": "2026-01-26T10:00:00Z"
  }
}
```

### List Skill Versions

```bash
GET /api/v1/skills/{name}/versions
```

Response:
```json
{
  "name": "code-review",
  "versions": [
    {"name": "code-review", "version": "2.0.0", ...},
    {"name": "code-review", "version": "1.0.0", ...}
  ],
  "count": 2
}
```

## Creating Custom Skills

### Step 1: Create the Skill File

Create a `.md` file in `config/skills/user/`:

```bash
mkdir -p config/skills/user
cat > config/skills/user/my-analysis.md << 'EOF'
---
name: my-analysis
version: 1.0.0
author: Your Name
category: analysis
description: Custom analysis workflow
requires_tools:
  - file_read
  - file_list
requires_role: generalist
budget_max: 3000
---

# My Analysis Skill

Instructions for the AI agent...

## Step 1: Gather Data
...

## Step 2: Analyze
...

## Output Format
...
EOF
```

### Step 2: Add Directory to SKILLS_PATH (Optional)

If using a custom directory:

```bash
export SKILLS_PATH="config/skills/core:config/skills/user:/custom/skills"
```

### Step 3: Restart Gateway

Skills are loaded at gateway startup:

```bash
docker compose -f deploy/compose/docker-compose.yml restart gateway
```

### Step 4: Verify Loading

```bash
curl -sS http://localhost:8080/api/v1/skills | jq '.skills[].name'
```

## Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `SKILLS_PATH` | `config/skills/core` (dev) or `/app/config/skills/core` (container) | Colon-separated list of directories to scan for skills |

Example:
```bash
# Multiple directories
export SKILLS_PATH="/app/config/skills/core:/app/config/skills/user:/custom/vendor-skills"
```

## Security Considerations

### Dangerous Skills

Skills with `dangerous: true` indicate they perform potentially destructive operations:

```yaml
---
name: cleanup-files
dangerous: true
description: Removes temporary files (REQUIRED when dangerous=true)
---
```

Requirements for dangerous skills:
- Must have a non-empty `description` field
- Should be used with explicit user confirmation
- Consider implementing approval workflows in production

### Role-Based Access Control

The `requires_role` field maps to Shannon's role presets:

| Role | Description | Typical Tools |
|------|-------------|---------------|
| `generalist` | General-purpose agent | All standard tools |
| `critic` | Code review and analysis | file_read, file_list |
| `developer` | Development tasks | file_read, file_write, bash |

When `requires_role` is set, the orchestrator bypasses task decomposition and runs the task as a single-agent execution with the specified role.

### Tool Restrictions

The `requires_tools` field declares which tools the skill expects:

```yaml
requires_tools:
  - file_read
  - file_list
  - bash
```

This serves as documentation and can be used for:
- Pre-validation that required tools are available
- Access control policies
- Audit logging

## Best Practices

1. **Be Specific**: Skills should provide clear, step-by-step guidance
2. **Structure Output**: Define expected output format in the skill
3. **Tool Requirements**: Only list tools the skill actually uses
4. **Version Control**: Use semantic versioning for skill updates
5. **Test Skills**: Verify skills produce expected results before deployment
6. **Document Context**: Include what the skill expects as input
7. **Error Handling**: Guide the agent on handling edge cases

## Example: Built-in Skills

### code-review

A systematic code review workflow:
- Security analysis (injection, XSS, auth gaps)
- Code quality metrics
- Performance review
- Testing coverage analysis

### debugging

Structured debugging methodology:
- Problem understanding
- Information gathering
- Hypothesis formation and testing
- Root cause analysis
- Solution implementation

### test-driven-dev

Test-driven development workflow:
- Write failing tests first
- Implement minimal code to pass
- Refactor with confidence

## Troubleshooting

### Skills Not Loading

1. Check directory exists:
   ```bash
   ls -la config/skills/core/
   ```

2. Verify YAML frontmatter syntax:
   ```bash
   head -20 config/skills/core/my-skill.md
   ```

3. Check gateway logs for loading errors:
   ```bash
   docker compose -f deploy/compose/docker-compose.yml logs gateway | grep -i skill
   ```

### Skill Not Found (404)

1. Verify skill is enabled (`enabled: true` or omitted)
2. Check exact name spelling
3. Ensure file extension is `.md`
4. Confirm directory is in `SKILLS_PATH`

### Version Conflicts

If two skills have the same `name@version`, loading fails. Use unique version numbers or place skills in separate directories.
