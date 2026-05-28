---
title: Health Check
prev: /docs/operations
weight: 1
---

TSDProxy has two layers of health checking: a **server readiness endpoint** for
external monitoring and **per-proxy backend health monitoring** that
automatically recovers from container restarts.

## Server Readiness Endpoint

```
GET /health/ready/
```

**Healthy (200):** `{"status":"OK"}`
**Not ready (503):** `{"status":"NOK"}`

## Docker HEALTHCHECK

The Docker image includes a HEALTHCHECK using the `healthcheck` binary that
calls the endpoint. The healthcheck binary reads `TSDPROXY_HTTP_PORT` to
determine which port to query. This environment variable is set automatically
by the main server binary from the `http.port` config value, so no manual
configuration is needed.

## External Monitoring

```bash
curl -f http://tsdproxy:8080/health/ready/
```

Replace `8080` with your configured `http.port` if different.

## Backend Health Monitoring

TSDProxy continuously monitors each proxy's backend target. When a container
restarts and gets a new IP address, TSDProxy detects the failure and
**automatically re-resolves the target** — no proxy restart, no Tailscale
teardown, no listener recreation.

### How It Works

1. **Health probes** — Each proxy's first non-redirect port is probed at a
   configurable interval (default: 30 seconds). HTTP/HTTPS targets are checked
   with a GET request; TCP targets with a connection attempt; UDP targets with
   a probe packet.
2. **Failure threshold** — When consecutive failures reach the configured
   threshold (default: 3), TSDProxy re-runs target resolution using the same
   code path used at startup.
3. **Hot-swap** — If the resolved target differs from the current one, it's
   swapped in place. Running connections continue on the old target; new
   connections use the updated address immediately.
4. **Backoff** — After each re-resolution attempt, the next attempt is delayed
   using exponential backoff (or a fixed cooldown). Any successful health check
   resets all counters immediately.

### Backoff Strategy

With default settings (30s interval, 3 failures, 0 cooldown):

| Attempt | Delay since last re-resolution |
|---------|-------------------------------|
| 1st | ~90s (3 × 30s consecutive failures) |
| 2nd | 30s |
| 3rd | 60s |
| 4th | 2min |
| 5th | 4min |
| 6th+ | 8min, 16min, … capped at 24h |

A successful health check at any point resets the counters and backoff.

With a fixed cooldown (e.g., `healthCheckCooldown: 120`), re-resolution fires
every 120 seconds while the target remains unhealthy, instead of using
exponential backoff.

### Configuration

Backend health monitoring can be configured per-provider (Docker or Lists) in
`tsdproxy.yaml` or overridden per-container with Docker labels.

| Setting | YAML (provider) | Docker Label | Default | Description |
|---------|-----------------|-------------|---------|-------------|
| Enable | `autoRestart` | `tsdproxy.auto_restart` | `true` | Enable/disable re-resolution on failure |
| Enabled | `healthCheckEnabled` | `tsdproxy.health_check_enabled` | `true` | Enable/disable health probes entirely |
| Interval | `healthCheckInterval` | `tsdproxy.health_check_interval` | `30` | Seconds between health probes |
| Failures | `healthCheckFailures` | `tsdproxy.health_check_failures` | `3` | Consecutive failures before re-resolution |
| Cooldown | `healthCheckCooldown` | `tsdproxy.health_check_cooldown` | `0` | Fixed cooldown in seconds (0 = exponential backoff) |

See [Docker labels]({{< ref "/docs/providers/docker#health-check-labels" >}}) or
[server configuration]({{< ref "/docs/serverconfig#docker-section" >}}) for examples.
