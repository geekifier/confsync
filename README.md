# confsync

Simple utility for syncing files exposed by a remote web server into a local directory. Useful for synchronizing configuration files.

This program polls a remote web server that provides a directory listing in JSON format, monitors for changes, and syncs files matching a user-provided regex pattern.

- [Use Cases](#use-cases)
- [Features](#features)
- [Directory Listing Format](#directory-listing-format)
- [Installation](#installation)
- [Configuration](#configuration)
- [Build \& Development](#build--development)
- [Health Endpoints](#health-endpoints)
- [Common Regex Patterns](#common-regex-patterns)
- [Security Considerations](#security-considerations)
- [Error Handling](#error-handling)
- [Contributing](#contributing)
- [License](#license)

## Use Cases

### Sync kubernetes-generated configuration to a remote host

The original use case for this program was to sync Gatus monitoring configs (defined as ConfigMaps on a Kubernetes cluster) to a mounted Docker Volume, where the configs are consumed by [gatus](https://github.com/TwiN/gatus). This way, the configs can be generated during the k8s gitops operations, but the actual monitoring happens off the cluster, closer to the end users.

Here's [an example](./examples/gatus.md) of that setup.

## Features

- **Regex-based file filtering**: Only sync files matching your specified pattern
- **Change detection**: Monitors file modification times and sizes to sync only when needed
- **Configurable polling**: Poll the remote server at specified intervals
- **Docker support**: Built for easy deployment within containers
- **Built-in health checks and metrics**: HTTP endpoints for status reporting and metrics
- **Atomic file operations**: Uses temporary files to ensure consistency
- **File cleanup**: Removes local files that no longer exist on the remote server
- **Flexible configuration**: Command-line flags and environment variables

## Directory Listing Format

The remote server should provide a JSON array with the following structure:

```json
[
  {
    "name": "config.yaml",
    "type": "file",
    "mtime": "Sun, 27 Jul 2025 04:23:20 GMT",
    "size": 167
  },
  {
    "name": "namespace_auth.configmap_authelia.yaml",
    "type": "file",
    "mtime": "Sun, 27 Jul 2025 04:23:23 GMT",
    "size": 266
  }
]
```

This is the default behavior of `nginx` when `autoindex_format json;` is specified in the config.

## Installation

### From Source

```bash
make build
./confsync -url https://example.com/api/files -dir ./sync -pattern "^.*\.y.*ml$"
```

### Using Docker

When using bind mounts (`-v "/host/someDir:/containerPath"`), ensure that the target directory exists first.

```bash
# manual execution
docker run -v $(pwd)/syncdir:/confsync \
  -e CONFSYNC_URL=https://example.com/api/files \
  -e CONFSYNC_FILE_PATTERN="^.*\.y.*ml$" \
  confsync
```

```yaml
# docker-compose.yaml
volumes:
  gatus-configs:

services:
  confsync:
    image: ghcr.io/geekifier/confsync:latest
    container_name: confsync
    environment:
      CONFSYNC_URL: https://gatus.my.domain.tld/config/
      CONFSYNC_FILE_PATTERN: '.*\.yaml'
      CONFSYNC_DELETE: true
    # Assign custom user id to the container
    # This would normally be an user with write access to the gatus-configs volume
    user: 1000:1000
    volumes:
      - gatus-configs:/confsync
    ports:
      - "8090:8080"
    restart: unless-stopped
  gatus:
    image: twinproduction/gatus:latest
    container_name: gatus
    depends_on:
      - confsync
    environment:
      GATUS_CONFIG_PATH: /config
      GATUS_DELAY_START_SECONDS: 2
    volumes:
      - gatus-configs:/config
    ports:
      # expose gatus on port 8089 of the host
      # if you are using a custom network and a reverse proxy, you don't need this
      - "8089:8080"
```

More examples are in the [examples](./examples/) directory.

## Configuration

### Command Line Flags

| Flag                | Environment Variable        | Default        | Description                                    |
| ------------------- | --------------------------- | -------------- | ---------------------------------------------- |
| `-url`              | `CONFSYNC_URL`              | _required_     | Remote server URL providing directory listing  |
| `-dir`              | `CONFSYNC_LOCAL_DIR`        | _required_     | Local directory to sync files to               |
| `-pattern`          | `CONFSYNC_FILE_PATTERN`     | `.*`           | Regex pattern to match files                   |
| `-interval`         | `CONFSYNC_POLL_INTERVAL`    | `60s`          | Polling interval                               |
| `-connect-timeout`  | `CONFSYNC_CONNECT_TIMEOUT`  | `10s`          | HTTP connection and listing timeout            |
| `-download-timeout` | `CONFSYNC_DOWNLOAD_TIMEOUT` | `0s`           | Maximum download time per file (0 = unlimited) |
| `-max-retries`      | `CONFSYNC_MAX_RETRIES`      | `3`            | Maximum number of retries for failed requests  |
| `-retry-delay`      | `CONFSYNC_RETRY_DELAY`      | `5s`           | Base delay for exponential backoff retries     |
| `-user-agent`       | `CONFSYNC_USER_AGENT`       | `confsync/1.0` | HTTP User-Agent header                         |
| `-delete`           | `CONFSYNC_DELETE`           | `false`        | Enable removal of local files not on remote    |
| `-verbose`          | `CONFSYNC_VERBOSE`          | `false`        | Enable verbose logging                         |
| `-health-port`      | `CONFSYNC_HEALTH_PORT`      | `8080`         | Port for health check endpoint (0 to disable)  |

### Timeout Behavior

The application uses two separate timeout mechanisms:

- **Connection Timeout** (`-connect-timeout`): Applied to directory listing requests and connection establishment. Default is 10 seconds.
- **Download Timeout** (`-download-timeout`): Maximum time allowed for downloading each individual file. Default is 0 (unlimited).

**Important**: When a new sync iteration begins, any in-progress downloads from the previous iteration are automatically cancelled to prevent resource conflicts and ensure timely sync operations.

### File Deletion Behavior

**By default, confsync does not delete local files** for safety. You must explicitly enable deletion with the `-delete` flag.

When file deletion is enabled (`-delete` flag), confsync will remove local files that no longer exist on the remote server (matching the configured pattern).

When file deletion is enabled, the sync process:

1. Fetches the remote directory listing
2. Identifies files to remove (in local directory but not in remote listing)
3. Removes obsolete files first
4. Downloads new/modified files
5. Updates the local cache

**WARNING**: When `-delete=true`, any files in the target directory that match the regex patterns, and do not exist on the remote server, WILL BE DELETED WITH EXTREME PREJUDICE.

## Build & Development

### Prerequisites

- Go 1.24+
- Docker (for container builds)
- Docker Buildx (for multi-architecture builds)

### Building

#### Local Binary

```bash
# Build for current platform
make build

# Build for all supported platforms
make build-all
```

#### Docker Images

```bash
# Build single-architecture image for local testing
make docker-local

# Build multi-architecture images (for CI/production)
make docker

# Build with GitHub Actions compatible caching
make docker-multiarch

# Build and push to GHCR.io with security attestations
make docker-push-ghcr
```

#### Customizing Versions

You can customize Go and Alpine versions used in Docker builds:

```bash
# Use custom versions for local build
GO_VERSION=1.23 ALPINE_VERSION=3.21 make docker-local

# Multi-arch with custom versions
GO_VERSION=1.23 ALPINE_VERSION=3.21 make docker
```

### Development Workflow

```bash
# Format code
make fmt

# Run tests
make test

# Build and run locally
make build
./confsync -url https://example.com/files -dir ./sync

# Run with Docker (builds local image first)
make docker-run CONFSYNC_URL=https://example.com/files
```

### Multi-Architecture Support

This project supports building Docker images for multiple architectures:

- **linux/amd64** - Intel/AMD 64-bit
- **linux/arm64** - ARM 64-bit (Apple Silicon, ARM servers)

**How Multi-Arch Images Work:**

Multi-architecture Docker images use a **manifest list** - a single tag that contains references to platform-specific images. When you run `docker run myimage:latest`, Docker automatically:

1. Checks your platform (e.g., Apple Silicon = `linux/arm64`)
2. Downloads the appropriate image variant from the manifest list
3. Runs the correct architecture without `-amd64` or `-arm64` tag suffixes needed

#### Local Multi-Arch Development

For local development with multi-architecture builds:

```bash
# Build both architectures (stores in buildx cache, not local Docker)
make docker

# For local testing, use single-arch build instead
make docker-local
docker run --rm confsync:latest --help
```

**Note**: Multi-architecture images use a single tag (`confsync:latest`) containing a manifest list that points to platform-specific images. Docker automatically selects the correct architecture for your platform when you run the image. You don't need separate `-amd64` or `-arm64` tagged images.

#### CI/CD Pipeline

The project includes comprehensive GitHub Actions workflows:

- **CI Pipeline** (`.github/workflows/ci.yml`):

  - Go testing across multiple versions
  - Multi-platform Docker builds
  - Security scanning with CodeQL and Trivy
  - Test coverage reporting
  - Automated builds on PRs and pushes

- **Docker Pipeline** (`.github/workflows/docker.yml`):

  - Multi-architecture Docker image builds
  - Automated tagging based on Git refs
  - GHCR (GitHub Container Registry) publishing
  - SBOM generation for security compliance

- **Release Pipeline** (`.github/workflows/release.yml`):
  - Automated releases on version tags
  - Cross-platform binary builds
  - Docker image publishing
  - Changelog generation
  - Release asset uploads

#### Release Process

1. Create and push a version tag:

   ```bash
   git tag v1.0.0
   git push origin v1.0.0
   ```

2. GitHub Actions will automatically:
   - Build binaries for all platforms
   - Build and push multi-arch Docker images
   - Create a GitHub release with changelog
   - Upload release assets

#### Available Make Targets

Run `make help` for a complete list of available targets:

- `build` - Build the binary for current platform
- `build-all` - Build for multiple platforms
- `test` - Run tests
- `docker-local` - Build Docker image for local testing (current platform)
- `docker` - Build multi-architecture Docker image
- `docker-multiarch` - Build multi-arch with GitHub Actions compatible caching
- `docker-push-ghcr` - Build and push multi-arch images to GHCR.io
- `buildx-setup` / `buildx-cleanup` - Manage Docker buildx

## Health Endpoints

When health checks are enabled (default port 8080), the following endpoints are available:

| Endpoint        | Description                                         |
| --------------- | --------------------------------------------------- |
| `/health`       | Comprehensive health status with metrics            |
| `/health/live`  | Liveness probe (same as `/health`)                  |
| `/health/ready` | Readiness probe (checks remote server connectivity) |
| `/metrics`      | Prometheus-compatible metrics                       |

### Health Status Response

```json
{
  "status": "healthy",
  "timestamp": "2025-07-27T10:30:00Z",
  "last_sync": "2025-07-27T10:29:30Z",
  "last_error": "",
  "synced_files": 42,
  "total_requests": 156,
  "failed_syncs": 2,
  "uptime": "2h30m15s",
  "config": {
    "remote_url": "https://example.com/files",
    "local_dir": "./sync",
    "file_pattern": "^.*\\.ya?ml$",
    "poll_interval": "30s"
  }
}
```

Status values:

- `healthy`: All operations working normally
- `degraded`: Recent errors but still functioning
- `unhealthy`: Persistent errors (returns HTTP 503)

### Example Usage

#### Sync YAML files every 30 seconds

```bash
./confsync \
  -url https://config-server.example.com/api/files \
  -pattern "^.*\.ya?ml$" \
  -interval 30s \
  -dir ./config \
  -verbose
```

#### Using environment variables

```bash
export CONFSYNC_URL=https://example.com/api/files
export CONFSYNC_LOCAL_DIR=./config
export CONFSYNC_FILE_PATTERN="^config.*\.json$"
export CONFSYNC_POLL_INTERVAL=15s
export CONFSYNC_VERBOSE=true

./confsync
```

## Common Regex Patterns

| Pattern       | Description                         |
| ------------- | ----------------------------------- |
| `^.*\.ya?ml$` | YAML files (.yaml and .yml)         |
| `^.*\.json$`  | JSON files                          |
| `^config.*`   | Files starting with "config"        |
| `.*\.conf$`   | Configuration files ending in .conf |

## Security Considerations

- The program runs as a non-root user in Docker containers
- Files are downloaded to temporary locations first, then atomically moved
- HTTP timeouts and retry limits prevent hanging connections
- If URLs you supply contain credentials/sensitive info, they may be logged, or exposed via metrics endpoints

## Error Handling

- **Network errors**: Automatic retry with configurable delays
- **File system errors**: Logged but don't stop the sync process
- **JSON parsing errors**: Logged and retried on next interval
- **Regex compilation errors**: Fatal error on startup

## Contributing

1. Fork the repository
2. Create a feature branch
3. Make your changes
4. Add tests
5. Submit a pull request

## License

See [LICENSE](./LICENSE).
