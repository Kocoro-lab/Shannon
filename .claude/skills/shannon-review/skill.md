# Shannon Code Review Checklist

## Workflow Changes

- [ ] Determinism maintained (no time.Sleep in activities)
- [ ] `aggregateAgentMetadata()` called to populate metadata
- [ ] Error handling with retries configured
- [ ] Replay test added/updated in ci-replay
- [ ] Workflow selection logic updated if needed
- [ ] Context propagation correct (workflow.Context vs context.Context)

## Database Changes

- [ ] Migration script created (numbered sequentially)
- [ ] Status values use UPPERCASE
- [ ] Session ID checks both UUID and external_id format
- [ ] Indexes added for new query patterns
- [ ] Foreign key constraints defined
- [ ] Soft delete pattern used (deleted_at)

## API Changes

- [ ] Request validation added
- [ ] Response includes usage metadata
- [ ] User ID authorization enforced
- [ ] Integration test added
- [ ] Error responses follow standard format
- [ ] OpenAPI spec updated

## Provider/Model Changes

- [ ] `config/models.yaml` updated (tier, pricing, catalog)
- [ ] Provider registered in Python (`__init__.py`)
- [ ] Provider detection in Go (`models/provider.go`)
- [ ] Token counting implemented
- [ ] Cost calculation verified
- [ ] Streaming support tested

## Vendor Adapter Pattern

- [ ] No vendor-specific code in core files
- [ ] Conditional imports with fallback
- [ ] Config overlay used (not committed)
- [ ] OpenAPI spec in separate file
- [ ] Generic field mirroring maintained
- [ ] Documentation updated

## Testing

- [ ] Unit tests added with >80% coverage
- [ ] Response body verified (not just status code)
- [ ] Database persistence checked
- [ ] Orchestrator logs reviewed
- [ ] Temporal replay determinism tested
- [ ] Race detection passed (`go test -race`)

## Security

- [ ] SQL injection prevented (parameterized queries)
- [ ] User input sanitized
- [ ] Authorization checks present
- [ ] Secrets not committed
- [ ] WASI sandbox limits enforced

## Documentation

- [ ] CLAUDE.md updated if workflow changes
- [ ] API docs updated
- [ ] Configuration examples provided
- [ ] Migration guide written (if breaking)

## Performance

- [ ] Database queries optimized (indexes)
- [ ] N+1 query patterns avoided
- [ ] Connection pooling configured
- [ ] Caching strategy considered
- [ ] Memory leaks checked

## Code Quality

- [ ] No duplicate files with "_enhanced" suffix
- [ ] No unnecessary complexity
- [ ] Functions under 50 lines
- [ ] Clear variable names
- [ ] Error messages helpful
