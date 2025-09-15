use std::env;
use std::fs;

#[tokio::main]
async fn main() -> anyhow::Result<()> {
    let args: Vec<String> = env::args().collect();
    if args.len() < 2 {
        eprintln!("Usage: cargo run --example wasi_hello -- <wasm_path> [stdin]");
        std::process::exit(2);
    }
    let wasm_path = &args[1];
    let stdin = if args.len() > 2 { &args[2] } else { "" };

    let bytes = fs::read(wasm_path)?;
    let sandbox = shannon_agent_core::wasi_sandbox::WasiSandbox::new()?;
    let out = sandbox.execute_wasm(&bytes, stdin).await?;
    print!("{}", out);
    Ok(())
}
