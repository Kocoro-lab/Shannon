# WASI Code Execution Example

Use this example to run a small "Hello, WASI" module inside the Shannon WASI sandbox and verify isolation, resource limits, and output capture.

## Prerequisites

- Rust toolchain + cargo
- Wasm text tools to compile `.wat` → `.wasm` (pick one):
  - `wabt` (provides `wat2wasm`), or
  - `wasm-tools` (provides `wasm-tools parse`)

## 1) Build the example wasm

- Source lives at `docs/assets/hello-wasi.wat`.
- Compile to a file under `/tmp` (preopened read-only by default):

```bash
# Option A: wabt
wat2wasm docs/assets/hello-wasi.wat -o /tmp/hello-wasi.wasm

# Option B: wasm-tools
wasm-tools parse -o /tmp/hello-wasi.wasm docs/assets/hello-wasi.wat

# Quick check: the module is valid
file /tmp/hello-wasi.wasm
```

## 2) Run via the example CLI

The repository includes a small example CLI to exercise the sandbox end-to-end.

```bash
cd rust/agent-core
cargo run --example wasi_hello -- /tmp/hello-wasi.wasm
```

You should see:

```
Hello from WASI!
```

Notes:
- The sandbox enforces:
  - Read-only FS preopens (defaults include `/tmp`).
  - Fuel metering and a 30s timeout via epoch interruption.
  - Runtime memory limits via `StoreLimits`.
  - Captured stdout/stderr via in-memory pipes.

## 3) Alternative: call via ToolExecutor (base64)

If you prefer not to write to the filesystem, you can pass the wasm bytes via base64. The ToolExecutor routes the `code_executor` tool to the WASI sandbox.

```bash
# Create base64 payload
BASE64=$(base64 -b 0 /tmp/hello-wasi.wasm)

# Run unit test that exercises the ToolExecutor + WASI path
cd rust/agent-core
RUST_LOG=info cargo test -q tools::tests::test_code_executor_with_base64_payload
```

## 4) Tuning resource limits

The WASI sandbox uses conservative defaults suitable for single-module execution:
- `memory_limit`: 256 MiB
- `table_elements_limit`: 1,024
- `instances_limit`: 4
- `tables_limit`: 4
- `memories_limit`: 2

You can tune them using the builder API:

```rust
use shannon_agent_core::wasi_sandbox::WasiSandbox;

let sandbox = WasiSandbox::new()
    .unwrap()
    .set_memory_limit(128 * 1024 * 1024)   // 128 MiB
    .set_table_elements_limit(512)         // per-table elements
    .set_instances_limit(2)                // max instances in the store
    .set_tables_limit(2)                   // max tables in the store
    .set_memories_limit(1);                // max memories in the store
```

These caps are enforced at runtime via `StoreLimits` and complement the early module pre-validation.

## 5) Troubleshooting

- Invalid module header:
  - Ensure the file is compiled to wasm (`.wasm`), not `.wat`.
- No output:
  - Verify the module uses WASI fd_write to stdout.
- Permission errors:
  - The sandbox only preopens read-only directories (defaults to `/tmp`). Place your wasm there or call `allow_path()` on the sandbox to preopen other directories.

## 6) Security considerations

- Keep preopened directories minimal (prefer read-only).
- Only opt-in to environment variable access when necessary.
- Adjust `memory_limit` and timeouts per workload; prefer smallest values that work.

