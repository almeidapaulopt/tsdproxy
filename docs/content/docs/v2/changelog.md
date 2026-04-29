---
title: Changelog
weight: 500
---

{{% steps %}}

### 2.0.0-beta5

#### New features

- Auto-detect `host.docker.internal` when generating default config
- Support for Docker internal networks via `tryDockerInternalNetwork`

#### Fixes

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
