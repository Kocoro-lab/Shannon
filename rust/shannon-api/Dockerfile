# Shannon API Dockerfile
# Multi-stage build for optimized production image

# Stage 1: Build
# Use Rust 1.92+ to support edition2024 required by some transitive dependencies
FROM rust:1.92-bookworm AS builder

WORKDIR /app

# Install build dependencies
RUN apt-get update && apt-get install -y \
    pkg-config \
    libssl-dev \
    protobuf-compiler \
    && rm -rf /var/lib/apt/lists/*

# Copy only Cargo files first for dependency caching
COPY Cargo.toml Cargo.lock ./

# Create a dummy main.rs to build dependencies
RUN mkdir -p src && \
    echo 'fn main() { println!("Dummy"); }' > src/main.rs && \
    echo 'pub fn dummy() {}' > src/lib.rs

# Build dependencies only (this layer is cached)
RUN cargo build --release && rm -rf src

# Copy actual source code
COPY src ./src

# Touch files to ensure rebuild
RUN touch src/main.rs src/lib.rs

# Build the actual application
RUN cargo build --release --features "gateway,database"

# Stage 2: Runtime
FROM debian:bookworm-slim AS runtime

WORKDIR /app

# Install runtime dependencies
RUN apt-get update && apt-get install -y \
    ca-certificates \
    libssl3 \
    curl \
    && rm -rf /var/lib/apt/lists/*

# Create non-root user
RUN useradd -m -s /bin/bash shannon

# Copy binary from builder
COPY --from=builder /app/target/release/shannon-api /usr/local/bin/shannon-api

# Copy configuration templates (if any exist)
# Note: This is optional - config may not exist in the builder stage
RUN mkdir -p /app/config

# Set ownership
RUN chown -R shannon:shannon /app

# Switch to non-root user
USER shannon

# Environment variables
ENV SHANNON_API_HOST=0.0.0.0
ENV SHANNON_API_PORT=8080
ENV SHANNON_API_ADMIN_PORT=8081
ENV RUST_LOG=info

# Expose ports
EXPOSE 8080 8081

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD curl -f http://localhost:8080/health || exit 1

# Run the application
ENTRYPOINT ["shannon-api"]
