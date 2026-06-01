---
title: Development Roadmap
prev: /docs/advanced
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

## ~~Proxy Lifecycle Webhooks~~ ✅ Implemented

Webhook notifications for proxy status changes are now supported. See
[Notifications]({{< ref "/docs/v3/notifications" >}}) for documentation.

```yaml
webhooks:
  - url: "https://ntfy.sh/mytopic"
    type: ntfy
    events: [Running, Error, Stopped]
```

---

## ~~Dashboard Authentication~~ ✅ Implemented

Dashboard authentication with admin/viewer roles and API key support is now
implemented. See [Admin Allowlist]({{< ref "/docs/v3/security/admin-allowlist" >}})
for documentation.

```yaml
admins:
  - "12345"  # alice@github
apiKey: "my-secret-api-key"
```

---

## ~~Metrics & Prometheus Endpoint~~ ✅ Implemented

Prometheus metrics endpoint at `/metrics` with per-proxy request counters,
latency histograms, and proxy status gauges. Protected by admin middleware.

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

## ~~DNS Challenge / Custom Domains~~ ✅ Implemented

Custom domains are now supported with external DNS providers (Cloudflare) and
ACME/Let's Encrypt TLS certificate provisioning. See
[Custom Domains]({{< ref "/docs/v3/advanced/custom-domains" >}}) for documentation.

```yaml
dnsProviders:
  cloudflare:
    provider: cloudflare
    apiToken: "your-token"
tlsProviders:
  acme:
    provider: acme
    email: "admin@example.com"
```

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

## ~~TCP / gRPC Proxying~~ ✅ Implemented

Raw TCP proxying is now supported. See [TCP Proxy & SSH](./tcp-proxy) for
documentation and examples.

```yaml
tsdproxy.port.1: "5432/tcp:5432"
```

---

## Tests

**Priority areas**: `internal/model/port.go` (port label parsing),
`internal/config/config.go` (validation), `internal/targetproviders/docker/`
(container label parsing), `internal/proxymanager/` (proxy lifecycle).

**Effort**: Foundational — makes all other features maintainable.
