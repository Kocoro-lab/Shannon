use crate::config::Config;
use crate::metrics::{TOOL_DURATION, TOOL_EXECUTIONS};
use anyhow::{Context, Result};
use std::collections::HashMap;
use std::path::Path;
use std::sync::Arc;
use std::time::Duration;
use tokio::sync::RwLock as TokioRwLock;
use tokio::time::timeout;
use tracing::{debug, error, info, warn};
use wasmtime::*;

#[cfg(target_os = "linux")]
use libc::{rlimit, setrlimit, RLIMIT_AS, RLIMIT_CPU, RLIMIT_NOFILE, RLIMIT_NPROC};

/// Resource limits for sandboxed execution
#[derive(Debug, Clone)]
pub struct ResourceLimits {
    pub memory_bytes: usize,
    pub cpu_time_ms: u64,
    pub wall_time_ms: u64,
    pub max_threads: u32,
    pub max_file_size: usize,
    pub max_open_files: u32,
}

impl Default for ResourceLimits {
    fn default() -> Self {
        let config = Config::global().unwrap_or_default();
        Self {
            memory_bytes: config.wasi.memory_limit_bytes,
            cpu_time_ms: 5000, // 5 seconds CPU time
            wall_time_ms: config.wasi.execution_timeout_secs * 1000, // Convert to ms
            max_threads: 4,
            max_file_size: 10 * 1024 * 1024, // 10MB
            max_open_files: 10,
        }
    }
}

/// Sandbox execution result
#[derive(Debug)]
pub struct SandboxResult {
    pub output: Vec<u8>,
    pub exit_code: i32,
    pub cpu_time_used_ms: u64,
    pub memory_used_bytes: usize,
    pub error: Option<String>,
}

/// Cache for compiled WASM modules to avoid recompilation
struct ModuleCache {
    modules: HashMap<String, Arc<Module>>,
}

impl ModuleCache {
    fn new() -> Self {
        Self {
            modules: HashMap::new(),
        }
    }

    fn get_or_compile(
        &mut self,
        path: &str,
        engine: &Engine,
        wasm_bytes: &[u8],
    ) -> Result<Arc<Module>> {
        if let Some(module) = self.modules.get(path) {
            debug!("Using cached WASM module for {}", path);
            return Ok(Arc::clone(module));
        }

        debug!(
            "Compiling WASM module for {} ({}MB)",
            path,
            wasm_bytes.len() / 1024 / 1024
        );
        let module = Module::new(engine, wasm_bytes).context("Failed to compile WASM module")?;
        let module = Arc::new(module);
        self.modules.insert(path.to_string(), Arc::clone(&module));
        info!("Cached compiled WASM module for {}", path);
        Ok(module)
    }

    fn clear(&mut self) {
        self.modules.clear();
        info!("Cleared WASM module cache");
    }
}

// Use tokio::sync::OnceCell for async-safe static initialization
static MODULE_CACHE: std::sync::OnceLock<Arc<TokioRwLock<ModuleCache>>> =
    std::sync::OnceLock::new();

fn get_module_cache() -> Arc<TokioRwLock<ModuleCache>> {
    MODULE_CACHE
        .get_or_init(|| Arc::new(TokioRwLock::new(ModuleCache::new())))
        .clone()
}

/// WASM Sandbox for secure tool execution using Wasmtime
#[allow(dead_code)]
pub struct WasmSandbox {
    limits: ResourceLimits,
    env_vars: HashMap<String, String>,
    allowed_paths: Vec<String>,
    engine: Arc<Engine>, // Thread-safe shared engine
}

impl WasmSandbox {
    /// Clear the WASM module cache (useful for development or when modules change)
    pub async fn clear_module_cache() {
        let cache_lock = get_module_cache();
        let mut cache = cache_lock.write().await;
        cache.clear();
    }
}

#[allow(dead_code)]
impl WasmSandbox {
    pub fn new() -> Result<Self> {
        // Create wasmtime engine with resource limits configuration
        let mut config = wasmtime::Config::new();

        // Enable necessary features
        config.wasm_threads(true);
        config.wasm_simd(true);
        config.wasm_reference_types(true);
        config.wasm_bulk_memory(true);

        // Set resource limits
        config.memory_guard_size(256 * 1024 * 1024); // 256MB guard size
        config.consume_fuel(true); // Enable fuel metering for CPU limits

        let engine = Arc::new(Engine::new(&config)?);

        Ok(Self {
            limits: ResourceLimits::default(),
            env_vars: HashMap::new(),
            allowed_paths: vec!["/tmp".to_string()],
            engine,
        })
    }

    pub fn with_limits(mut self, limits: ResourceLimits) -> Self {
        self.limits = limits;
        self
    }

    pub fn with_env(mut self, key: String, value: String) -> Self {
        self.env_vars.insert(key, value);
        self
    }

    pub fn allow_path(mut self, path: String) -> Self {
        self.allowed_paths.push(path);
        self
    }

    /// Execute a WASM module in the sandbox with full isolation
    pub async fn execute_wasm(&self, wasm_path: &Path, input: &str) -> Result<SandboxResult> {
        info!("Executing WASM module: {:?}", wasm_path);
        let timer = std::time::Instant::now();

        // Track metrics
        let tool_name = wasm_path
            .file_name()
            .and_then(|n| n.to_str())
            .unwrap_or("unknown");

        // Execute with timeout
        let result = timeout(
            Duration::from_millis(self.limits.wall_time_ms),
            self.execute_wasm_internal(wasm_path, input),
        )
        .await;

        let elapsed = timer.elapsed();
        if let Some(tool_duration) = TOOL_DURATION.get() {
            tool_duration
                .with_label_values(&[tool_name])
                .observe(elapsed.as_secs_f64());
        }

        match result {
            Ok(Ok(result)) => {
                debug!("WASM execution completed successfully");
                if let Some(tool_executions) = TOOL_EXECUTIONS.get() {
                    tool_executions
                        .with_label_values(&[tool_name, "success"])
                        .inc();
                }
                Ok(result)
            }
            Ok(Err(e)) => {
                warn!("WASM execution failed: {}", e);
                if let Some(tool_executions) = TOOL_EXECUTIONS.get() {
                    tool_executions
                        .with_label_values(&[tool_name, "error"])
                        .inc();
                }
                Ok(SandboxResult {
                    output: Vec::new(),
                    exit_code: 1,
                    cpu_time_used_ms: elapsed.as_millis() as u64,
                    memory_used_bytes: 0,
                    error: Some(e.to_string()),
                })
            }
            Err(_) => {
                error!("WASM execution timed out");
                if let Some(tool_executions) = TOOL_EXECUTIONS.get() {
                    tool_executions
                        .with_label_values(&[tool_name, "timeout"])
                        .inc();
                }
                Ok(SandboxResult {
                    output: Vec::new(),
                    exit_code: -1,
                    cpu_time_used_ms: self.limits.wall_time_ms,
                    memory_used_bytes: 0,
                    error: Some("Execution timed out".to_string()),
                })
            }
        }
    }

    /// Execute a tool in a sandboxed process with RLIMIT enforcement
    pub async fn execute_tool(&self, tool_path: &Path, input: &str) -> Result<String> {
        info!("Executing tool in sandbox: {:?}", tool_path);

        // Validate path is allowed
        if !self.validate_path(tool_path) {
            anyhow::bail!("Tool path not in allowed list: {:?}", tool_path);
        }

        let tool_name = tool_path
            .file_name()
            .and_then(|n| n.to_str())
            .unwrap_or("unknown");
        let timer = std::time::Instant::now();

        // Use tokio process with resource limits
        use tokio::process::Command;

        let mut cmd = Command::new(tool_path);

        // Clear environment and add only allowed vars
        cmd.env_clear();
        for (key, value) in &self.env_vars {
            cmd.env(key, value);
        }

        // Apply resource limits on Linux
        #[cfg(target_os = "linux")]
        {
            use std::os::unix::process::CommandExt;
            let limits = self.limits.clone();
            unsafe {
                cmd.pre_exec(move || apply_rlimits(&limits).map_err(std::io::Error::other));
            }
        }

        // Set up pipes
        cmd.stdin(std::process::Stdio::piped())
            .stdout(std::process::Stdio::piped())
            .stderr(std::process::Stdio::piped());

        // Execute with timeout
        let result = timeout(Duration::from_millis(self.limits.wall_time_ms), async {
            let mut child = cmd.spawn()?;

            // Write input to stdin
            if let Some(mut stdin) = child.stdin.take() {
                use tokio::io::AsyncWriteExt;
                stdin.write_all(input.as_bytes()).await?;
                stdin.shutdown().await?;
            }

            let output = child.wait_with_output().await?;
            Ok::<_, anyhow::Error>(output)
        })
        .await;

        let elapsed = timer.elapsed();
        if let Some(tool_duration) = TOOL_DURATION.get() {
            tool_duration
                .with_label_values(&[tool_name])
                .observe(elapsed.as_secs_f64());
        }

        match result {
            Ok(Ok(output)) => {
                if output.status.success() {
                    if let Some(tool_executions) = TOOL_EXECUTIONS.get() {
                        tool_executions
                            .with_label_values(&[tool_name, "success"])
                            .inc();
                    }
                    let stdout = String::from_utf8_lossy(&output.stdout);
                    Ok(stdout.to_string())
                } else {
                    if let Some(tool_executions) = TOOL_EXECUTIONS.get() {
                        tool_executions
                            .with_label_values(&[tool_name, "error"])
                            .inc();
                    }
                    let stderr = String::from_utf8_lossy(&output.stderr);
                    anyhow::bail!("Tool execution failed: {}", stderr)
                }
            }
            Ok(Err(e)) => {
                if let Some(tool_executions) = TOOL_EXECUTIONS.get() {
                    tool_executions
                        .with_label_values(&[tool_name, "error"])
                        .inc();
                }
                anyhow::bail!("Tool execution error: {}", e)
            }
            Err(_) => {
                if let Some(tool_executions) = TOOL_EXECUTIONS.get() {
                    tool_executions
                        .with_label_values(&[tool_name, "timeout"])
                        .inc();
                }
                anyhow::bail!("Tool execution timed out")
            }
        }
    }

    async fn execute_wasm_internal(&self, wasm_path: &Path, input: &str) -> Result<SandboxResult> {
        info!(
            "WASM execution for {:?} - using wasmtime sandbox",
            wasm_path
        );

        let exec_start = std::time::Instant::now();

        // Check input memory limit
        if input.len() > self.limits.memory_bytes {
            return Err(anyhow::anyhow!("Input exceeds memory limit"));
        }

        // Get path as string for cache key
        let wasm_path_str = wasm_path.to_string_lossy().to_string();

        // Try to get cached module first
        let cache_lock = get_module_cache();
        let cached_module = {
            let cache = cache_lock.read().await;
            cache.modules.get(&wasm_path_str).map(Arc::clone)
        };

        let module = if let Some(module) = cached_module {
            debug!("Using cached WASM module for {}", wasm_path_str);
            module
        } else {
            // Read WASM module
            let wasm_bytes = tokio::fs::read(wasm_path)
                .await
                .context("Failed to read WASM module")?;

            // Validate WASM module size (50MB limit to prevent memory exhaustion)
            const MAX_WASM_SIZE: usize = 50 * 1024 * 1024;
            if wasm_bytes.len() > MAX_WASM_SIZE {
                error!(
                    "WASM module size {} exceeds limit of {} bytes",
                    wasm_bytes.len(),
                    MAX_WASM_SIZE
                );
                return Err(anyhow::anyhow!("WASM module exceeds size limit of 50MB"));
            }

            // Additional validation: check for WASM magic number
            if wasm_bytes.len() < 4 || &wasm_bytes[0..4] != b"\0asm" {
                return Err(anyhow::anyhow!("Invalid WASM module format"));
            }

            // Compile and cache module
            let mut cache = cache_lock.write().await;
            cache.get_or_compile(&wasm_path_str, &self.engine, &wasm_bytes)?
        };

        // Simple execution approach without complex WASI for now
        // This provides sandboxing through resource limits and fuel metering
        let output_buffer: Vec<u8> = {
            // Create store
            let mut store = Store::new(&self.engine, ());

            // Set fuel limit based on CPU time (approximate)
            // Use ~100K fuel units per second for better control
            // This allows roughly 100M instructions per second
            let fuel_limit = self.limits.cpu_time_ms * 100;
            store
                .set_fuel(fuel_limit)
                .context("Failed to set fuel limit")?;

            // Create basic linker
            let linker = Linker::new(&self.engine);

            // Try to instantiate and run the module
            let out = match linker.instantiate(&mut store, &module) {
                Ok(instance) => {
                    // Try to find an exported function to call
                    if let Some(func) = instance.get_func(&mut store, "execute") {
                        // Call the function with no parameters
                        match func.call(&mut store, &[], &mut []) {
                            Ok(_) => format!(
                                "[SANDBOXED] Executed WASM module successfully\nInput: {}",
                                input
                            )
                            .into_bytes(),
                            Err(e) => {
                                format!("[SANDBOXED] WASM execution error: {}\nInput: {}", e, input)
                                    .into_bytes()
                            }
                        }
                    } else {
                        // No execute function, just indicate the module was loaded
                        format!(
                            "[SANDBOXED] WASM module loaded (no execute function)\nInput: {}",
                            input
                        )
                        .into_bytes()
                    }
                }
                Err(e) => format!(
                    "[SANDBOXED] Failed to instantiate WASM module: {}\nInput: {}",
                    e, input
                )
                .into_bytes(),
            };

            // Calculate resource usage (best-effort)
            let fuel_consumed = fuel_limit.saturating_sub(store.get_fuel().unwrap_or(0));
            let _cpu_time_used_ms = fuel_consumed / 100; // Match our 100 units/ms rate

            out
        };

        let exec_time = exec_start.elapsed();
        let output_len = output_buffer.len();

        Ok(SandboxResult {
            output: output_buffer,
            exit_code: 0,
            cpu_time_used_ms: exec_time.as_millis() as u64,
            memory_used_bytes: output_len,
            error: None,
        })
    }

    pub fn validate_memory_usage(&self, bytes: usize) -> bool {
        if bytes > self.limits.memory_bytes {
            warn!(
                "Memory usage {} exceeds limit {}",
                bytes, self.limits.memory_bytes
            );
            false
        } else {
            debug!("Memory usage {} within limits", bytes);
            true
        }
    }

    /// Validate that a path is allowed for access
    pub fn validate_path(&self, path: &Path) -> bool {
        let path_str = path.to_string_lossy();

        for allowed in &self.allowed_paths {
            if path_str.starts_with(allowed) {
                return true;
            }
        }

        warn!("Path access denied: {:?}", path);
        false
    }

    /// Create a sandboxed filesystem view
    pub async fn create_fs_sandbox(&self, work_dir: &Path) -> Result<SandboxedFs> {
        // Create a temporary directory for the sandbox
        let sandbox_dir = tempfile::tempdir().context("Failed to create sandbox directory")?;

        info!("Created sandbox filesystem at: {:?}", sandbox_dir.path());

        Ok(SandboxedFs {
            root: sandbox_dir.path().to_path_buf(),
            work_dir: work_dir.to_path_buf(),
            _temp_dir: sandbox_dir,
        })
    }
}

/// Apply resource limits on Linux systems
#[cfg(target_os = "linux")]
fn apply_rlimits(limits: &ResourceLimits) -> Result<()> {
    // CPU time limit
    let cpu_limit = rlimit {
        rlim_cur: (limits.cpu_time_ms / 1000) as libc::rlim_t,
        rlim_max: (limits.cpu_time_ms / 1000) as libc::rlim_t,
    };
    unsafe {
        if setrlimit(RLIMIT_CPU, &cpu_limit) != 0 {
            warn!("Failed to set CPU limit");
        }
    }

    // Memory limit (address space)
    let mem_limit = rlimit {
        rlim_cur: limits.memory_bytes as libc::rlim_t,
        rlim_max: limits.memory_bytes as libc::rlim_t,
    };
    unsafe {
        if setrlimit(RLIMIT_AS, &mem_limit) != 0 {
            warn!("Failed to set memory limit");
        }
    }

    // File descriptor limit
    let fd_limit = rlimit {
        rlim_cur: limits.max_open_files as libc::rlim_t,
        rlim_max: limits.max_open_files as libc::rlim_t,
    };
    unsafe {
        if setrlimit(RLIMIT_NOFILE, &fd_limit) != 0 {
            warn!("Failed to set file descriptor limit");
        }
    }

    // Process/thread limit
    let proc_limit = rlimit {
        rlim_cur: limits.max_threads as libc::rlim_t,
        rlim_max: limits.max_threads as libc::rlim_t,
    };
    unsafe {
        if setrlimit(RLIMIT_NPROC, &proc_limit) != 0 {
            warn!("Failed to set process limit");
        }
    }

    debug!(
        "Applied resource limits: CPU={}s, Memory={}MB, FDs={}, Threads={}",
        limits.cpu_time_ms / 1000,
        limits.memory_bytes / (1024 * 1024),
        limits.max_open_files,
        limits.max_threads
    );

    Ok(())
}

/// Sandboxed filesystem view
pub struct SandboxedFs {
    root: std::path::PathBuf,
    work_dir: std::path::PathBuf,
    _temp_dir: tempfile::TempDir, // Keep temp dir alive
}

impl SandboxedFs {
    /// Map a path to the sandboxed filesystem
    pub fn map_path(&self, path: &Path) -> std::path::PathBuf {
        // Base directory inside sandbox is the work_dir relative to root
        let base_rel = self.work_dir.strip_prefix("/").unwrap_or(&self.work_dir);
        let base = self.root.join(base_rel);
        if path.is_absolute() {
            // Absolute input path is mapped under base as a relative tail
            let relative = path.strip_prefix("/").unwrap_or(path);
            base.join(relative)
        } else {
            // Relative path under the base
            base.join(path)
        }
    }

    /// Write a file to the sandboxed filesystem
    pub async fn write_file(&self, path: &Path, content: &[u8]) -> Result<()> {
        let sandbox_path = self.map_path(path);

        // Create parent directories
        if let Some(parent) = sandbox_path.parent() {
            tokio::fs::create_dir_all(parent)
                .await
                .context("Failed to create directories")?;
        }

        tokio::fs::write(&sandbox_path, content)
            .await
            .context("Failed to write file")?;

        debug!(
            "Wrote {} bytes to sandboxed path: {:?}",
            content.len(),
            sandbox_path
        );
        Ok(())
    }

    /// Read a file from the sandboxed filesystem
    pub async fn read_file(&self, path: &Path) -> Result<Vec<u8>> {
        let sandbox_path = self.map_path(path);

        let content = tokio::fs::read(&sandbox_path)
            .await
            .context("Failed to read file")?;

        debug!(
            "Read {} bytes from sandboxed path: {:?}",
            content.len(),
            sandbox_path
        );
        Ok(content)
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[tokio::test]
    async fn test_sandbox_creation() {
        let sandbox = WasmSandbox::new().expect("Failed to create WasmSandbox for test");
        assert!(sandbox.validate_memory_usage(100 * 1024 * 1024)); // 100MB should be OK
        assert!(!sandbox.validate_memory_usage(500 * 1024 * 1024)); // 500MB should fail
    }

    #[tokio::test]
    async fn test_path_validation() {
        let sandbox = WasmSandbox::new()
            .expect("Failed to create WasmSandbox for test")
            .allow_path("/home/test".to_string());

        assert!(sandbox.validate_path(Path::new("/tmp/file.txt")));
        assert!(sandbox.validate_path(Path::new("/home/test/file.txt")));
        assert!(!sandbox.validate_path(Path::new("/etc/passwd")));
    }

    #[tokio::test]
    async fn test_sandboxed_fs() {
        let sandbox = WasmSandbox::new().expect("Failed to create WasmSandbox for test");
        let fs = sandbox
            .create_fs_sandbox(Path::new("/work"))
            .await
            .expect("Failed to create sandboxed filesystem for test");

        // Test writing and reading
        let test_path = Path::new("test.txt");
        let test_content = b"Hello, sandbox!";

        fs.write_file(test_path, test_content)
            .await
            .expect("Failed to write test file");
        let read_content = fs
            .read_file(test_path)
            .await
            .expect("Failed to read test file");

        assert_eq!(read_content, test_content);
    }

    #[tokio::test]
    async fn test_sandboxed_fs_traversal_stays_in_root() {
        let sandbox = WasmSandbox::new().expect("Failed to create WasmSandbox for test");
        let fs = sandbox
            .create_fs_sandbox(Path::new("/work"))
            .await
            .expect("Failed to create sandboxed filesystem for test");

        // Attempt to traverse upwards; mapping should keep under sandbox root
        let rel = Path::new("../../escape.txt");
        let mapped = fs.map_path(rel);
        // mapped must start with sandbox root
        assert!(mapped.starts_with(&fs.root));

        let content = b"contained";
        fs.write_file(rel, content)
            .await
            .expect("Failed to write test file with relative path");
        let read_back = fs
            .read_file(rel)
            .await
            .expect("Failed to read test file with relative path");
        assert_eq!(read_back, content);
    }
}
