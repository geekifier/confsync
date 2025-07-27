# Variables
BINARY_NAME=confsync
VERSION?=latest
DOCKER_IMAGE=confsync:$(VERSION)
GO_VERSION=1.21

# Build targets
.PHONY: build clean test run docker docker-run docker-compose help

# Default target
all: build

# Build the binary
build:
	@echo "Building $(BINARY_NAME)..."
	go build -o $(BINARY_NAME) .

# Build for multiple platforms
build-all:
	@echo "Building for multiple platforms..."
	GOOS=linux GOARCH=amd64 go build -o $(BINARY_NAME)-linux-amd64 .
	GOOS=linux GOARCH=arm64 go build -o $(BINARY_NAME)-linux-arm64 .
	GOOS=windows GOARCH=amd64 go build -o $(BINARY_NAME)-windows.exe .
	GOOS=darwin GOARCH=amd64 go build -o $(BINARY_NAME)-darwin-amd64 .
	GOOS=darwin GOARCH=arm64 go build -o $(BINARY_NAME)-darwin-arm64 .

# Clean build artifacts
clean:
	@echo "Cleaning build artifacts..."
	rm -f $(BINARY_NAME) $(BINARY_NAME)-*
	docker rmi $(DOCKER_IMAGE) 2>/dev/null || true

# Run tests
test:
	@echo "Running tests..."
	go test -v ./...

# Format code
fmt:
	@echo "Formatting code..."
	go fmt ./...

# Run the application (requires CONFSYNC_URL environment variable)
run: build
	@echo "Running $(BINARY_NAME)..."
	./$(BINARY_NAME)

# Build Docker image
docker:
	@echo "Building Docker image $(DOCKER_IMAGE)..."
	docker build -t $(DOCKER_IMAGE) .

# Run with Docker
docker-run: docker
	@echo "Running with Docker..."
	docker run --rm -v $(PWD)/sync:/app/sync \
		-e CONFSYNC_URL=$(CONFSYNC_URL) \
		-e CONFSYNC_FILE_PATTERN=$(CONFSYNC_FILE_PATTERN) \
		-e CONFSYNC_VERBOSE=true \
		$(DOCKER_IMAGE)

# Run with Docker Compose
docker-compose:
	@echo "Starting with Docker Compose..."
	docker-compose up --build

# Stop Docker Compose
docker-compose-down:
	@echo "Stopping Docker Compose..."
	docker-compose down

# Test health endpoint (requires running application)
health-check:
	@echo "Checking health endpoint..."
	curl -s http://localhost:8080/health | jq '.' || curl -s http://localhost:8080/health

# Test readiness endpoint
ready-check:
	@echo "Checking readiness endpoint..."
	curl -s http://localhost:8080/health/ready | jq '.' || curl -s http://localhost:8080/health/ready

# Show help
help:
	@echo "Available targets:"
	@echo "  build          - Build the binary"
	@echo "  build-all      - Build for multiple platforms"
	@echo "  clean          - Clean build artifacts"
	@echo "  test           - Run tests"
	@echo "  fmt            - Format code"
	@echo "  run            - Run the application"
	@echo "  docker         - Build Docker image"
	@echo "  docker-run     - Run with Docker"
	@echo "  docker-compose - Start with Docker Compose"
	@echo "  docker-compose-down - Stop Docker Compose"
	@echo "  health-check   - Test health endpoint"
	@echo "  ready-check    - Test readiness endpoint"
	@echo "  help           - Show this help"
	@echo ""
	@echo "Environment variables for docker-run:"
	@echo "  CONFSYNC_URL           - Remote server URL (required)"
	@echo "  CONFSYNC_FILE_PATTERN  - File pattern regex (optional)"
	@echo ""
	@echo "Example usage:"
	@echo "  make build"
	@echo "  CONFSYNC_URL=https://example.com/files make run"
	@echo "  CONFSYNC_URL=https://example.com/files CONFSYNC_FILE_PATTERN='^.*\.yaml$$' make docker-run"
