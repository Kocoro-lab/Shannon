# Build stage
FROM golang:1.24-alpine AS builder

WORKDIR /app

# Install dependencies
RUN apk add --no-cache git

# Copy go mod files
COPY go/orchestrator/go.mod go/orchestrator/go.sum ./go/orchestrator/
WORKDIR /app/go/orchestrator
RUN go mod download

# Copy source code
WORKDIR /app
COPY go/orchestrator ./go/orchestrator
COPY protos ./protos

# Build the gateway
WORKDIR /app/go/orchestrator
RUN CGO_ENABLED=0 GOOS=linux go build -o gateway cmd/gateway/main.go

# Final stage
FROM alpine:3.19

RUN apk --no-cache add ca-certificates wget

WORKDIR /app

# Copy binary from builder
COPY --from=builder /app/go/orchestrator/gateway /app/gateway

# Expose port
EXPOSE 8080

# Run the gateway
CMD ["/app/gateway"]