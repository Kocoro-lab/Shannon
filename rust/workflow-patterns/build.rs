//! Build script for workflow-patterns crate.
//!
//! Handles WASM module compilation and validation.

use std::env;
use std::path::PathBuf;

fn main() {
    // Detect if building for WASM target
    let target = env::var("TARGET").unwrap_or_default();
    let is_wasm = target.starts_with("wasm32");

    if is_wasm {
        println!("cargo:warning=Building for WASM target: {}", target);

        // Set WASM-specific configurations
        println!("cargo:rustc-cfg=wasm_target");

        // Ensure cdylib is used for WASM
        if target == "wasm32-wasi" {
            println!("cargo:rustc-cfg=wasi_target");
        }

        // Output WASM build artifacts location
        let out_dir = env::var("OUT_DIR").unwrap();
        let manifest_dir = env::var("CARGO_MANIFEST_DIR").unwrap();
        println!(
            "cargo:warning=WASM artifacts will be in: {}/target/wasm32-wasi/",
            manifest_dir
        );
    }

    // Rerun if build script changes
    println!("cargo:rerun-if-changed=build.rs");
    println!("cargo:rerun-if-changed=Cargo.toml");
}
