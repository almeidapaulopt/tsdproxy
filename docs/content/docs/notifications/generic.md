---
title: Generic Webhook
weight: 5
prev: /docs/notifications/slack
next: /docs/advanced
---

Send TSDProxy status alerts to any HTTP endpoint that accepts JSON. This works with custom scripts, automation platforms (n8n, Zapier, Home Assistant), or any service that ingests JSON webhooks.

## Configuration

```yaml {filename="/config/tsdproxy.yaml"}
webhooks:
  - url: "https://example.com/webhook"
    type: generic
    events:
      - Running
      - Stopped
      - Error
```

Omit `type` entirely — `generic` is the default when no type is specified.

## JSON Payload

TSDProxy sends a `POST` request with `Content-Type: application/json`:

```json
{
  "proxy": "myapp",
  "status": "Running",
  "previous_status": "Starting",
  "timestamp": "2026-05-15T10:00:00Z",
  "message": "Proxy 'myapp' status changed from Starting to Running"
}
```

| Field | Type | Description |
|-------|------|-------------|
| `proxy` | string | Proxy hostname |
| `status` | string | Current status (e.g., `Running`, `Error`) |
| `previous_status` | string | Previous status |
| `timestamp` | string | UTC timestamp in RFC 3339 format |
| `message` | string | Human-readable status change description |

## Custom Payload Template

Set `template` to render a custom request body with Go `text/template`. The template receives the webhook event fields as `.ProxyName`, `.Status`, `.OldStatus`, `.Timestamp`, and `.Message`. When `template` is set and parses successfully, it takes precedence over `type` and is sent as `Content-Type: application/json`.

```yaml {filename="/config/tsdproxy.yaml"}
webhooks:
  - url: "https://hooks.slack.com/services/your/webhook/url"
    type: slack
    template: |
      {"text":"TSDProxy: Proxy `{{.ProxyName}}` changed from `{{.OldStatus}}` to `{{.Status}}`"}
```

## With Custom Headers

```yaml {filename="/config/tsdproxy.yaml"}
webhooks:
  - url: "https://example.com/webhook"
    headers:
      Authorization: "Bearer your-token"
      X-Custom-Header: "custom-value"
    events:
      - Error
```

Custom headers are applied after `Content-Type`, so you can override it if your endpoint expects a different content type.

## Examples

### Home Assistant

Send events to a [REST sensor](https://www.home-assistant.io/integrations/rest/) or automation trigger:

```yaml {filename="/config/tsdproxy.yaml"}
webhooks:
  - url: "https://homeassistant.local:8123/api/webhook/tsdproxy"
    type: generic
    headers:
      Authorization: "Bearer long-lived-access-token"
```

### n8n

Use the [Webhook node](https://docs.n8n.io/integrations/builtin/core-nodes/n8n-nodes-base.webhook/) to receive TSDProxy events:

```yaml {filename="/config/tsdproxy.yaml"}
webhooks:
  - url: "https://n8n.example.com/webhook/tsdproxy"
    type: generic
```

### Custom Script

Point the webhook at any HTTP server:

```yaml {filename="/config/tsdproxy.yaml"}
webhooks:
  - url: "http://localhost:3000/tsdproxy-events"
    type: generic
```

## Notes

- **Default type.** If you omit `type`, TSDProxy sends generic JSON. You only need `type: generic` for clarity.
- **Response handling.** TSDProxy considers the webhook successful when the response status is below 300. Any status code 300 or above triggers a retry (up to 3 attempts with exponential backoff).
- **Timeout.** Each request has a 10-second timeout. If your endpoint is slow, consider putting a queue in front of it.
