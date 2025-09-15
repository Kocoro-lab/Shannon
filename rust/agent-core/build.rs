use std::io::Result;
use std::path::Path;

fn main() -> Result<()> {
    // Ensure a usable `protoc` is available (vendored fallback)
    if std::env::var_os("PROTOC").is_none() {
        if let Ok(pb) = protoc_bin_vendored::protoc_bin_path() {
            std::env::set_var("PROTOC", pb);
        }
    }
    // Determine proto path - check if we're in Docker or local
    let proto_path = if Path::new("/protos").exists() {
        // Docker environment
        "/protos"
    } else {
        // Local development
        "../../protos"
    };

    let common_proto = format!("{}/common/common.proto", proto_path);
    let agent_proto = format!("{}/agent/agent.proto", proto_path);

    // Compile protobuf files with reflection support
    tonic_build::configure()
        .build_server(true)
        .build_client(true)
        .file_descriptor_set_path(std::env::var("OUT_DIR").unwrap() + "/shannon_descriptor.bin")
        .compile_protos(&[&common_proto, &agent_proto], &[proto_path])?;
    Ok(())
}
