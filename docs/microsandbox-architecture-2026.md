# Shannon AI: MicroSandbox Architecture (2026)

## Overview
The **MicroSandbox** is the secure, high-performance embedded runtime for Shannon AI's agentic workflows. It enables the execution of untrusted or semi-trusted agent logic (WASM) directly within the `shannon-api` process, enforcing strict capability-based security policies (Network, Filesystem, Environment).

This document outlines the finalized 2026 architecture, leveraging **Wasmtime v40** and **Embedded SurrealDB**.

## Core Components

### 1. Runtime: Wasmtime v40
We utilize [Wasmtime](https://wasmtime.dev/) as the WebAssembly engine.
-   **Version**: v40.0.0 (January 2026).
-   **Execution Model**: Core WebAssembly Modules (`wasm32-wasip1`).
    -   *Rationale*: Provides maximum compatibility with existing toolchains (Rust, C++, TinyGo) and Python-to-WASM compilers while maintaining a minimal footprint.
    -   *Future Proofing*: The architecture isolates the runtime implementation, allowing a seamless migration to the Component Model (WASIp2) when the ecosystem toolchain (wit-bindgen, jco) further matures for Python agents.

### 2. System Interface: WASI Preview 1 (Optimized)
We use the stabilized WASI Preview 1 support in Wasmtime v40.
-   **Context Management**: `WasiCtxBuilder::build_p1()` provides a robust, ergonomic way to construct secure contexts.
-   **Linking**: `wasmtime_wasi::p1::add_to_linker_async` ensures ensuring non-blocking execution for I/O bound agents.

### 3. Security: Capability-Based Policies
The sandbox enforces a "Deny by Default" policy, controlled by the `SandboxCapabilities` struct.
-   **Filesystem**: Virtualized root or strictly mapped directories (`DirPerms::READ`, `DirPerms::WRITE`).
-   **Network**: Allow-list based outgoing HTTP/Socket connections (via host function interception or WASI socket restrictions).
-   **Environment**: Sanitized environment variables.
-   **Compute**: strict `fuel` (CPU budget) limiting to prevent infinite loops.

### 4. Persistence: Embedded SurrealDB
The `durable-shannon` worker maintains state using an embedded SurrealDB instance.
-   **Engine**: RocksDB (`kv-rocksdb`) running in-process.
-   **Benefits**:
    -   Zero-latency IPC (direct function calls).
    -   Single binary deployment (no sidecar processes to manage).
    -   ACID compliance for complex multi-step agent workflows.

## Architectural Data Flow

```mermaid
graph TD
    A[Shannon API (Tauri Core)] -->|Submit Task| B(Durable Engine)
    B -->|Check Policy| C{Policy Check}
    C -->|Allowed| D[MicroSandbox]
    C -->|Denied| E[Reject]
    
    subgraph MicroSandbox [Wasmtime v40 Runtime]
        D -->|Instantiate| F[Wasm Agent]
        F -->|WASI P1| G[VFS / Host Resources]
        F -->|Host Func| H[Shannon Tools (LLM, Search)]
    end
    
    B -->|Persist Event| I[(SurrealDB Embedded)]
```

## Implementation Strategy
-   **Crate**: `durable-shannon`
-   **Module**: `microsandbox`
-   **Integration point**: `shannon-api/src/workflow/engine.rs`

This architecture ensures Shannon AI remains a "Localhost-First" sovereign AI platform, capable of running complex agents safely on user hardware.
