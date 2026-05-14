---
title: Changelog
prev: /docs/faq
weight: 500
---

{{% steps %}}

### Unreleased

#### Changes

- Replaced Datastar with Vanilla JS + EventSource for SSE real-time updates
- Removed Datastar Go and JavaScript dependencies

### 2.0.0

#### Breaking changes

- Files provider renamed to **Lists** (config key changed from `files:` to `lists:`)
- Lists use a new YAML format supporting multiple ports and redirects

#### Deprecated Docker labels

- `tsdproxy.autodetect` — use per-port `no_autodetect` option instead
- `tsdproxy.container_port` — use the new port configuration syntax
- `tsdproxy.funnel` — use `tailscale_funnel` port option instead
- `tsdproxy.scheme` — use the new port configuration syntax
- `tsdproxy.tlsvalidate` — use per-port TLS settings instead

#### New features

- **Multi-port support** — expose multiple ports per container with granular protocol control
- **TCP port forwarding** — proxy raw TCP connections (SSH, databases, gRPC) through Tailscale
- **Tailscale Funnel** — expose services to the public internet via `tailscale_funnel` port option
- **Real-time dashboard** — SSE-powered web UI with live proxy status, search, and alphabetical sorting
- **OAuth authentication** — headless auth without using the dashboard
- **Interactive login** — manual Tailscale authentication when no auth key is configured
- **Tags on Tailscale hosts** — assign Tailscale tags to proxied machines
- **Docker Swarm support** — full support for Docker Swarm stacks
- **HTTP and HTTPS proxy modes** — per-port protocol selection
- **Multiple redirects** — configure multiple HTTP→HTTPS redirects per proxy
- **Tailscale user profile** — displayed in top-right of dashboard
- **Identity headers** — pass Tailscale identity headers (`x-tsdproxy-username`, `x-tsdproxy-displayname`, `x-tsdproxy-profilepicurl`) and standard auth headers (`Remote-User`, `X-Forwarded-User`, `X-Auth-Request-User`) to backend services
- **`no_autodetect` per-port option** — disable autodetection at the port level
- **`preventDuplicates`** — opt-in config to auto-remove stale Tailscale devices before creating new nodes (OAuth only)
- **Auto-detect `host.docker.internal`** — automatically detected when generating default config
- **Docker internal networks** — support via `tryDockerInternalNetwork` config option
- **Live config reload** — configuration changes take effect without restart
- **Health check endpoint** — `/health/ready/` for Docker HEALTHCHECK and orchestrators
- **Backup and restore** — operations for TSDProxy state persistence
- **Standalone deployment** — run as a binary outside Docker

#### Fixes

- Fix identity header spoofing: strip all identity headers from incoming requests before setting TSDProxy headers
- Fix race condition in duplicate-hostname proxy replacement
- Fix proxy manager context leak on zero-ports path causing WatchEvents goroutine leak
- Fix memory leak: events channel not closed on proxy shutdown
- Fix config file watcher: replace `log.Fatal` with error returns, honor context in list provider, fix session cookie `Secure` flag, survive atomic file replacement
- Fix health endpoint returning wrong status codes; buffer template rendering
- Fix dashboard SSE: unique connection IDs, buffer channels, escape template data, prevent send-on-closed-channel races
- Fix config: trim whitespace from auth keys, restrict file permissions, fix list provider reload
- Fix Tailscale: prevent panic on closed channel during shutdown, nil-deref on shutdown
- Fix OAuth single-use auth keys cached and reused after restart causing "Invalid API Key" errors
- Fix OAuth cached key validated against current tags and ephemeral settings
- Fix stale tsnet state auto-recovery on restart and after changing ephemeral flag
- Fix ephemeral nodes leaving stale state on disk causing "node key expired" on reboot
- Fix TCP target scheme: default to matching proxy protocol (tcp→tcp instead of tcp→http)
- Fix TCP goroutine leak: port handler connections not cleaned up on shutdown
- Fix TCP proxy SSE streaming: add `FlushInterval` for immediate event delivery
- Fix legacy label proxy: use HTTP for legacy labels to avoid ACME TLS cert failures on Docker bridge
- Fix Docker networking: extract `getTargetURL` into resolve helpers with deterministic IP selection
- Fix Docker redirect ports silently dropped when configured via labels
- Fix Docker containers with no published ports returning error when internal port is known
- Fix Docker event watcher panic: guard channel sends against consumer exit
- Fix healthcheck binary: use configurable port from `TSDPROXY_HTTP_PORT` instead of hardcoded 8080
- Fix stuck proxy in NeedsLogin state without auth URL — now shows as error in dashboard
- Fix logging: downgrade `NeedsLogin` without auth URL from error to info, suppress expected `context.Canceled` errors
- TLS certificate prefetch for faster proxy startup
- Readiness ordering: HTTP server waits for proxy manager
- Race conditions in proxy lifecycle (start/stop ordering)
- Hardened auth-key file path validation (symlink and non-regular file rejection)
- Improve "invalid key" error messages for auth failures and hardware attestation
- Warn on unrecognized Docker label port options
- Remove redundant `X-Forwarded-For` header copy
- Fix cross-page documentation links (use Hugo `ref` shortcodes)

#### Changes

- Migrated to Tailscale v2 client library
- Migrated Docker client from `docker/docker` to `moby/moby` sub-modules for improved type safety
- Unified icon download pipeline into reproducible JS script
- Upgraded datastar from v0.21.4 to v1.0.1
- Upgraded tailscale.com from v1.84.0 to v1.98.0
- Dependency updates: OpenTelemetry v1.36.0
- Comprehensive documentation overhaul
- Added comprehensive E2E test suite: basic proxy, health endpoints, label parsing, port config, Docker networking, cold-start discovery, TCP, WebSocket, HTTP method forwarding, auth keys, multi-provider, tags, funnel, persistence, reload, and web client tests
- Replaced `go test` with `gotestsum` for all test targets
- CI: bump Hugo version to 0.161.1 for hextra v0.12.2 compatibility

{{% /steps %}}
