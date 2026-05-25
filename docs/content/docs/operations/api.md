---
title: API Reference
prev: /docs/operations
weight: 3
---

TSDProxy exposes a REST API on the same HTTP server as the dashboard (port 8080
by default). All endpoints return JSON.

## Authentication

All API endpoints require authentication. There are three authentication methods:

1. **Tailscale identity** — automatic for connections through a Tailscale proxy.
   Tailnet users have viewer-level access (read endpoints); admin access requires
   the user's ID to be in the `admins` list.
2. **API key** — include `Authorization: Bearer <key>` header. API keys grant
   full admin access. Configure via `apiKey` or `apiKeyFile` in `tsdproxy.yaml`.
3. **Localhost** — requests from `127.0.0.0/8` or `::1` are permitted when
   `adminAllowLocalhost: true` is set (for bootstrapping only).

See [Admin Allowlist]({{< ref "/docs/security/admin-allowlist" >}}) for full
authentication details.

Errors return:

```json
{ "message": "error description", "code": 403 }
```

## Proxies

### List all proxies

```http
GET /api/v1/proxies
```

Returns every visible proxy.

```json
{
  "proxies": [
    {
      "name": "myapp",
      "label": "My App",
      "status": "Running",
      "health": "Up",
      "healthLatency": "12ms",
      "url": "https://myapp.tailnet.ts.net",
      "category": "Production",
      "uptime": "2d 5h 30m",
      "ports": [
        {
          "name": "443/https",
          "proxyProtocol": "https",
          "proxyPort": 443,
          "targetUrl": "http://172.31.0.1:80",
          "tlsValidate": true,
          "isRedirect": false,
          "funnel": false
        }
      ],
      "tailscale": {
        "tags": "tag:production",
        "ephemeral": false,
        "runWebClient": false
      },
      "statusHistory": [
        { "status": "Running", "timestamp": "2026-05-13T10:30:00Z" }
      ],
      "targetProvider": "docker",
      "targetId": "abc123def456",
      "targetImage": "nginx:alpine"
    }
  ]
}
```

### Get a proxy

```http
GET /api/v1/proxies/{name}
```

Returns a single proxy by hostname. Same shape as the list entry above.

### Get proxy ports

```http
GET /api/v1/proxies/{name}/ports
```

Returns only the port configuration for a proxy.

```json
{
  "ports": [
    {
      "name": "443/https",
      "proxyProtocol": "https",
      "proxyPort": 443,
      "targetUrl": "http://172.31.0.1:80",
      "tlsValidate": true,
      "isRedirect": false,
      "funnel": false
    }
  ]
}
```

## Health & Version

### Health

```http
GET /api/health
```

Aggregated proxy health summary.

```json
{
  "total": 5,
  "running": 4,
  "stopped": 0,
  "error": 0,
  "paused": 1,
  "unhealthy": 0
}
```

### Version

```http
GET /api/version
```

TSDProxy build info.

```json
{
  "version": "2.0.0",
  "author": "almeidapaulopt",
  "isDirty": false
}
```

## Identity

### WhoAmI

```http
GET /api/whoami
```

Returns the caller's Tailscale identity. Requires a Tailscale connection —
direct TCP access returns 401. Use this to discover your `id` when
bootstrapping the [admin allowlist]({{< ref "/docs/security/admin-allowlist" >}}).

```json
{
  "id": "12345",
  "displayName": "Alice",
  "username": "alice@github",
  "profilePicUrl": "https://avatars.tailscale.com/..."
}
```

## Dashboard Actions

These endpoints are accessible to all authenticated users (viewer role).

### Toggle proxy pin

```http
POST /api/dashboard/pin/{name}
```

Pin or unpin a proxy in the dashboard. Pinned proxies appear at the top
of the list. The toggle is idempotent — calling it again unpins the proxy.

### Update preferences

```http
PUT /api/dashboard/preferences
```

Save dashboard preferences for the current user. Accepts a JSON body:

```json
{
  "dark": true,
  "view": "list",
  "sort": "name",
  "grouped": false,
  "filterStatus": "Running",
  "filterHealth": "Up",
  "pinned": ["myapp", "nas"]
}
```

All fields are optional — only included fields are updated. Preferences are
persisted at `{DataDir}/dashboard/preferences/{userID}.json`.

## Admin Actions

All admin endpoints require admin authorization (user in `admins` list or
API key authentication). Returns `{"status": "ok"}` on success unless
otherwise noted.

### Metrics

```http
GET /metrics
```

Prometheus metrics endpoint. Exports per-proxy request counters, latency
histograms, in-flight request gauges, and proxy status gauges. Protected
by admin middleware.

### Restart proxy

```http
POST /api/proxy/{name}/restart
```

Stops and re-creates the proxy using its current configuration.

### Pause proxy

```http
POST /api/proxy/{name}/pause
```

Closes all port listeners while keeping the Tailscale node alive.
The proxy status transitions to `Paused`.

### Resume proxy

```http
POST /api/proxy/{name}/resume
```

Reopens port listeners on a paused proxy. Status returns to `Running`.

### Re-authenticate proxy

```http
POST /api/proxy/{name}/reauth
```

Alias for restart. Triggers the Tailscale authentication flow if the
proxy is in `Authenticating` state.

### Test webhook

```http
POST /api/webhook/test
```

Sends a synthetic webhook event through all configured webhook URLs.
Useful for validating webhook configuration.

```json
{ "message": "test webhook sent" }
```

## SSE Streams

The dashboard UI uses Server-Sent Events — these are not part of the REST
API but are available for custom integrations.

| Endpoint | Description |
|---|---|
| `GET /stream` | Proxy status stream (htmx SSE with `hx-partial` updates) |
| `GET /stream/{name}/logs` | Per-proxy access log stream |
