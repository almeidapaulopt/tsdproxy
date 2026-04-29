---
title: Development Roadmap
prev: /docs/v2/advanced
weight: 10
---

Ideas for future TSDProxy features, ordered by effort.

## Multi-Target Load Balancing

**Status**: Code-ready. `PortConfig.targets` is `[]*url.URL` — the struct
supports multiple backends but only the first is used today. Adding
round-robin across targets would be a small change in the proxy handler.

```yaml
# Proposed config
tsdproxy.port.1: "443/https:80/http,80/http,80/http"
# or in lists:
ports:
  443/https:
    targets:
      - http://backend1:8080
      - http://backend2:8080
      - http://backend3:8080
```

---

## Proxy Lifecycle Webhooks

**Status**: 7 proxy states already tracked in `model/status.go`. Add optional
URLs that receive HTTP POST on state transitions (started, stopped, error).

```yaml
# Proposed config
proxyname:
  webhooks:
    on_start: https://ntfy.sh/mytopic
    on_error: https://discord.webhook/url
```

**Effort**: Add webhook dispatch in `proxymanager/` lifecycle methods.

---

## Dashboard Authentication

**Status**: UUID session middleware already exists in `core/sessions.go`.
The dashboard at `:8080` has no auth beyond Tailscale network membership.
Add password/token or Tailscale identity check.

```yaml
# Proposed config
http:
  hostname: 0.0.0.0
  port: 8080
  dashboardAuth: token  # or password, oauth
  dashboardToken: "my-secret-token"
```

**Effort**: Minor middleware addition. Sessions already tracked.

---

## CLI Management Commands

**Status**: Binary only runs the server. Add `tsdproxy list`, `tsdproxy status`,
`tsdproxy restart <name>` via the existing HTTP endpoints.

```bash
tsdproxy --config /config/tsdproxy.yaml list
tsdproxy --config /config/tsdproxy.yaml restart myproxy
```

**Effort**: Add subcommands in `cmd/server/main.go` using existing HTTP routes.

---

## Metrics & Prometheus Endpoint

**Status**: OpenTelemetry is already imported (`go.opentelemetry.io/otel`).
Add `/metrics` with proxy count, request latency, error rates.

**Effort**: Wire up OTel meter, expose metrics handler on management port.

---

## Rate Limiting per Proxy

Protect backends from overload. Configurable per-port or per-proxy.

```yaml
# Proposed label
tsdproxy.port.1: "443/https:80/http, rate_limit=100/min"
```

**Effort**: Rate limiter middleware in the proxy handler.

---

## IP / User Access Control

Restrict which Tailscale users or IPs can access a proxy. Already have
WhoIs user identity resolution in the Tailscale provider.

```yaml
# Proposed label
tsdproxy.port.1: "443/https:80/http, allow_users=alice,bob"
tsdproxy.port.1: "443/https:80/http, allow_ips=100.64.0.0/10"
```

**Effort**: Middleware checking WhoIs identity against allow lists.

---

## Proxy Templates ???

Reusable named port configurations to reduce label verbosity.

```yaml
# /config/tsdproxy.yaml
templates:
  webapp:
    ports:
      - "443/https"
    options: "no_autodetect"
```

```yaml
# Docker label
tsdproxy.template: "webapp"
tsdproxy.port.1: "443/https:8080/http"
```

**Effort**: Template resolution in `targetproviders/docker/container.go`.

---

## DNS Challenge / Custom Domains

Allow custom domains via DNS-01 challenge instead of Tailscale MagicDNS
subdomains. Useful for services needing their own domain names.

**Effort**: Integrate with ACME provider library for DNS challenge support.

---

## Kubernetes Target Provider

Watch Kubernetes Ingresses or Services with `tsdproxy.*` annotations.
Same `TargetProvider` interface — add `targetproviders/kubernetes/`.

```yaml
# k8s annotation
metadata:
  annotations:
    tsdproxy.enable: "true"
    tsdproxy.name: "my-service"
```

**Effort**: New target provider using `client-go`. Largest market expansion.

---

## TCP / gRPC Proxying

Currently HTTP/HTTPS only. Tailscale supports raw TCP — allow proxying
databases, SSH, gRPC services.

```yaml
# Proposed config
tsdproxy.port.1: "5432/tcp:5432"
```

**Effort**: TCP reverse proxy in port handler, separate from HTTP handler.

---

## Tests

**Priority areas**: `internal/model/port.go` (port label parsing),
`internal/config/config.go` (validation), `internal/targetproviders/docker/`
(container label parsing), `internal/proxymanager/` (proxy lifecycle).

**Effort**: Foundational — makes all other features maintainable.
