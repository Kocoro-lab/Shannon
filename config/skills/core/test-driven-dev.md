---
name: test-driven-dev
version: 1.0.0
author: Shannon
category: development
description: Test-driven development workflow for implementing features
requires_tools:
  - file_read
  - file_write
  - file_list
  - bash
requires_role: generalist
budget_max: 10000
dangerous: false
enabled: true
metadata:
  complexity: medium
  estimated_duration: 20min
---

# Test-Driven Development Skill

You are implementing a feature using test-driven development (TDD). Follow the Red-Green-Refactor cycle.

## TDD Cycle

### 1. Red Phase - Write Failing Test

Before writing any implementation code:
- Understand the requirement clearly
- Write a test that describes the expected behavior
- Run the test to confirm it fails (for the right reason)
- The test should be:
  - Specific (tests one thing)
  - Independent (doesn't depend on other tests)
  - Descriptive (test name explains what it tests)

### 2. Green Phase - Make Test Pass

Write the minimum code needed to make the test pass:
- Focus only on making the current test pass
- Don't add extra functionality
- Don't worry about code elegance yet
- Run the test to confirm it passes

### 3. Refactor Phase - Improve Code

With passing tests as a safety net:
- Remove duplication
- Improve naming
- Extract methods/functions if needed
- Ensure code is clean and readable
- Run tests after each change to ensure they still pass

## Workflow

1. **Understand Requirements**
   - Read existing code with `file_list` and `file_read`
   - Identify the testing framework in use
   - Understand existing test patterns

2. **Write Test First**
   - Create or update test file
   - Write test for the smallest unit of functionality
   - Run test to see it fail

3. **Implement Minimum Code**
   - Write just enough code to pass the test
   - Avoid over-engineering
   - Keep it simple

4. **Refactor**
   - Clean up the implementation
   - Ensure tests still pass
   - Document if needed

5. **Repeat**
   - Move to the next requirement
   - Build up functionality incrementally

## Output Format

For each TDD cycle, provide:

### Test Written
```
[Test code]
```

### Implementation
```
[Production code]
```

### Refactoring Notes
[What was improved and why]

### Next Steps
[What to test next]

Remember: The tests are documentation. They should clearly express the intended behavior.
