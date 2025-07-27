#!/bin/bash

# Multi-Architecture Docker Build Script for confsync
# This script helps build and push multi-architecture Docker images

set -e

# Default values
PLATFORMS="linux/amd64,linux/arm64"
VERSION="latest"
DOCKER_REGISTRY=""
BUILDER_NAME="confsync-builder"
PUSH=false
LOAD=false

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Helper functions
log_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

log_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

log_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Function to show usage
show_usage() {
    cat << EOF
Multi-Architecture Docker Build Script for confsync

Usage: $0 [OPTIONS]

Options:
    -p, --platforms PLATFORMS    Target platforms (default: linux/amd64,linux/arm64)
    -r, --registry REGISTRY      Docker registry for pushing images
    -v, --version VERSION        Image version tag (default: latest)
    -b, --builder BUILDER        Builder name (default: confsync-builder)
    --push                       Push images to registry
    --load                       Load single-platform image to local Docker
    --setup                      Setup buildx builder only
    --cleanup                    Cleanup buildx builder only
    -h, --help                   Show this help message

Examples:
    # Build for local use (single platform)
    $0 --platforms linux/amd64 --load

    # Build for multiple platforms (stored in buildx cache)
    $0 --platforms linux/amd64,linux/arm64

    # Build and push to GitHub Container Registry
    $0 --registry ghcr.io/username --push

    # Build for specific platforms and push
    $0 --platforms linux/amd64,linux/arm64,linux/arm/v7 --push --registry docker.io/username

    # Setup builder
    $0 --setup

    # Cleanup
    $0 --cleanup
EOF
}

# Function to setup buildx builder
setup_builder() {
    log_info "Setting up buildx builder: $BUILDER_NAME"
    
    if docker buildx inspect "$BUILDER_NAME" >/dev/null 2>&1; then
        log_warning "Builder $BUILDER_NAME already exists, using existing builder"
        docker buildx use "$BUILDER_NAME"
    else
        log_info "Creating new builder: $BUILDER_NAME"
        docker buildx create --name "$BUILDER_NAME" --use --bootstrap
    fi
    
    log_success "Builder setup completed"
    docker buildx inspect --bootstrap
}

# Function to cleanup buildx builder
cleanup_builder() {
    log_info "Cleaning up buildx builder: $BUILDER_NAME"
    docker buildx rm "$BUILDER_NAME" 2>/dev/null || log_warning "Builder $BUILDER_NAME not found or already removed"
    log_success "Builder cleanup completed"
}

# Function to build images
build_images() {
    log_info "Building multi-architecture Docker images"
    log_info "Platforms: $PLATFORMS"
    log_info "Version: $VERSION"
    
    # Ensure builder exists
    if ! docker buildx inspect "$BUILDER_NAME" >/dev/null 2>&1; then
        setup_builder
    else
        docker buildx use "$BUILDER_NAME"
    fi
    
    # Build command
    BUILD_ARGS=(
        "buildx" "build"
        "--platform" "$PLATFORMS"
    )
    
    # Add tags
    BUILD_ARGS+=("--tag" "confsync:$VERSION")
    if [ -n "$DOCKER_REGISTRY" ]; then
        BUILD_ARGS+=("--tag" "$DOCKER_REGISTRY/confsync:$VERSION")
    fi
    
    # Add push or load flag
    if [ "$PUSH" = true ]; then
        if [ -z "$DOCKER_REGISTRY" ]; then
            log_error "Registry must be specified when pushing"
            exit 1
        fi
        BUILD_ARGS+=("--push")
        log_info "Images will be pushed to: $DOCKER_REGISTRY/confsync:$VERSION"
    elif [ "$LOAD" = true ]; then
        # Load only works with single platform
        if [[ "$PLATFORMS" == *","* ]]; then
            log_error "Cannot load multi-platform build to local Docker"
            log_error "Options:"
            log_error "  1. Use single platform: --platforms linux/amd64"
            log_error "  2. Push to registry: --push --registry your-registry"
            log_error "  3. Build without --load (stores in buildx cache only)"
            exit 1
        fi
        BUILD_ARGS+=("--load")
        log_info "Image will be loaded to local Docker"
    else
        if [[ "$PLATFORMS" == *","* ]]; then
            log_warning "Multi-platform build will be stored in buildx cache only"
            log_warning "Use --push with --registry to push to a registry"
            log_warning "Or use single platform with --load for local Docker"
        fi
    fi
    
    # Add context
    BUILD_ARGS+=(".")
    
    # Execute build
    log_info "Executing: docker ${BUILD_ARGS[*]}"
    docker "${BUILD_ARGS[@]}"
    
    log_success "Build completed successfully!"
    
    # Show built images info
    if [ "$PUSH" = true ] && [ -n "$DOCKER_REGISTRY" ]; then
        log_info "Inspecting pushed manifest:"
        docker buildx imagetools inspect "$DOCKER_REGISTRY/confsync:$VERSION" || log_warning "Could not inspect remote manifest"
    fi
}

# Parse command line arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        -p|--platforms)
            PLATFORMS="$2"
            shift 2
            ;;
        -r|--registry)
            DOCKER_REGISTRY="$2"
            shift 2
            ;;
        -v|--version)
            VERSION="$2"
            shift 2
            ;;
        -b|--builder)
            BUILDER_NAME="$2"
            shift 2
            ;;
        --push)
            PUSH=true
            shift
            ;;
        --load)
            LOAD=true
            shift
            ;;
        --setup)
            setup_builder
            exit 0
            ;;
        --cleanup)
            cleanup_builder
            exit 0
            ;;
        -h|--help)
            show_usage
            exit 0
            ;;
        *)
            log_error "Unknown option: $1"
            show_usage
            exit 1
            ;;
    esac
done

# Validate arguments
if [ "$PUSH" = true ] && [ "$LOAD" = true ]; then
    log_error "Cannot use both --push and --load options"
    exit 1
fi

# Main execution
log_info "Starting multi-architecture build process"
build_images
log_success "All operations completed successfully!"
