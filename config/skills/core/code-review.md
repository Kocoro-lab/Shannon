---
name: code-review
version: 1.0.0
author: Shannon
category: development
description: Systematic code review workflow with security and quality checks
requires_tools:
  - file_read
  - file_list
  - bash
requires_role: critic
budget_max: 5000
dangerous: false
enabled: true
metadata:
  complexity: medium
  estimated_duration: 5min
---

# Code Review Skill

You are performing a systematic code review. Follow this workflow:

## 1. Context Gathering
- Use `file_list` to discover files in the target directory
- Read relevant files with `file_read`
- Use `bash` to run `git diff` for recent changes if in a git repository

## 2. Analysis Checklist

### Security Review
- SQL injection vulnerabilities (parameterized queries?)
- XSS vulnerabilities (output encoding?)
- Command injection (shell escaping?)
- Path traversal (canonicalization?)
- Authentication/authorization gaps
- Secrets in code (API keys, passwords)

### Code Quality
- Function/method length (prefer <50 lines)
- Cyclomatic complexity
- Code duplication
- Error handling completeness
- Naming conventions consistency

### Performance
- N+1 query patterns
- Inefficient algorithms (O(n²) where O(n) possible)
- Missing indexes hints
- Unnecessary memory allocations

### Testing
- Test coverage gaps
- Missing edge case tests
- Test isolation issues

## 3. Output Format

Provide findings in this structure:

### Critical Issues
- [Issue description with file:line reference]

### Recommendations
- [Improvement suggestion with rationale]

### Positive Observations
- [Good practices to acknowledge]

Be specific with line numbers and code snippets. Prioritize actionable feedback.
