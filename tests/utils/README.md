# Utility Tests

This directory contains utility tests for specific features and development tools that don't fit into the full e2e or integration test categories.

## Test Scripts

### Feature Tests

- **test_budget_controls.sh** - Tests budget management and token tracking
  - User budget limits
  - Session budget tracking
  - Backpressure mechanisms

- **test_token_aggregation.sh** - Tests token usage aggregation
  - Token counting
  - Cross-service token tracking

### Development Utilities

- **test_ci_local.sh** - Simulate CI environment locally
  - Tests in Ubuntu Docker container
  - Validates CI build steps
  - Useful for debugging CI failures

- **test_grpc_reflection.sh** - Tests gRPC service reflection
  - Service discovery
  - Method introspection
  - API validation

## Running Tests

```bash
# Run individual tests
./tests/utils/test_budget_controls.sh
./tests/utils/test_token_aggregation.sh
./tests/utils/test_grpc_reflection.sh

# Test CI locally
./tests/utils/test_ci_local.sh
```

## Prerequisites

- Services running (`make dev`)
- `grpcurl` installed for gRPC tests
- Docker for CI simulation

## When to Use These Tests

- **During development**: Quick validation of specific features
- **Debugging**: Isolate and test individual components
- **CI troubleshooting**: Reproduce CI environment locally
- **Feature validation**: Test specific functionality without full e2e overhead

These tests complement the comprehensive e2e and integration suites by providing targeted, quick validation of specific system aspects.