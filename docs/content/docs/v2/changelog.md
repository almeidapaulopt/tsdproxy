---
title: Changelog
weight: 500
---

{{% steps %}}

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
