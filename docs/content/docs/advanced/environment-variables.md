---
title: Environment Variables
prev: /docs/advanced/docker-secrets
next: /docs/advanced/docker-networking
---

This page documents every environment variable that TSDProxy recognizes, including active variables used at runtime and legacy variables carried over from versions before v0.6.0.

## Active Variables

| Variable | Purpose | Default | Set By |
|----------|---------|---------|--------|
| `TSDPROXY_HTTP_PORT` | HTTP server port for the healthcheck binary | Value of `http.port` in config (defaults to `8080`) | Server binary (automatic) |
| `TSDPROXY_PPROF` | Enable Go pprof profiling endpoints | `"false"` | User |
| `DOCKER_HOST` | Docker daemon address (standard Docker variable) | `unix:///var/run/docker.sock` | User / Docker runtime |

### `TSDPROXY_HTTP_PORT`

Set automatically by the server binary on startup. It reads the `http.port` value from your config file and exports it so the healthcheck binary can reach the readiness endpoint at `http://127.0.0.1:<port>/health/ready/`.

You should not need to set this yourself. If the variable is empty (for example, when running the healthcheck binary standalone), it falls back to `8080`.

### `TSDPROXY_PPROF`

Set to `"true"` to enable Go's built-in profiling endpoints on the HTTP server. This is useful for debugging performance issues or memory leaks.

```yaml {filename="docker-compose.yaml"}
services:
  tsdproxy:
    image: almeidapaulopt/tsdproxy:2
    environment:
      TSDPROXY_PPROF: "true"
```

When enabled, the following endpoints become available:

| Endpoint | Purpose |
|----------|---------|
| `/debug/pprof/` | Profile index |
| `/debug/pprof/cmdline` | Command line |
| `/debug/pprof/profile` | CPU profile |
| `/debug/pprof/symbol` | Symbol table |
| `/debug/pprof/trace` | Execution trace |

> [!WARNING]
> pprof endpoints expose internal runtime data, including memory contents and goroutine stacks. Never enable this in production.

### `DOCKER_HOST`

The standard Docker environment variable. TSDProxy reads it during initial config generation to determine the Docker daemon address. If set, it overrides the default socket path. Most Docker installations set this automatically. If you need to point TSDProxy at a remote Docker host, set this variable or configure `docker.<name>.host` in your config file.

## Legacy Variables

The following environment variables are from versions prior to v0.6.0. They are read **only** during initial config generation when no config file exists yet. Once a config file has been generated, these variables have no effect.

| Variable | Purpose | Replaced By |
|----------|---------|-------------|
| `TSDPROXY_HOSTNAME` | HTTP hostname for Docker target resolution | `docker.<name>.targetHostname` in config |
| `TSDPROXY_AUTHKEY` | Tailscale auth key | `tailscale.providers.<name>.authKey` in config |
| `TSDPROXY_AUTHKEYFILE` | Path to file containing a Tailscale auth key | `tailscale.providers.<name>.authKeyFile` in config |
| `TSDPROXY_CONTROLURL` | Tailscale control server URL | `tailscale.providers.<name>.controlUrl` in config |
| `TSDPROXY_DATADIR` | Tailscale data directory | `tailscale.dataDir` in config |

> [!CAUTION]
> These variables are deprecated and will be removed in a future release. Migrate to the config file options described in [Server Configuration](../../serverconfig/).

## Docker Compose Example

```yaml {filename="docker-compose.yaml"}
services:
  tsdproxy:
    image: almeidapaulopt/tsdproxy:2
    container_name: tsdproxy
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
      - ./tsdproxy.yaml:/config/tsdproxy.yaml
      - ./data:/data
    environment:
      # Optional: enable pprof for debugging (do not use in production)
      TSDPROXY_PPROF: "true"
      # Optional: override Docker daemon address
      DOCKER_HOST: "unix:///var/run/docker.sock"
    ports:
      - "8080:8080"
    restart: unless-stopped
```

`TSDPROXY_HTTP_PORT` does not appear in this example because the server binary sets it automatically. It only needs to be present in the environment that the healthcheck binary runs in, which Docker handles through the same container environment.
