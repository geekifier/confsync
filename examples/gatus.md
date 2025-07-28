# Docker Compose setup for gatus

Below are some minimal examples of using `confsync` to enable dynamically-generated monitoring configuration from Kubernetes to be injected into a container running [gatus](https://github.com/TwiN/gatus).

## Option 1 - share volume from consync container (preferred)

In this example, the `confsync` volume owned by the `confsync` container is shared with the consuming container. Every time the confis is refreshed by confsync, gatus automatically reloads its own config.

Gatus can also create it's own sqlite DB in the volume, as confsync will not delete files not covered by the regex condition.

```yaml
# docker-compose.yaml
services:
  confsync:
    image: ghcr.io/geekifier/confsync:latest
    container_name: confsync
    environment:
      CONFSYNC_URL: https://gatus.my.domain.tld/config/
      CONFSYNC_FILE_PATTERN: '.*\.yaml'
      CONFSYNC_DELETE: true
    # optional: expose the health check and metrics port 8080 on host port 8090
    ports:
      - "8090:8080"
    restart: unless-stopped
  gatus:
    image: twinproduction/gatus:latest
    container_name: gatus
    depends_on:
      - confsync
    environment:
      # match the path of the synced files
      GATUS_CONFIG_PATH: /confsync
      GATUS_DELAY_START_SECONDS: 2
    volumes_from:
      - confsync
    ports:
      # expose gatus on port 8089 of the host
      # if you are using a custom network and a reverse proxy, you don't need this
      - "8089:8080"
```

## Option 2: mount the volume from host

In this example, you are creating a new named volume for the compose project, and then mounting it to both containers. The only downside of this approach, is that you need to ensure that the user running the `confsync` container has write permissions to that path.

By default, `confsync` runs as `nobody:nogroup`. Your options to handle the permissions are as follows:

- grant write permissions to nobody/nogroup
- change the user running the container to a user that has the write permissions to the target directory

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
