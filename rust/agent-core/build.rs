use std::path::Path;

fn main() -> Result<(), Box<dyn std::error::Error>> {
    // Ensure a usable `protoc` is available (vendored fallback)
    if std::env::var_os("PROTOC").is_none() {
        if let Ok(pb) = protoc_bin_vendored::protoc_bin_path() {
            unsafe {
                std::env::set_var("PROTOC", pb);
            }
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

    // Compile protobuf files with tonic-prost-build 0.14 API
    let proto_path_string = proto_path.to_string();
    tonic_prost_build::configure()
        .build_server(true)
        .build_client(true)
        .file_descriptor_set_path(format!("{}/shannon_descriptor.bin", std::env::var("OUT_DIR")?))
        .compile_protos(&[&common_proto, &agent_proto], &[&proto_path_string])?;
    Ok(())
}
