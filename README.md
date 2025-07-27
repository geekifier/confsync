# confsync

Simple utility for syncing files exposed by a remote web server into a local directory. Useful for synchronizing configuration files.

This program polls a remote web server that provides a directory listing in JSON format, monitors for changes, and syncs files matching a user-provided regex pattern.

## Use Cases

### Sync kubernetes-generated configuration to a remote host

The original use case for this program was to sync Gatus monitoring configs (defined as ConfigMaps on a Kubernetes cluster) to a mounted Docker Volume, where the configs are consumed by [Gatus](https://github.com/TwiN/gatus). This way, the configs can be generated during the k8s gitops operations, but the actual monitoring happens off the cluster, near the users.

TODO: Link to an example docker-compose Gatus config here.

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

```bash
docker run -v $(pwd)/sync:/app/sync \
  -e CONFSYNC_URL=https://example.com/api/files \
  -e CONFSYNC_LOCAL_DIR=/app/sync \
  -e CONFSYNC_FILE_PATTERN="^.*\.y.*ml$" \
  confsync
```

See also: [Docker Compose Example](#docker-compose)

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

#### Sync all files continuously (1 second interval)

```bash
./confsync \
  -url https://files.example.com/listing \
  -pattern ".*" \
  -interval 1s \
  -dir ./downloads
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

## Docker Examples

### Basic Docker Run

```bash
docker run -d \
  --name confsync \
  -v /host/path/sync:/app/sync \
  -e CONFSYNC_URL=https://example.com/api/files \
  -e CONFSYNC_LOCAL_DIR=/app/sync \
  -e CONFSYNC_FILE_PATTERN="^.*\.y.*ml$" \
  -e CONFSYNC_POLL_INTERVAL=30s \
  confsync
```

### Docker Compose

```yaml
version: "3.8"
services:
  confsync:
    build: .
    restart: unless-stopped
    environment:
      - CONFSYNC_URL=https://config.example.com/files
      - CONFSYNC_LOCAL_DIR=/app/sync
      - CONFSYNC_FILE_PATTERN=^.*\.ya?ml$
      - CONFSYNC_POLL_INTERVAL=30s
      - CONFSYNC_VERBOSE=true
    volumes:
      - ./config:/app/sync
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
- If URLs you supply contain credentials/sensitive info, they may be logged

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
