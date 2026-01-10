//! Build script for shannon-api.
//!
//! Generates Rust code from protobuf definitions for gRPC communication
//! with the Go orchestrator service.
//!
//! The gRPC code is only needed for cloud mode (Temporal via orchestrator).
//! In embedded mode, the Durable engine runs locally without gRPC.

fn main() -> Result<(), Box<dyn std::error::Error>> {
    // Only compile protos if grpc feature is enabled
    // This allows embedded mode to build without proto dependencies
    #[cfg(feature = "grpc")]
    {
        compile_protos()?;
    }

    Ok(())
}

#[cfg(feature = "grpc")]
fn compile_protos() -> Result<(), Box<dyn std::error::Error>> {
    use std::path::PathBuf;

    // Use vendored protoc to avoid system dependency
    std::env::set_var("PROTOC", protoc_bin_vendored::protoc_bin_path().unwrap());

    // Proto files to compile
    let protos = [
        "../../protos/orchestrator/orchestrator.proto",
        "../../protos/orchestrator/streaming.proto",
        "../../protos/common/common.proto",
    ];

    // Check if proto files exist (they might not in some build environments)
    let protos_exist = protos.iter().all(|p| {
        PathBuf::from(p).exists()
    });

    if !protos_exist {
        println!("cargo:warning=Proto files not found, skipping gRPC code generation");
        return Ok(());
    }

    // Include paths for imports
    let includes = ["../../protos"];

    // Output directory
    let out_dir = PathBuf::from("src/proto");
    std::fs::create_dir_all(&out_dir)?;

    // Use tonic_prost_build with the correct 0.14.x API (matching agent-core)
    tonic_prost_build::configure()
        .build_server(false) // We only need client code
        .build_client(true)
        .out_dir(&out_dir) // Output to src/proto for visibility
        .compile_protos(&protos, &includes)?;

    // Tell cargo to rerun if protos change
    for proto in &protos {
        println!("cargo:rerun-if-changed={proto}");
    }

    Ok(())
}
