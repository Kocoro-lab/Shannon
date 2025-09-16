# Build stage
FROM golang:1.24-alpine AS builder

RUN apk add --no-cache git

WORKDIR /app

# Copy go.mod and go.sum
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Check if proto files exist, if not, provide clear error message
RUN if [ ! -d "internal/pb" ]; then \
      echo "ERROR: Proto files not found in internal/pb/"; \
      echo "Please run 'make proto' from the project root before building the Docker image"; \
      echo "Or ensure proto files are generated and committed to the repository"; \
      exit 1; \
    fi

# Build the application
RUN CGO_ENABLED=0 GOOS=linux go build -mod=mod -a -installsuffix cgo -o orchestrator .

# Runtime stage
FROM alpine:3.19

RUN apk --no-cache add ca-certificates

WORKDIR /root/

# Copy the binary from builder
COPY --from=builder /app/orchestrator .

EXPOSE 50052

CMD ["./orchestrator"]
