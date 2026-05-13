---
title: API Reference
prev: /docs/operations
weight: 3
---

TSDProxy exposes a REST API on the same HTTP server as the dashboard (port 8080
by default). All endpoints return JSON.

## Authentication

Endpoints that modify state (restart, pause, resume, reauth, webhook test)
require admin authorization when an [admin allowlist]({{< ref "/docs/security/admin-allowlist" >}})
is configured. Read-only endpoints are always accessible.

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

## Admin Actions

All admin endpoints require authorization when an admin allowlist is
configured. Returns `{"status": "ok"}` on success.

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
| `GET /stream` | Proxy status stream (Datastar SSE, merges into dashboard DOM) |
| `GET /stream/{name}/logs` | Per-proxy access log stream |
