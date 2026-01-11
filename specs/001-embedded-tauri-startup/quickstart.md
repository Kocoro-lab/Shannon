# Quickstart: Embedded App Startup Readiness

## Prerequisites

- Clone repo: `/Users/gqadonis/Projects/prometheus/Shannon`
- Desktop dependencies installed (Node.js + Rust toolchain)

## Validate embedded startup flow

1. Run desktop app in embedded mode:

   ```bash
   cd /Users/gqadonis/Projects/prometheus/Shannon/desktop
   npm run tauri:dev
   ```

2. Verify readiness gating:

   - Confirm chat UI is disabled until the embedded service reports ready.
   - If port 1906 is occupied, confirm the app binds to an available port in 1907-1915.

3. Check debug console logs:

   - Open the debug console UI and confirm startup logs appear in chronological order.

## Run automated tests

1. Rust tests (embedded service and Tauri integration tests):

   ```bash
   cd /Users/gqadonis/Projects/prometheus/Shannon/desktop/src-tauri
   cargo test
   ```

2. Desktop UI tests:

   ```bash
   cd /Users/gqadonis/Projects/prometheus/Shannon/desktop
   npm run test
   ```
