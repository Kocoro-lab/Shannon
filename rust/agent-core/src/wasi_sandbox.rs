use anyhow::Context;
use std::collections::HashMap;
use std::path::PathBuf;
use std::sync::Arc;
use std::time::Duration;
use std::time::Instant;
use tracing::{debug, info, warn};
use wasmtime::*;
use wasmtime_wasi::p2::pipe::{MemoryInputPipe, MemoryOutputPipe};
use wasmtime_wasi::{DirPerms, FilePerms, WasiCtxBuilder};

use crate::config::Config;
use crate::metrics::{TOOL_DURATION, TOOL_EXECUTIONS};

/// WASI-enabled sandbox with proper isolation
#[derive(Clone)]
pub struct WasiSandbox {
    engine: Arc<Engine>,
    allowed_paths: Vec<PathBuf>,
    allow_env_access: bool,
    env_vars: HashMap<String, String>,
    memory_limit: usize,
    fuel_limit: u64,
    execution_timeout: Duration,
    table_elements_limit: usize,
    instances_limit: usize,
    tables_limit: usize,
    memories_limit: usize,
}

impl std::fmt::Debug for WasiSandbox {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("WasiSandbox")
            .field("allowed_paths", &self.allowed_paths)
            .field("allow_env_access", &self.allow_env_access)
            .field("env_vars", &self.env_vars)
            .field("memory_limit", &self.memory_limit)
            .field("fuel_limit", &self.fuel_limit)
            .field("execution_timeout", &self.execution_timeout)
            .field("table_elements_limit", &self.table_elements_limit)
            .field("instances_limit", &self.instances_limit)
            .field("tables_limit", &self.tables_limit)
            .field("memories_limit", &self.memories_limit)
            .field("engine", &"<wasmtime::Engine>")
            .finish()
    }
}

impl WasiSandbox {
    pub fn new() -> Result<Self> {
        let app_config = Config::global().unwrap_or_default();
        Self::with_config(&app_config)
    }

    pub fn with_config(app_config: &Config) -> Result<Self> {
        // Create wasmtime engine with security-focused configuration
        let mut wasm_config = wasmtime::Config::new();

        // Enable necessary features for WASI
        wasm_config.wasm_reference_types(true);
        wasm_config.wasm_bulk_memory(true);
        wasm_config.consume_fuel(true);

        // Security settings - enable epoch interruption for timeouts
        wasm_config.epoch_interruption(true);
        wasm_config.memory_guard_size(64 * 1024 * 1024); // 64MB guard
        wasm_config.parallel_compilation(false); // Reduce resource usage

        let engine = Arc::new(Engine::new(&wasm_config)?);

        Ok(Self {
            engine,
            allowed_paths: app_config
                .wasi
                .allowed_paths
                .iter()
                .map(PathBuf::from)
                .collect(),
            allow_env_access: false,
            env_vars: HashMap::new(),
            memory_limit: app_config.wasi.memory_limit_bytes,
            fuel_limit: app_config.wasi.max_fuel,
            execution_timeout: app_config.wasi_timeout(),
            // Python WASM requires larger table limits (5413+ elements)
            table_elements_limit: 10000, // Increased for Python WASM
            instances_limit: 10,
            tables_limit: 10,
            memories_limit: 4,
        })
    }

    #[allow(dead_code)]
    pub fn allow_path(mut self, path: impl Into<PathBuf>) -> Self {
        self.allowed_paths.push(path.into());
        self
    }

    pub fn set_execution_timeout(mut self, timeout: Duration) -> Self {
        self.execution_timeout = timeout;
        self
    }

    pub fn allow_env_access(mut self, allow: bool) -> Self {
        self.allow_env_access = allow;
        self
    }

    pub fn set_env(mut self, key: String, value: String) -> Self {
        self.env_vars.insert(key, value);
        self
    }

    pub fn set_memory_limit(mut self, bytes: usize) -> Self {
        self.memory_limit = bytes;
        self
    }

    #[allow(dead_code)]
    pub fn set_table_elements_limit(mut self, elems: usize) -> Self {
        self.table_elements_limit = elems;
        self
    }

    #[allow(dead_code)]
    pub fn set_instances_limit(mut self, n: usize) -> Self {
        self.instances_limit = n;
        self
    }

    #[allow(dead_code)]
    pub fn set_tables_limit(mut self, n: usize) -> Self {
        self.tables_limit = n;
        self
    }

    #[allow(dead_code)]
    pub fn set_memories_limit(mut self, n: usize) -> Self {
        self.memories_limit = n;
        self
    }

    pub async fn execute_wasm(&self, wasm_bytes: &[u8], input: &str) -> Result<String> {
        self.execute_wasm_with_args(wasm_bytes, input, None).await
    }

    pub async fn execute_wasm_with_args(
        &self,
        wasm_bytes: &[u8],
        input: &str,
        argv: Option<Vec<String>>,
    ) -> Result<String> {
        info!("Executing WASM with WASI isolation (argv: {:?})", argv);
        let start = Instant::now();

        // Validate permissions first
        self.validate_permissions()
            .context("Permission validation failed")?;

        // Validate WASM module
        if wasm_bytes.len() > 50 * 1024 * 1024 {
            return Err(anyhow::anyhow!(
                "WASM module too large: {} bytes",
                wasm_bytes.len()
            ));
        }

        if wasm_bytes.len() < 4 || &wasm_bytes[0..4] != b"\0asm" {
            return Err(anyhow::anyhow!("Invalid WASM module format"));
        }

        // Pre-validate module memory maximums against configured limit (best-effort)
        // If the module declares a memory maximum larger than allowed, reject early.
        // Note: This does not replace runtime enforcement but prevents obviously unsafe modules.
        {
            let tmp_module = Module::new(&self.engine, wasm_bytes)
                .context("Failed to compile WASM module for inspection")?;
            let mut exceeds = false;
            for export in tmp_module.exports() {
                if let ExternType::Memory(mem_ty) = export.ty() {
                    if let Some(max_pages) = mem_ty.maximum() {
                        let max_bytes = (max_pages as usize) * (64 * 1024);
                        if max_bytes > self.memory_limit {
                            exceeds = true;
                            break;
                        }
                    }
                }
            }
            if exceeds {
                return Err(anyhow::anyhow!(
                    "WASM module declares memory larger than allowed (limit={} bytes)",
                    self.memory_limit
                ));
            }
        }

        // Clone data needed for the blocking task
        let wasm_bytes = wasm_bytes.to_vec();
        let input = input.to_string();
        let argv = argv.clone();
        let engine = self.engine.clone();
        let memory_limit = self.memory_limit;
        let table_elements_limit = self.table_elements_limit;
        let instances_limit = self.instances_limit;
        let memories_limit = self.memories_limit;
        let tables_limit = self.tables_limit;
        let fuel_limit = self.fuel_limit;
        let execution_timeout = self.execution_timeout;
        let allowed_paths = self.allowed_paths.clone();
        let env_vars = self.env_vars.clone();
        let allow_env_access = self.allow_env_access;

        // Start epoch ticker for timeout enforcement with cancellation support
        let engine_weak = Arc::downgrade(&self.engine);
        let (stop_tx, mut stop_rx) = tokio::sync::oneshot::channel::<()>();
        let ticker_handle = tokio::spawn(async move {
            let mut interval = tokio::time::interval(Duration::from_millis(100));
            loop {
                tokio::select! {
                    _ = interval.tick() => {
                        if let Some(engine) = engine_weak.upgrade() {
                            engine.increment_epoch();
                        } else {
                            break; // Engine dropped, stop ticking
                        }
                    }
                    _ = &mut stop_rx => {
                        break; // Stop requested after execution completes
                    }
                }
            }
        });

        // Run the WASM execution in a blocking thread to avoid async runtime conflicts
        let result = tokio::task::spawn_blocking(move || -> Result<String> {
            // Create WASI context with security-focused configuration
            let mut wasi_builder = WasiCtxBuilder::new();

            // Configure argv if provided (needed for Python WASM and other interpreters)
            if let Some(args) = argv {
                debug!("WASI: Setting argv: {:?}", args);
                wasi_builder.args(&args);
            }

            // Explicitly disable network access for security
            // WASI preview1 does not provide network capabilities by default.
            // Network socket operations will fail with ENOSYS (function not implemented).
            // This is enforced at the WASI capability level - no TCP/UDP/HTTP operations are possible.
            // Future WASI preview2 network proposals will require explicit opt-in.

            // Additional security: Don't inherit environment and stdio by default
            // Note: WasiCtxBuilder doesn't inherit by default, we only add what we explicitly allow

            // Configure environment variables (disabled by default for security)
            if allow_env_access {
                for (key, value) in &env_vars {
                    wasi_builder.env(key, value);
                }
                debug!("WASI: Allowed {} environment variables", env_vars.len());
            }

            // Configure filesystem access with read-only permissions by default
            for allowed_path in &allowed_paths {
                // Canonicalize path to prevent symlink escapes
                let canonical_path = match allowed_path.canonicalize() {
                    Ok(path) => path,
                    Err(e) => {
                        warn!(
                            "WASI: Failed to canonicalize path {:?}: {}",
                            allowed_path, e
                        );
                        continue;
                    }
                };

                // Verify the canonical path is still within allowed boundaries
                // This prevents symlink attacks that could escape the sandbox
                if !canonical_path.starts_with(allowed_path) && !allowed_path.starts_with("/tmp") {
                    warn!(
                        "WASI: Path {:?} resolves outside allowed directory",
                        allowed_path
                    );
                    continue;
                }

                if canonical_path.exists() && canonical_path.is_dir() {
                    wasi_builder.preopened_dir(
                        canonical_path.clone(),
                        canonical_path.to_string_lossy(),
                        DirPerms::READ,  // Read-only directory access
                        FilePerms::READ, // Read-only file access
                    )?;
                    debug!(
                        "WASI: Allowed read-only directory access to {:?} (canonical: {:?})",
                        allowed_path, canonical_path
                    );
                }
            }

            // Configure stdin/stdout/stderr using in-memory pipes for isolation and capture
            let stdin_pipe = MemoryInputPipe::new(input.as_bytes().to_vec());
            let stdout_pipe = MemoryOutputPipe::new(1024 * 1024); // 1MB buffer
            let stderr_pipe = MemoryOutputPipe::new(1024 * 1024);

            // Keep clones to read after execution
            let stdout_reader = stdout_pipe.clone();
            let stderr_reader = stderr_pipe.clone();

            wasi_builder
                .stdin(stdin_pipe)
                .stdout(stdout_pipe)
                .stderr(stderr_pipe);

            // Build a Preview1 context to use with the core wasm Linker
            let wasi_ctx = wasi_builder.build_p1();

            // Build store limits to enforce memory/table/instance caps
            let store_limits = wasmtime::StoreLimitsBuilder::new()
                .memory_size(memory_limit)
                .table_elements(table_elements_limit)
                .instances(instances_limit)
                .memories(memories_limit)
                .tables(tables_limit)
                .trap_on_grow_failure(false)
                .build();

            // Host context containing WASI and limits for the store
            struct HostCtx {
                wasi: wasmtime_wasi::p1::WasiP1Ctx,
                limits: wasmtime::StoreLimits,
            }

            // Compile module
            let module = Module::new(&engine, &wasm_bytes).map_err(|e| {
                warn!("WASI: Failed to compile WASM module: {}", e);
                anyhow::anyhow!("Failed to compile WASM module: {}", e)
            })?;

            // Create store with WASI context and resource limits
            let mut store = Store::new(
                &engine,
                HostCtx {
                    wasi: wasi_ctx,
                    limits: store_limits,
                },
            );
            // Attach limiter for memory/table growth
            store.limiter(|host| &mut host.limits);

            // Set fuel limit for CPU control
            store
                .set_fuel(fuel_limit)
                .context("Failed to set fuel limit")?;

            // TODO: Add memory limits via StoreLimits once lifetime issues are resolved
            // For now, we rely on the memory guard size set in engine config (64MB)
            debug!(
                "WASI: Memory limit configured via engine guard size: {}MB",
                64
            );

            // Set epoch deadline for timeout control
            let deadline_ticks = (execution_timeout.as_millis() / 100) as u64;
            store.set_epoch_deadline(deadline_ticks);

            // Create linker and add WASI so modules with WASI imports run correctly
            let mut linker: Linker<HostCtx> = Linker::new(&engine);
            wasmtime_wasi::p1::add_to_linker_sync(&mut linker, |t: &mut HostCtx| &mut t.wasi)
                .context("Failed to add WASI to linker")?;

            // Execute the module
            let instance = linker.instantiate(&mut store, &module).map_err(|e| {
                warn!("WASI: Failed to instantiate WASM module: {}", e);
                anyhow::anyhow!("Failed to instantiate WASM module: {}", e)
            })?;

            // Try to call _start (WASI main entry point)
            let execution_result = if let Some(start_func) = instance.get_func(&mut store, "_start")
            {
                debug!("WASI: Found _start function, executing module with security limits");
                start_func.call(&mut store, &[], &mut [])
            } else {
                warn!("WASI: No _start function found in module");
                return Err(anyhow::anyhow!("WASM module has no _start entry point"));
            };

            // Handle execution result
            match execution_result {
                Ok(_) => {
                    debug!("WASI: Module executed successfully within security constraints");
                    // Read captured stdout (best-effort)
                    let out_bytes = stdout_reader.contents();
                    let out = String::from_utf8_lossy(out_bytes.as_ref()).to_string();
                    if let Some(tool_executions) = TOOL_EXECUTIONS.get() {
                        tool_executions
                            .with_label_values(&["wasi", "success"])
                            .inc();
                    }
                    if let Some(tool_duration) = TOOL_DURATION.get() {
                        tool_duration
                            .with_label_values(&["wasi"])
                            .observe(start.elapsed().as_secs_f64());
                    }
                    Ok(out)
                }
                Err(e) => {
                    warn!("WASI: Module execution failed: {}", e);
                    let err_bytes = stderr_reader.contents();
                    let err_out = String::from_utf8_lossy(err_bytes.as_ref()).to_string();
                    let error_msg = format!("[WASI] Execution failed: {}\n{}", e, err_out);
                    if let Some(tool_executions) = TOOL_EXECUTIONS.get() {
                        tool_executions.with_label_values(&["wasi", "error"]).inc();
                    }
                    if let Some(tool_duration) = TOOL_DURATION.get() {
                        tool_duration
                            .with_label_values(&["wasi"])
                            .observe(start.elapsed().as_secs_f64());
                    }
                    Err(anyhow::anyhow!(error_msg))
                }
            }
        })
        .await
        .context("WASM execution task panicked")?;

        // Stop epoch ticker
        let _ = stop_tx.send(());

        // Wait for ticker to stop
        let _ = ticker_handle.await;

        result
    }

    pub fn validate_permissions(&self) -> Result<()> {
        for path in &self.allowed_paths {
            if !path.exists() {
                warn!("WASI: Allowed path does not exist: {:?}", path);
            } else if !path.is_dir() {
                return Err(anyhow::anyhow!(
                    "WASI: Allowed path is not a directory: {:?}",
                    path
                ));
            }
        }
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    // Minimal wasm binary exporting an empty `_start` function.
    // (module (type (func)) (func (type 0)) (export "_start" (func 0)) (code (func)))
    const MINIMAL_WASM: &[u8] = &[
        0x00, 0x61, 0x73, 0x6d, // \0asm
        0x01, 0x00, 0x00, 0x00, // version 1
        // Type section: id=1, size=4, count=1, functype 0x60, 0 params, 0 results
        0x01, 0x04, 0x01, 0x60, 0x00, 0x00,
        // Function section: id=3, size=2, count=1, type index=0
        0x03, 0x02, 0x01, 0x00,
        // Export section: id=7, size=10, count=1, name len=6, name="_start", kind=func(0), index=0
        0x07, 0x0a, 0x01, 0x06, 0x5f, 0x73, 0x74, 0x61, 0x72, 0x74, 0x00, 0x00,
        // Code section: id=10, size=4, count=1, body size=2, locals=0, end=0x0b
        0x0a, 0x04, 0x01, 0x02, 0x00, 0x0b,
    ];

    #[tokio::test]
    async fn test_wasi_sandbox_creation() {
        let sandbox = WasiSandbox::new().unwrap();
        assert!(sandbox.validate_permissions().is_ok());
    }

    #[tokio::test]
    async fn test_wasi_sandbox_configuration() {
        let sandbox = WasiSandbox::new()
            .unwrap()
            .allow_env_access(true)
            .set_env("TEST_VAR".to_string(), "test_value".to_string())
            .set_execution_timeout(Duration::from_secs(60));

        assert!(sandbox.allow_env_access);
        assert_eq!(sandbox.env_vars.get("TEST_VAR").unwrap(), "test_value");
        assert_eq!(sandbox.execution_timeout, Duration::from_secs(60));
    }

    #[tokio::test]
    async fn test_wasi_executes_minimal_wasm() {
        let sandbox = WasiSandbox::new().unwrap();
        let out = sandbox
            .execute_wasm(MINIMAL_WASM, "")
            .await
            .expect("minimal wasm should execute successfully");
        assert!(out.is_empty(), "expected empty stdout, got: {}", out);
    }
}
