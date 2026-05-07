---
title: Changelog
prev: /docs/v2/faq
weight: 500
---

{{% steps %}}

### 2.0.0-rc1

#### New features

- Pass standard auth headers (`Remote-User`, `X-Forwarded-User`, `X-Auth-Request-User`, etc.) to reverse-proxy-aware backends (Authelia, OAuth2 Proxy, Traefik, FileBrowser)

#### Changes

- Documentation quality improvements and error fixes

### 2.0.0-beta7

#### New features

- Support `no_autodetect` as a per-port option (in addition to the container-level `tsdproxy.autodetect` label)
- Add opt-in `preventDuplicates` to auto-remove stale Tailscale devices before creating new nodes (OAuth only)

#### Fixes

- Fix identity header spoofing: strip all identity headers from incoming requests before setting TSDProxy headers
- Fix race condition in duplicate-hostname proxy replacement
- Fix proxy manager context leak on zero-ports path causing WatchEvents goroutine leak
- Fix config file watcher: replace `log.Fatal` with error returns, honor context in list provider, fix session cookie `Secure` flag
- Fix health endpoint returning wrong status codes; buffer template rendering
- Fix dashboard SSE: use unique connection IDs, buffer channels, escape template data
- Fix config: trim whitespace from auth keys, restrict file permissions, fix list provider reload
- Fix Tailscale: prevent panic on closed channel during shutdown
- Fix TCP target scheme: default to matching proxy protocol (tcp→tcp instead of tcp→http)
- Fix legacy label proxy: use HTTP for legacy labels to avoid ACME TLS cert failures on Docker bridge, revert to HTTPS for connectivity
- Fix Docker networking: extract `getTargetURL` into resolve helpers with deterministic IP selection
- Fix logging: downgrade `NeedsLogin` without auth URL from error to info
- Fix logging: suppress expected `context.Canceled` errors during shutdown
- Fix Docker Swarm secrets documentation: remove unnecessary Swarm requirement

#### Changes

- Migrated Docker client from `docker/docker` to `moby/moby` sub-modules for improved type safety
- Upgraded datastar from v0.21.4 to v1.0.1
- Upgraded tailscale.com from v1.96.5 to v1.98.0
- Replaced `go test` with `gotestsum` for all test targets
- Added comprehensive E2E test suite: basic proxy, health endpoints, label parsing, port config, Docker networking, cold-start discovery, TCP, WebSocket, HTTP method forwarding, auth keys, multi-provider, tags, funnel, persistence, reload, and web client tests

### 2.0.0-beta6

#### New features

- TCP port forwarding support: proxy raw TCP connections (SSH, databases, gRPC) through Tailscale
- Pass Tailscale identity headers (`x-tsdproxy-username`, `x-tsdproxy-displayname`, `x-tsdproxy-profilepicurl`) to backend services
- Interactive login: allow manual Tailscale authentication when no auth key is configured
- Development roadmap page in documentation

#### Fixes

- Fix OAuth single-use auth keys cached and reused after restart causing "Invalid API Key" errors
- Fix OAuth cached key validated against current tags and ephemeral settings (prevents tag mismatch)
- Fix stale tsnet state auto-recovery on restart
- Fix Docker redirect ports silently dropped when configured via labels
- Fix Docker containers with no published ports returning error when internal port is known
- Fix Docker event watcher panic: guard channel sends against consumer exit
- Fix dashboard SSE: prevent send-on-closed-channel races in status broadcast
- Fix proxy SSE streaming: add `FlushInterval` for immediate event delivery
- Fix TCP goroutine leak: port handler connections not cleaned up on shutdown
- Fix Tailscale watcher: close events channel and prevent nil-deref crashes
- Fix healthcheck binary: use configurable port from `TSDPROXY_HTTP_PORT` instead of hardcoded 8080
- Fix cross-page documentation links (use Hugo `ref` shortcodes)
- Warn on unrecognized Docker label port options
- Improve "invalid key" error messages for auth failures and hardware attestation
- Remove redundant `X-Forwarded-For` header copy

#### Changes

- Comprehensive documentation overhaul
- CI: bump Hugo version to 0.161.1 for hextra v0.12.2 compatibility

### 2.0.0-beta5

#### New features

- Auto-detect `host.docker.internal` when generating default config
- Support for Docker internal networks via `tryDockerInternalNetwork`
- Opt-in `preventDuplicates` config to auto-remove stale Tailscale devices before creating new nodes (OAuth only)

#### Fixes

- Fix memory leak: events channel not closed on proxy shutdown, leaking goroutines and object graphs
- Fix SSE streaming: reverse proxy now flushes immediately so Server-Sent Events reach the client
- Fix TCP goroutine leak: port handler connections were not cleaned up on shutdown
- Fix Docker event watcher panic: unbuffered channel sends blocked forever after consumer exit
- Fix dashboard SSE client race: closing message channel caused send-on-closed-channel panics
- Fix redirect ports silently dropped when configured via Docker labels
- Fix containers with no published ports returning error when internal port is known
- Fix OAuth cached key reused across proxies with different tags or ephemeral settings
- Fix OAuth single-use auth keys cached and reused after restart causing "Invalid API Key" errors
- Fix stale tsnet state after changing ephemeral flag — state is now auto-cleaned on config change
- Fix ephemeral nodes leaving stale state on disk after shutdown, causing "node key expired" on reboot
- Fix stuck proxy in NeedsLogin state without auth URL now shows as error in dashboard
- Fix healthcheck binary using hardcoded port 8080 — now reads `TSDPROXY_HTTP_PORT` from config
- Fix broken cross-page links in documentation site
- Warn on unrecognized Docker label port options (e.g. `no_autodetect`)
- Improve \"invalid key\" error message to mention hardware attestation and expired keys
- Tailscale watcher nil-deref on shutdown
- TLS certificate prefetch for faster proxy startup
- Readiness ordering (HTTP server waits for proxy manager)
- Config file watcher survives atomic replacement
- Race conditions in proxy lifecycle (start/stop ordering)
- Hardened auth-key file path validation

#### Changes

- Migrated to Tailscale v2 client library
- Unified icon download pipeline
- Dependency updates: tailscale.com v1.84.0, OpenTelemetry v1.36.0

### 2.0.0-beta4

#### New features

- Multiple ports in each Tailscale host
- Enable multiple redirects
- Proxies can use HTTP and HTTPS
- OAuth authentication without using the dashboard
- Assign Tags on Tailscale hosts
- Dashboard gets updated in real-time with SSE
- Search in the dashboard
- Dashboard proxies sorted alphabetically
- Add support for Docker Swarm stacks
- Tailscale user profile in top-right of Dashboard
- Pass Tailscale identity headers to destination services

#### Breaking changes

- Files provider renamed to **Lists** (key changed from `files:` to `lists:`)
- Lists use a new YAML format supporting multiple ports and redirects

{{% /steps %}}
