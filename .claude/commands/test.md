Run tests for a specific component.

Usage: /test <component>

Where <component> is one of: go, rust, python, all

Steps:
- go: `cd go/orchestrator && go test -race ./...`
- rust: `cd rust/agent-core && cargo test`
- python: `cd python/llm-service && python3 -m pytest`
- all: Run all three in sequence
