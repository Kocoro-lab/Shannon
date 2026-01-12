//! WASM module cache for durable workflow execution.
//!
//! Provides an LRU cache for compiled WASM modules with:
//! - Size-limited caching (default 100MB)
//! - Module preloading on startup
//! - Lazy loading on first use
//! - Thread-safe concurrent access
//! - Module versioning support

use parking_lot::RwLock;
use std::path::PathBuf;
use std::sync::Arc;
use wasmtime::{Engine, Module};

/// Error types for WASM cache operations.
#[derive(Debug, thiserror::Error)]
pub enum CacheError {
    /// Module not found in cache or filesystem.
    #[error("Module not found: {0}")]
    ModuleNotFound(String),

    /// Failed to load WASM module.
    #[error("Failed to load WASM module: {0}")]
    LoadError(String),

    /// Cache size limit exceeded.
    #[error("Cache size limit exceeded: {current} bytes (limit: {limit} bytes)")]
    SizeLimitExceeded { current: usize, limit: usize },

    /// Invalid module version.
    #[error("Invalid module version: expected {expected}, got {actual}")]
    VersionMismatch { expected: String, actual: String },
}

/// Metadata for a cached WASM module.
#[derive(Debug, Clone)]
pub struct ModuleMetadata {
    /// Module name/identifier.
    pub name: String,
    /// Module version.
    pub version: String,
    /// Module size in bytes.
    pub size: usize,
    /// Last access timestamp.
    pub last_access: std::time::Instant,
    /// Number of times accessed.
    pub access_count: u64,
}

/// Entry in the WASM module cache.
struct CacheEntry {
    /// Compiled WASM module.
    module: Arc<Module>,
    /// Module metadata.
    metadata: ModuleMetadata,
}

/// LRU cache for WASM modules.
///
/// # Design
/// - Uses LRU eviction when size limit is reached
/// - Thread-safe with read-write lock
/// - Preloads modules on startup for low-latency access
/// - Lazy-loads modules on first use
///
/// # Example
/// ```rust,ignore
/// let cache = WasmCache::new(100 * 1024 * 1024, engine)?; // 100MB
///
/// // Preload modules
/// cache.preload(&["chain_of_thought", "research"])?;
///
/// // Get module (cached or lazy-load)
/// let module = cache.get("chain_of_thought", "1.0.0")?;
/// ```
pub struct WasmCache {
    /// WASM engine for compilation.
    engine: Arc<Engine>,
    /// Cache entries (module name -> entry).
    entries: Arc<RwLock<lru::LruCache<String, CacheEntry>>>,
    /// Maximum cache size in bytes.
    max_size: usize,
    /// Current cache size in bytes.
    current_size: Arc<RwLock<usize>>,
    /// Base directory for WASM modules.
    module_dir: PathBuf,
}

impl WasmCache {
    /// Create a new WASM cache.
    ///
    /// # Arguments
    /// * `max_size` - Maximum cache size in bytes (default: 100MB)
    /// * `engine` - WASM engine for module compilation
    ///
    /// # Errors
    /// Returns error if engine initialization fails
    pub fn new(max_size: usize, engine: Arc<Engine>) -> Result<Self, CacheError> {
        // Use unbounded LRU (we'll manage size manually)
        let entries = Arc::new(RwLock::new(lru::LruCache::unbounded()));

        Ok(Self {
            engine,
            entries,
            max_size,
            current_size: Arc::new(RwLock::new(0)),
            module_dir: PathBuf::from("target/wasm32-wasip1/release"),
        })
    }

    /// Create with default settings (100MB cache).
    ///
    /// # Errors
    /// Returns error if engine initialization fails
    pub fn default(engine: Arc<Engine>) -> Result<Self, CacheError> {
        Self::new(100 * 1024 * 1024, engine)
    }

    /// Set the base directory for WASM modules.
    #[must_use]
    pub fn with_module_dir(mut self, dir: PathBuf) -> Self {
        self.module_dir = dir;
        self
    }

    /// Preload modules on startup.
    ///
    /// # Arguments
    /// * `module_names` - List of module names to preload
    ///
    /// # Errors
    /// Returns error if any module fails to load
    pub fn preload(&self, module_names: &[&str]) -> Result<(), CacheError> {
        tracing::info!("Preloading {} WASM modules", module_names.len());

        for name in module_names {
            // Use latest version for preloading
            let version = "latest";
            match self.load_module(name, version) {
                Ok(_) => tracing::debug!("Preloaded module: {}", name),
                Err(e) => {
                    tracing::warn!("Failed to preload module {}: {}", name, e);
                    // Continue with other modules
                }
            }
        }

        Ok(())
    }

    /// Get a module from cache or load it.
    ///
    /// # Arguments
    /// * `name` - Module name
    /// * `version` - Module version
    ///
    /// # Returns
    /// Arc to the compiled WASM module
    ///
    /// # Errors
    /// Returns error if module cannot be found or loaded
    pub fn get(&self, name: &str, version: &str) -> Result<Arc<Module>, CacheError> {
        let cache_key = format!("{}:{}", name, version);

        // Try cache first (fast path)
        {
            let mut entries = self.entries.write();
            if let Some(entry) = entries.get_mut(&cache_key) {
                // Update access metadata
                entry.metadata.last_access = std::time::Instant::now();
                entry.metadata.access_count += 1;

                tracing::debug!(
                    "Cache hit: {} (accesses: {})",
                    cache_key,
                    entry.metadata.access_count
                );

                return Ok(Arc::clone(&entry.module));
            }
        }

        // Cache miss - load module (slow path)
        tracing::debug!("Cache miss: {} - loading", cache_key);
        self.load_module(name, version)
    }

    /// Load a WASM module from filesystem.
    fn load_module(&self, name: &str, version: &str) -> Result<Arc<Module>, CacheError> {
        let cache_key = format!("{}:{}", name, version);

        // Construct module path
        let module_path = if version == "latest" {
            self.module_dir.join(format!("{}.wasm", name))
        } else {
            self.module_dir.join(format!("{}-{}.wasm", name, version))
        };

        // Check if file exists
        if !module_path.exists() {
            return Err(CacheError::ModuleNotFound(cache_key));
        }

        // Load and compile module
        let module_bytes = std::fs::read(&module_path).map_err(|e| {
            CacheError::LoadError(format!("Failed to read {}: {}", module_path.display(), e))
        })?;

        let module_size = module_bytes.len();

        let module = Module::from_binary(&self.engine, &module_bytes)
            .map_err(|e| CacheError::LoadError(format!("Failed to compile {}: {}", name, e)))?;

        let module = Arc::new(module);

        // Check cache size and evict if necessary
        self.ensure_cache_space(module_size)?;

        // Create cache entry
        let entry = CacheEntry {
            module: Arc::clone(&module),
            metadata: ModuleMetadata {
                name: name.to_string(),
                version: version.to_string(),
                size: module_size,
                last_access: std::time::Instant::now(),
                access_count: 1,
            },
        };

        // Insert into cache
        {
            let mut entries = self.entries.write();
            let mut current_size = self.current_size.write();

            if let Some((_old_key, old_entry)) = entries.push(cache_key.clone(), entry) {
                // Evicted old entry, reduce size
                *current_size = current_size.saturating_sub(old_entry.metadata.size);
                tracing::debug!("Evicted old entry: {}", cache_key);
            }

            *current_size += module_size;
        }

        tracing::info!(
            "Loaded and cached module: {} ({} bytes, cache: {}/{} bytes)",
            cache_key,
            module_size,
            *self.current_size.read(),
            self.max_size
        );

        Ok(module)
    }

    /// Ensure cache has space for a new module.
    fn ensure_cache_space(&self, required_size: usize) -> Result<(), CacheError> {
        let mut current_size = self.current_size.write();

        // Check if we have space
        if *current_size + required_size <= self.max_size {
            return Ok(());
        }

        // Evict LRU entries until we have space
        let mut entries = self.entries.write();
        let mut evicted_size = 0;

        while *current_size + required_size - evicted_size > self.max_size {
            if let Some((_key, entry)) = entries.pop_lru() {
                evicted_size += entry.metadata.size;
                tracing::debug!(
                    "Evicting LRU module: {} ({} bytes)",
                    entry.metadata.name,
                    entry.metadata.size
                );
            } else {
                // Cache is empty but still not enough space
                return Err(CacheError::SizeLimitExceeded {
                    current: required_size,
                    limit: self.max_size,
                });
            }
        }

        *current_size = current_size.saturating_sub(evicted_size);

        tracing::info!(
            "Evicted {} bytes to make space for new module",
            evicted_size
        );

        Ok(())
    }

    /// Get cache statistics.
    #[must_use]
    pub fn stats(&self) -> CacheStats {
        let entries = self.entries.read();
        let current_size = *self.current_size.read();

        let mut total_accesses = 0u64;
        let mut modules = Vec::new();

        for (_key, entry) in entries.iter() {
            total_accesses += entry.metadata.access_count;
            modules.push(entry.metadata.clone());
        }

        CacheStats {
            module_count: entries.len(),
            total_size: current_size,
            max_size: self.max_size,
            total_accesses,
            modules,
        }
    }

    /// Clear all cached modules.
    pub fn clear(&self) {
        let mut entries = self.entries.write();
        let mut current_size = self.current_size.write();

        entries.clear();
        *current_size = 0;

        tracing::info!("Cleared WASM module cache");
    }

    /// Invalidate a specific module version.
    ///
    /// Use this when a module version is updated.
    pub fn invalidate(&self, name: &str, version: &str) {
        let cache_key = format!("{}:{}", name, version);

        let mut entries = self.entries.write();
        let mut current_size = self.current_size.write();

        if let Some(entry) = entries.pop(&cache_key) {
            *current_size = current_size.saturating_sub(entry.metadata.size);
            tracing::info!("Invalidated cached module: {}", cache_key);
        }
    }
}

/// Cache statistics.
#[derive(Debug, Clone)]
pub struct CacheStats {
    /// Number of modules in cache.
    pub module_count: usize,
    /// Total size of cached modules in bytes.
    pub total_size: usize,
    /// Maximum cache size in bytes.
    pub max_size: usize,
    /// Total number of module accesses.
    pub total_accesses: u64,
    /// Metadata for all cached modules.
    pub modules: Vec<ModuleMetadata>,
}

impl std::fmt::Debug for WasmCache {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        let stats = self.stats();
        f.debug_struct("WasmCache")
            .field("max_size", &self.max_size)
            .field("current_size", &stats.total_size)
            .field("module_count", &stats.module_count)
            .field("module_dir", &self.module_dir)
            .finish()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn create_test_engine() -> Arc<Engine> {
        Arc::new(Engine::default())
    }

    #[test]
    fn test_cache_creation() {
        let engine = create_test_engine();
        let cache = WasmCache::new(100 * 1024 * 1024, engine).unwrap();

        let stats = cache.stats();
        assert_eq!(stats.module_count, 0);
        assert_eq!(stats.total_size, 0);
        assert_eq!(stats.max_size, 100 * 1024 * 1024);
    }

    #[test]
    fn test_cache_with_custom_dir() {
        let engine = create_test_engine();
        let cache = WasmCache::new(100 * 1024 * 1024, engine)
            .unwrap()
            .with_module_dir(PathBuf::from("/custom/path"));

        assert_eq!(cache.module_dir, PathBuf::from("/custom/path"));
    }

    #[test]
    fn test_cache_clear() {
        let engine = create_test_engine();
        let cache = WasmCache::new(100 * 1024 * 1024, engine).unwrap();

        cache.clear();

        let stats = cache.stats();
        assert_eq!(stats.module_count, 0);
        assert_eq!(stats.total_size, 0);
    }

    #[test]
    fn test_cache_invalidate() {
        let engine = create_test_engine();
        let cache = WasmCache::new(100 * 1024 * 1024, engine).unwrap();

        // Invalidating non-existent module should not panic
        cache.invalidate("test_module", "1.0.0");

        let stats = cache.stats();
        assert_eq!(stats.module_count, 0);
    }

    #[test]
    fn test_module_not_found_error() {
        let engine = create_test_engine();
        let cache = WasmCache::new(100 * 1024 * 1024, engine).unwrap();

        let result = cache.get("nonexistent_module", "1.0.0");
        assert!(result.is_err());
        assert!(matches!(result.unwrap_err(), CacheError::ModuleNotFound(_)));
    }

    #[test]
    fn test_size_limit_exceeded_error() {
        let engine = create_test_engine();
        let cache = WasmCache::new(100, engine).unwrap(); // Very small cache

        // Try to ensure space for a huge module
        let result = cache.ensure_cache_space(1000);
        assert!(result.is_err());
        assert!(matches!(
            result.unwrap_err(),
            CacheError::SizeLimitExceeded { .. }
        ));
    }
}
