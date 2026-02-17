Regenerate protobuf files after .proto changes.

Steps:
1. Run `make proto` to regenerate all proto files
2. Run `cd go/orchestrator && go mod tidy` to update Go dependencies
3. Run `cd rust/agent-core && cargo build` to verify Rust compiles
4. Run `make ci` to verify everything passes
