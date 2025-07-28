# Variables
BINARY_NAME=confsync
VERSION?=latest
DOCKER_IMAGE=confsync:$(VERSION)
GHCR_IMAGE=ghcr.io/$(shell echo $(shell git config --get remote.origin.url) | sed 's/.*github\.com[:/]\([^/]*\/[^/]*\)\.git/\1/' | tr '[:upper:]' '[:lower:]')/confsync:$(VERSION)
GO_VERSION=1.24
ALPINE_VERSION=3.22
PLATFORMS=linux/amd64,linux/arm64

# Build arguments for Docker
BUILD_ARGS=--build-arg GO_VERSION=$(GO_VERSION) --build-arg ALPINE_VERSION=$(ALPINE_VERSION)

# Build targets
.PHONY: build clean test run docker docker-multiarch docker-push-ghcr help

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

# Run linter (requires golangci-lint to be installed)
lint:
	@echo "Running golangci-lint..."
	golangci-lint run --timeout=5m

# Fix linting issues automatically where possible
lint-fix:
	@echo "Running golangci-lint with --fix..."
	golangci-lint run --fix --timeout=5m

# Run all checks (format, lint, test) - use this before pushing
check: fmt lint test
	@echo "All checks passed! Ready to push."

# Run the application (requires CONFSYNC_URL environment variable)
run: build
	@echo "Running $(BINARY_NAME)..."
	./$(BINARY_NAME)

# Build Docker image for local testing (current platform only)
docker-local:
	@echo "Building Docker image for local testing (current platform)..."
	docker build \
		$(BUILD_ARGS) \
		--tag $(DOCKER_IMAGE) \
		.
	@echo "Local Docker image built with tag: $(DOCKER_IMAGE)"
	@echo "To run locally:"
	@echo "  docker run --rm $(DOCKER_IMAGE) --help"

# Build Docker image (multi-architecture by default)
docker:
	@echo "Building multi-architecture Docker image..."
	docker buildx create --name confsync-builder --use --bootstrap 2>/dev/null || docker buildx use confsync-builder
	docker buildx build \
		$(BUILD_ARGS) \
		--platform $(PLATFORMS) \
		--tag $(DOCKER_IMAGE) \
		--cache-from type=local,src=/tmp/.buildx-cache \
		--cache-to type=local,dest=/tmp/.buildx-cache,mode=max \
		.
	@echo "Multi-architecture image built with tag: $(DOCKER_IMAGE)"
	@echo "Note: Multi-arch images are not loaded into local Docker daemon"
	@echo "Use 'make docker-local' for local testing"

# Build multi-architecture images for GHCR.io and push
docker-push-ghcr:
	@echo "Building and pushing multi-architecture images to GHCR.io..."
	@echo "Image: $(GHCR_IMAGE)"
	docker buildx create --name confsync-builder --use --bootstrap 2>/dev/null || docker buildx use confsync-builder
	docker buildx build \
		$(BUILD_ARGS) \
		--platform $(PLATFORMS) \
		--tag $(GHCR_IMAGE) \
		--cache-from type=gha \
		--cache-to type=gha,mode=max \
		--provenance=true \
		--sbom=true \
		--push \
		.
	@echo "Multi-architecture image pushed to: $(GHCR_IMAGE)"

# Build multi-architecture images locally (GitHub Actions style)
docker-multiarch:
	@echo "Building multi-architecture images locally (GitHub Actions compatible)..."
	docker buildx create --name confsync-builder --use --bootstrap 2>/dev/null || docker buildx use confsync-builder
	docker buildx build \
		$(BUILD_ARGS) \
		--platform $(PLATFORMS) \
		--tag $(DOCKER_IMAGE) \
		--cache-from type=gha \
		--cache-to type=gha,mode=max \
		--provenance=true \
		--sbom=true \
		.
	@echo "Multi-architecture image built: $(DOCKER_IMAGE)"

# Setup buildx builder
buildx-setup:
	@echo "Setting up Docker buildx for multi-architecture builds..."
	docker buildx create --name confsync-builder --use --bootstrap || true
	docker buildx inspect --bootstrap

# Remove buildx builder
buildx-cleanup:
	@echo "Cleaning up buildx builder..."
	docker buildx rm confsync-builder 2>/dev/null || true

# Run with Docker
docker-run: docker-local
	@echo "Running with Docker..."
	docker run --rm -v $(PWD)/sync:/app/sync \
		-e CONFSYNC_URL=$(CONFSYNC_URL) \
		-e CONFSYNC_FILE_PATTERN=$(CONFSYNC_FILE_PATTERN) \
		-e CONFSYNC_VERBOSE=true \
		$(DOCKER_IMAGE)

# Show help
help:
	@echo "Available targets:"
	@echo "  build                - Build the binary for current platform"
	@echo "  build-all            - Build for multiple platforms"
	@echo "  clean                - Clean build artifacts"
	@echo "  test                 - Run tests"
	@echo "  fmt                  - Format code"
	@echo "  lint                 - Run golangci-lint (requires golangci-lint installed)"
	@echo "  lint-fix             - Run golangci-lint with --fix"
	@echo "  check                - Run fmt, lint, and test (use before pushing)"
	@echo "  run                  - Run the application"
	@echo "  docker-local         - Build Docker image for local testing (current platform)"
	@echo "  docker               - Build multi-architecture Docker image"
	@echo "  docker-multiarch     - Build multi-arch with GitHub Actions compatible caching"
	@echo "  docker-push-ghcr     - Build and push multi-arch images to GHCR.io"
	@echo "  docker-run           - Run with Docker"
	@echo "  docker-compose       - Start with Docker Compose"
	@echo "  docker-compose-down  - Stop Docker Compose"
	@echo "  buildx-setup         - Setup buildx for multi-arch builds"
	@echo "  buildx-cleanup       - Remove buildx builder"
	@echo "  health-check         - Test health endpoint"
	@echo "  ready-check          - Test readiness endpoint"
	@echo "  help                 - Show this help"
	@echo ""
	@echo "Dependencies required:"
	@echo "  - golangci-lint"
	@echo "  - docker"
	@echo "  - jq"
	@echo ""
	@echo "Environment variables:"
	@echo "  VERSION              - Image version tag (default: latest)"
	@echo "  GO_VERSION           - Go version for Docker build (default: 1.24)"
	@echo "  ALPINE_VERSION       - Alpine version for Docker build (default: 3.22)"
	@echo "  PLATFORMS            - Target platforms (default: linux/amd64,linux/arm64)"
	@echo ""
	@echo "Example usage:"
	@echo "  make build"
	@echo "  make docker                    # Build multi-arch locally"
	@echo "  make docker-push-ghcr          # Push to GHCR.io"
	@echo "  VERSION=v1.2.3 make docker     # Build with specific version"
