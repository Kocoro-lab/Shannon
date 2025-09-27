# Memory System Integration Tests Makefile

.PHONY: all test mock integration clean help

# Default target
all: test

# Run all tests
test: mock integration

# Run mock tests (no services required)
mock:
	@echo "🧪 Running mock tests (no services required)..."
	@go test -v -run "^Test[^M][^e][^m]" . -timeout 2m

# Run integration tests (requires services)
integration: check-services
	@echo "🧪 Running integration tests (services required)..."
	@RUN_INTEGRATION_TESTS=true go test -v -run TestMemoryIntegrationSuite . -timeout 10m

# Run specific test suites
test-chunking:
	@echo "🧪 Testing chunking pipeline..."
	@RUN_INTEGRATION_TESTS=true go test -v -run TestMemoryIntegrationSuite/TestChunkingPipelineIntegration . -timeout 5m

test-batch:
	@echo "🧪 Testing batch embeddings..."
	@RUN_INTEGRATION_TESTS=true go test -v -run TestMemoryIntegrationSuite/TestBatchEmbeddingIntegration . -timeout 5m

test-mmr:
	@echo "🧪 Testing MMR diversity..."
	@RUN_INTEGRATION_TESTS=true go test -v -run TestMemoryIntegrationSuite/TestMMRDiversityIntegration . -timeout 5m

test-idempotency:
	@echo "🧪 Testing idempotency..."
	@RUN_INTEGRATION_TESTS=true go test -v -run TestMemoryIntegrationSuite/TestIdempotencyIntegration . -timeout 5m

test-reconstruction:
	@echo "🧪 Testing chunk reconstruction..."
	@RUN_INTEGRATION_TESTS=true go test -v -run TestMemoryIntegrationSuite/TestChunkReconstructionAccuracy . -timeout 5m

test-isolation:
	@echo "🧪 Testing session isolation..."
	@RUN_INTEGRATION_TESTS=true go test -v -run TestMemoryIntegrationSuite/TestCrossSessionIsolation . -timeout 5m

test-performance:
	@echo "🧪 Testing performance under load..."
	@RUN_INTEGRATION_TESTS=true RUN_PERFORMANCE_TESTS=true go test -v -run TestMemoryIntegrationSuite/TestPerformanceUnderLoad . -timeout 5m

test-hierarchical:
	@echo "🧪 Testing hierarchical memory..."
	@RUN_INTEGRATION_TESTS=true go test -v -run TestMemoryIntegrationSuite/TestHierarchicalMemoryIntegration . -timeout 5m

# Benchmark tests
benchmark:
	@echo "📊 Running benchmarks..."
	@go test -bench=. -benchmem -run=^$$ ../../go/orchestrator/internal/activities -timeout 10m

# Check if services are running
check-services:
	@echo "Checking required services..."
	@nc -z localhost 5432 || (echo "❌ PostgreSQL not running on port 5432" && exit 1)
	@nc -z localhost 6379 || (echo "❌ Redis not running on port 6379" && exit 1)
	@nc -z localhost 6333 || (echo "❌ Qdrant not running on port 6333" && exit 1)
	@nc -z localhost 8000 || (echo "❌ LLM Service not running on port 8000" && exit 1)
	@echo "✅ All required services are running"

# Start services
start-services:
	@echo "Starting services..."
	@cd ../.. && docker compose -f deploy/compose/docker-compose.yml up -d postgres redis qdrant llm-service orchestrator
	@echo "Waiting for services to be ready..."
	@sleep 10

# Stop services
stop-services:
	@echo "Stopping services..."
	@cd ../.. && docker compose -f deploy/compose/docker-compose.yml down

# Clean test cache
clean:
	@echo "Cleaning test cache..."
	@go clean -testcache

# Run tests with coverage
coverage:
	@echo "Running tests with coverage..."
	@RUN_INTEGRATION_TESTS=true go test -v -coverprofile=coverage.out -covermode=atomic ./...
	@go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"

# Run tests with race detection
race:
	@echo "Running tests with race detection..."
	@RUN_INTEGRATION_TESTS=true go test -v -race ./...

# Help
help:
	@echo "Shannon Memory System Integration Tests"
	@echo ""
	@echo "Available targets:"
	@echo "  make test              - Run all tests (mock + integration)"
	@echo "  make mock             - Run mock tests (no services needed)"
	@echo "  make integration      - Run integration tests (requires services)"
	@echo ""
	@echo "Specific test suites:"
	@echo "  make test-chunking    - Test chunking pipeline"
	@echo "  make test-batch       - Test batch embeddings"
	@echo "  make test-mmr         - Test MMR diversity"
	@echo "  make test-idempotency - Test idempotency"
	@echo "  make test-reconstruction - Test chunk reconstruction"
	@echo "  make test-isolation   - Test session isolation"
	@echo "  make test-performance - Test performance under load"
	@echo "  make test-hierarchical - Test hierarchical memory"
	@echo ""
	@echo "Other targets:"
	@echo "  make benchmark        - Run performance benchmarks"
	@echo "  make coverage         - Generate coverage report"
	@echo "  make race            - Run tests with race detection"
	@echo "  make start-services  - Start required services"
	@echo "  make stop-services   - Stop services"
	@echo "  make clean           - Clean test cache"
	@echo "  make help            - Show this help message"