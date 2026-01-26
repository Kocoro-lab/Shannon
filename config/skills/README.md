# Shannon Skills System

Skills are markdown-based task definitions that provide structured guidance for specific workflows. They're compatible with Anthropic's Agent Skills specification.

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

## Skill File Format

Skills are markdown files with YAML frontmatter:

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

Your skill content in markdown format...
```

## Frontmatter Fields

| Field | Required | Description |
|-------|----------|-------------|
| `name` | Yes | Unique skill identifier (lowercase, hyphens, underscores) |
| `version` | No | Semantic version (default: 1.0.0) |
| `author` | No | Skill author |
| `category` | No | Category for grouping (e.g., development, research, analysis) |
| `description` | No | Brief description (required if dangerous=true) |
| `requires_tools` | No | List of tools this skill needs |
| `requires_role` | No | Role preset to use (bypasses decomposition) |
| `budget_max` | No | Maximum token budget |
| `dangerous` | No | Whether skill performs dangerous operations |
| `enabled` | No | Whether skill is active (default: true) |
| `metadata` | No | Additional key-value metadata |

## Using Skills

### Via API

```bash
# List all skills
curl http://localhost:8080/api/v1/skills

# Get a specific skill
curl http://localhost:8080/api/v1/skills/code-review

# Use a skill in a task
curl -X POST http://localhost:8080/api/v1/tasks \
  -H "Content-Type: application/json" \
  -d '{
    "query": "Review the authentication module",
    "skill": "code-review",
    "session_id": "my-session"
  }'
```

### Skill Execution

When a task specifies a `skill`:
1. The skill's content becomes the system prompt
2. If `requires_role` is set, it's applied to bypass decomposition
3. The task runs as a single-agent execution with the skill's guidance

## Custom Skills

Create custom skills by adding `.md` files to:
- `config/skills/user/` for personal skills
- `config/skills/vendor/{vendor}/` for vendor-specific skills

Set the `SKILLS_PATH` environment variable to add additional search paths:

```bash
export SKILLS_PATH="/app/config/skills/core:/custom/skills"
```

## Best Practices

1. **Be Specific**: Skills should provide clear, actionable guidance
2. **Structure Output**: Define expected output format
3. **Tool Requirements**: Only list tools the skill actually needs
4. **Version Control**: Use semantic versioning for skill updates
5. **Test Skills**: Verify skills work before deploying to production
