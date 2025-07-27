# Multi-Architecture Build and Release Guide

## 🚀 Quick Start

### Local Development

```bash
# Build for current platform
make build

# Run tests
make test

# Build Docker image for local testing (current platform)
make docker-local

# Build multi-architecture Docker image (for production/CI)
make docker

# Build with GitHub Actions compatible caching
make docker-multiarch
```

### Multi-Architecture Docker Builds

```bash
# Build single-arch for local testing
make docker-local

# Build multi-arch (no registry, for CI)
make docker

# Build and push to GHCR.io with security attestations
make docker-push-ghcr

# Build with specific version
VERSION=v1.2.3 make docker-local
```

### Supported Platforms

| Platform      | Architecture | Use Case                                 |
| ------------- | ------------ | ---------------------------------------- |
| `linux/amd64` | x86_64       | Intel/AMD servers, most cloud instances  |
| `linux/arm64` | aarch64      | ARM servers, Apple Silicon, AWS Graviton |

### Production Release

```bash
# Create and push a tag
git tag v1.0.0
git push origin v1.0.0
```
