//! WASM Sandbox Runtime with Capability Enforcement (Wasmtime v40)
//! -------------------------------------------------------------

use tokio::sync::oneshot;
use tokio::time::Duration;
use serde_json::Value;

use wasmtime::{
    Engine, Module, Store, Instance, Linker, Config,
};
use wasmtime_wasi::{
    WasiCtxBuilder, DirPerms, FilePerms,
};
// ... imports ...

// ... inside WasmSandbox ...

// ... impl WasiView removed ...
use wasmtime_wasi::p1::{self, WasiP1Ctx};

use crate::microsandbox::{
    error::*,
    policy::SandboxCapabilities,
};

/// Wrapper representing a WASM sandbox "runtime"
#[derive(Debug)]
pub struct WasmSandbox {
    pub engine: Engine,
    pub module: Module,
}

impl WasmSandbox {
    /// Load a module from bytes
    pub fn load(bytes: &[u8]) -> Result<Self> {
        let mut config = Config::new();

        // WASI + preview2/component model support
        config.wasm_multi_memory(true);
        config.wasm_simd(true);
        config.async_support(true);
        config.consume_fuel(true);

        // Allow WASM to import WASI functions
        let engine = Engine::new(&config)
            .map_err(|e| MicroVmError::Wasm(format!("Engine init: {}", e)))?;

        let module = Module::new(&engine, bytes)
            .map_err(|e| MicroVmError::Wasm(format!("Module load: {}", e)))?;

        Ok(Self { engine, module })
    }

    /// Instantiate a WASM process under a specific policy.
    pub async fn instantiate(
        &self,
        caps: SandboxCapabilities,
    ) -> Result<WasmProcess> {
        caps.validate()?;

        // Build WASI P1 context (this handles WasiCtx + ResourceTable + Adapter)
        let mut wasi_builder = WasiCtxBuilder::new();

        match &caps.env {
            crate::microsandbox::policy::EnvironmentCapability::None => {}
            crate::microsandbox::policy::EnvironmentCapability::AllowList(vars) => {
                for (k, v) in vars {
                    wasi_builder.env(k, v);
                }
            }
            crate::microsandbox::policy::EnvironmentCapability::AllowAll => {
                for (k, v) in std::env::vars() {
                    wasi_builder.env(&k, &v);
                }
            }
        }

        match &caps.fs {
            crate::microsandbox::policy::FileSystemCapability::None => {}
            crate::microsandbox::policy::FileSystemCapability::ReadOnly(dirs) => {
                for p in dirs {
                    wasi_builder.preopened_dir(p, p, DirPerms::READ, FilePerms::READ)
                         .map_err(|e| MicroVmError::Io(format!("fs open: {}", e)))?;
                }
            }
            crate::microsandbox::policy::FileSystemCapability::ReadWrite(dirs) => {
                for p in dirs {
                    wasi_builder.preopened_dir(p, p, DirPerms::all(), FilePerms::all())
                        .map_err(|e| MicroVmError::Io(format!("fs open: {}", e)))?;
                }
            }
        }
        
        // Inherit Stdio for simpler debugging (optional, subject to policy?)
        // For now, let's inherit to see logs.
        wasi_builder.inherit_stdio();

        let wasi_p1_ctx = wasi_builder.build_p1();

        let mut store = Store::new(
            &self.engine,
            WasiState {
                p1: wasi_p1_ctx,
                caps: caps.clone(),
            },
        );

        let _fuel = caps.cpu_budget();
        // Wasmtime v40: add_fuel is directly on Store
        // store.as_context_mut().add_fuel(fuel).map_err(|e| MicroVmError::Wasm(format!("add_fuel: {}", e)))?;

        // Linker must be typed to WasiState
        let mut linker: Linker<WasiState> = Linker::new(&self.engine);
        
        // Register WASI P1 imports
        // This ensures the WASM module can call WASI functions
        p1::add_to_linker_async(&mut linker, |t| &mut t.p1)
             .map_err(|e| MicroVmError::Wasm(format!("WASI command linker: {}", e)))?;

        let instance = linker
            .instantiate_async(&mut store, &self.module)
            .await
            .map_err(|e| MicroVmError::Wasm(format!("instantiate: {}", e)))?;

        let (kill_tx, kill_rx) = oneshot::channel::<()>();
        let pid = 1234;

        tokio::spawn(async move {
            tokio::select! {
                _ = tokio::time::sleep(Duration::from_millis(caps.timeout_ms)) => {
                    eprintln!("[MicroSandbox] PID {} timed out", pid);
                    let _ = kill_tx.send(());
                }
            }
        });

        Ok(WasmProcess {
            store,
            instance,
            capabilities: caps,
            kill_rx: Some(kill_rx),
        })
    }
}

pub struct WasiState {
    pub p1: WasiP1Ctx,
    pub caps: SandboxCapabilities,
}


// Debug implementation for WasiState
impl std::fmt::Debug for WasiState {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("WasiState")
            .field("caps", &self.caps)
            .finish()
    }
}

pub struct WasmProcess {
    pub store: Store<WasiState>,
    pub instance: Instance,
    pub capabilities: SandboxCapabilities,
    pub kill_rx: Option<oneshot::Receiver<()>>,
}

// Debug implementation for WasmProcess
impl std::fmt::Debug for WasmProcess {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("WasmProcess")
            .field("instance", &"Instance(...)")
            .field("capabilities", &self.capabilities)
            .finish()
    }
}

impl WasmProcess {
    pub fn pid(&self) -> usize {
        1234
    }

    pub fn kill(&mut self) {
        if let Some(mut rx) = self.kill_rx.take() {
            let _ = rx.close();
        }
    }

    pub async fn call_json(&mut self, func_name: &str, input: &Value) -> Result<Value> {
        self.enforce_syscall_policy(input)?;
        
        let func = self
            .instance
            .get_typed_func::<u32, u32>(&mut self.store, func_name)
            .map_err(|e| MicroVmError::Wasm(format!("func lookup '{}': {}", func_name, e)))?;

        let input_str = serde_json::to_string(input)
            .map_err(|e| MicroVmError::Wasm(format!("json encode: {}", e)))?;

        let ptr_in = self.write_string(&input_str).await?;

        let ptr_out = func
            .call_async(&mut self.store, ptr_in)
            .await
            .map_err(|e| MicroVmError::Wasm(format!("call '{}': {}", func_name, e)))?;

        let output_str = self.read_string(ptr_out).await?;

        let out_json: Value = serde_json::from_str(&output_str)
            .map_err(|e| MicroVmError::Wasm(format!("json parse: {}", e)))?;

        Ok(out_json)
    }

    fn enforce_syscall_policy(&self, input: &Value) -> Result<()> {
        if let Some(obj) = input.as_object() {
            if let Some(host) = obj.get("host") {
                if let Some(host_str) = host.as_str() {
                    self.store.data().caps.check_network_access(host_str)?;
                }
            }
        }
        Ok(())
    }

    async fn write_string(&mut self, s: &str) -> Result<u32> {
        let memory = self
            .instance
            .get_memory(&mut self.store, "memory")
            .ok_or_else(|| MicroVmError::Wasm("WASM memory not exported".into()))?;

        let bytes = s.as_bytes();
        let len = bytes.len() as u32;

        let alloc = self
            .instance
            .get_typed_func::<u32, u32>(&mut self.store, "alloc")
            .map_err(|e| MicroVmError::Wasm(format!("alloc: {}", e)))?;

        let ptr = alloc
            .call_async(&mut self.store, len)
            .await
            .map_err(|e| MicroVmError::Wasm(format!("alloc call: {}", e)))?;

        memory
            .write(&mut self.store, ptr as usize, bytes)
            .map_err(|e| MicroVmError::Io(format!("memory write: {}", e)))?;

        Ok(ptr)
    }

    async fn read_string(&mut self, ptr: u32) -> Result<String> {
        let memory = self
            .instance
            .get_memory(&mut self.store, "memory")
            .ok_or_else(|| MicroVmError::Wasm("WASM memory not exported".into()))?;

        let mut buf = Vec::new();
        let mut offset = ptr as usize;

        loop {
            let byte = {
                let mut tmp = [0u8; 1];
                memory
                    .read(&mut self.store, offset, &mut tmp)
                    .map_err(|e| MicroVmError::Io(format!("memory read: {}", e)))?;
                tmp[0]
            };

            if byte == 0 {
                break;
            }
            buf.push(byte);
            offset += 1;
        }

        String::from_utf8(buf)
            .map_err(|e| MicroVmError::Io(format!("utf8: {}", e)))
    }
}
