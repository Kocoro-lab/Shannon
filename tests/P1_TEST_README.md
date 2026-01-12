# P1 Task Management Integration Tests

Integration test suite for P1.6-P1.8 (List Tasks, Pause, Resume) from the Embedded Feature Parity specification.

## Test File

**Location**: [`tests/p1_task_management.rs`](../tests/p1_task_management.rs)

## Prerequisites

1. **Start the embedded Shannon API**:
   ```bash
   cd desktop
   npm run dev
   ```

2. **Verify API is running**:
   ```bash
   curl http://localhost:8765/health
   ```
   Should return: `{"status":"healthy"}`

## Running Tests

### Run All P1 Tests
```bash
cargo test --test p1_task_management --features embedded -- --ignored --nocapture
```

### Run Specific Test
```bash
# Test API availability
cargo test test_api_is_running --features embedded -- --ignored --nocapture

# Test pagination
cargo test test_list_tasks_pagination --features embedded -- --ignored --nocapture

# Test pause/resume
cargo test test_pause_resume_task --features embedded -- --ignored --nocapture

# Test filtering
cargo test test_list_tasks_filter_by_status --features embedded -- --ignored --nocapture

# Test performance
cargo test test_list_tasks_performance --features embedded -- --ignored --nocapture

# Full end-to-end
cargo test test_end_to_end_task_lifecycle --features embedded -- --ignored --nocapture
```

## Test Coverage

### P1.6: List Tasks (6 tests)
- ✅ `test_list_tasks_empty` - Empty state handling
- ✅ `test_list_tasks_pagination` - Offset/limit pagination with 25 tasks
- ✅ `test_list_tasks_filter_by_status` - Filter by task status
- ✅ `test_list_tasks_filter_by_session` - Filter by session ID
- ✅ `test_list_tasks_combined_filters` - Multiple filters together
- ✅ `test_list_tasks_persistence` - Database persistence verification

### P1.7 & P1.8: Pause/Resume (5 tests)
- ✅ `test_pause_resume_task` - Basic pause and resume flow
- ✅ `test_pause_nonexistent_task` - Error handling for invalid task
- ✅ `test_resume_not_paused_task` - Resume task that isn't paused
- ✅ `test_multiple_pause_resume_cycles` - Repeated pause/resume
- ✅ `test_end_to_end_task_lifecycle` - Full lifecycle with all operations

### Additional Tests (3 tests)
- ✅ `test_pagination_with_active_tasks` - In-memory + database merge
- ✅ `test_invalid_pagination_params` - Error handling
- ✅ `test_list_tasks_performance` - Performance benchmarking

**Total**: 14 comprehensive integration tests

## What Each Test Verifies

### List Tasks Tests

**Empty State** (`test_list_tasks_empty`):
- Returns empty array when no tasks exist
- total_count is 0
- Default limit is 20

**Pagination** (`test_list_tasks_pagination`):
- Submits 25 tasks
- Fetches 3 pages (10, 10, 5)
- Verifies no duplicate IDs across pages
- Confirms total_count ≥ 25

**Status Filtering** (`test_list_tasks_filter_by_status`):
- Submits 5 tasks
- Filters by `status=running`
- Filters by `status=completed`
- Verifies only matching statuses returned

**Session Filtering** (`test_list_tasks_filter_by_session`):
- Creates 3 tasks in session A
- Creates 2 tasks in session B
- Filters by each session
- Verifies correct counts and session IDs

**Combined Filters** (`test_list_tasks_combined_filters`):
- Submits 5 tasks to same session
- Filters by both status AND session
- Verifies both filters applied correctly

**Persistence** (`test_list_tasks_persistence`):
- Submits 3 tasks
- Verifies they appear in database
- Notes: Full restart test requires manual verification

### Pause/Resume Tests

**Basic Flow** (`test_pause_resume_task`):
- Submits task
- Pauses it → verifies `is_paused=true` in control state
- Resumes it → verifies `is_paused=false`
- Checks pause_reason, paused_at timestamps

**Error Handling** (`test_pause_nonexistent_task`):
- Attempts to pause non-existent task
- Verifies error response (404 or 500)

**Resume Unpaused** (`test_resume_not_paused_task`):
- Resumes task that was never paused
- Should succeed (no-op)
- Control state shows not paused

**Multiple Cycles** (`test_multiple_pause_resume_cycles`):
- Pause → Resume → Pause → Resume
- Verifies state changes correctly each time

**End-to-End** (`test_end_to_end_task_lifecycle`):
1. Submit task
2. Verify appears in list
3. Check status
4. Pause task
5. Resume task
6. Verify still in list
7. All steps logged

### Additional Tests

**Active Tasks** (`test_pagination_with_active_tasks`):
- Submits 10 tasks (not yet persisted to DB)
- Lists with pagination
- Verifies in-memory tasks included in results

**Performance** (`test_list_tasks_performance`):
- Submits 100 tasks
- Measures list query time
- Asserts < 100ms (should be ~10-20ms with indexes)
- Logs timing for analysis

## Performance Targets

| Operation | Target | Measurement |
|-----------|--------|-------------|
| List 20 tasks | < 50ms | Measured in test |
| List 100 tasks | < 200ms | Stress test |
| Count query | < 20ms | Included in list |
| Pause task | < 20ms | Control state update |
| Resume task | < 20ms | Control state update |

## Expected Test Results

All tests should **PASS** with output similar to:

```
running 14 tests
test test_api_is_running ... ok
test test_list_tasks_empty ... ok
test test_list_tasks_pagination ... ok
test test_list_tasks_filter_by_status ... ok
test test_list_tasks_filter_by_session ... ok
test test_list_tasks_combined_filters ... ok
test test_list_tasks_persistence ... ok
test test_pause_resume_task ... ok
test test_pause_nonexistent_task ... ok
test test_resume_not_paused_task ... ok
test test_multiple_pause_resume_cycles ... ok
test test_end_to_end_task_lifecycle ... ok
test test_pagination_with_active_tasks ... ok
test test_list_tasks_performance ... ok

test result: ok. 14 passed; 0 failed; 0 ignored
```

## Troubleshooting

### API Not Running
```
Error: Connection refused (os error 61)
```
**Solution**: Start the desktop app first
```bash
cd desktop && npm run dev
```

### Database Errors
```
Error: SQLite not initialized
```
**Solution**: Ensure embedded feature enabled and database initialized

### Timeout Errors
```
Timeout waiting for task status: completed
```
**Solution**: Increase timeout or check if workflow engine is processing tasks

## Manual Testing Checklist

After automated tests pass, manually verify:

- [ ] Restart desktop app
- [ ] Check tasks still appear in list (persistence)
- [ ] Submit task, pause mid-execution, verify it stops progressing
- [ ] Resume paused task, verify it continues and completes
- [ ] Check database file directly: `sqlite3 ~/.config/shannon/shannon.sqlite "SELECT * FROM runs LIMIT 5;"`

## Notes

- Tests use `#[ignore]` to prevent running in CI without API
- Run with `-- --ignored` to execute
- Use `--nocapture` to see println! output
- Tests are idempotent (can run multiple times)
- Each test cleans up after itself (best effort)

## Future Enhancements

- [ ] Add unit tests for database methods
- [ ] Add tests for cursor-based pagination
- [ ] Add concurrent access tests
- [ ] Add stress tests (1000+ tasks)
- [ ] Add workflow integration tests (verify pause actually stops execution)
