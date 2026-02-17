Run full CI checks before pushing.

Steps:
1. Run `make ci` to execute all CI checks
2. This includes:
   - Linting (Go, Rust, Python)
   - Unit tests
   - Proto generation verification
   - Replay determinism tests
3. Fix any failures before pushing
