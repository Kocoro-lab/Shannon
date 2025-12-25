# Build stage
FROM rust:latest as builder

WORKDIR /usr/src/app

# Copy manifests and lockfile for deterministic builds
COPY rust/agent-core/Cargo.toml rust/agent-core/Cargo.lock ./
COPY rust/agent-core/build.rs ./

# Copy proto files for build.rs
COPY protos /protos

# Copy source code
COPY rust/agent-core/src ./src

# Install protoc
RUN apt-get update && apt-get install -y protobuf-compiler

# Build the application
RUN cargo build --release

# Runtime stage
FROM debian:bookworm-slim

RUN apt-get update && apt-get install -y \
    ca-certificates \
    netcat-openbsd \
    && rm -rf /var/lib/apt/lists/*

COPY --from=builder /usr/src/app/target/release/shannon-agent-core /usr/local/bin/shannon-agent-core

EXPOSE 50051

CMD ["shannon-agent-core"]
