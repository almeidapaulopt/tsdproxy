---
title: Gotify
weight: 2
prev: /docs/notifications/ntfy
next: /docs/notifications/discord
---

Send TSDProxy status alerts to [Gotify](https://gotify.net/) — a self-hosted notification server.

## Configuration

Gotify accepts JSON payloads via its message API. Use the `generic` type since TSDProxy sends JSON that Gotify can parse:

```yaml {filename="/config/tsdproxy.yaml"}
webhooks:
  - url: "https://gotify.example.com/message?token=your-app-token"
    type: generic
    events:
      - Running
      - Stopped
      - Error
```

## With Authentication

Gotify uses a token in the query string for app-level authentication. You can also pass the token as a header:

```yaml {filename="/config/tsdproxy.yaml"}
webhooks:
  - url: "https://gotify.example.com/message"
    type: generic
    headers:
      X-Gotify-Key: "your-app-token"
    events:
      - Running
      - Stopped
      - Error
```

## Message Format

TSDProxy sends a JSON payload that Gotify stores as-is:

```json
{
  "proxy": "myapp",
  "status": "Running",
  "previous_status": "Starting",
  "timestamp": "2026-05-15T10:00:00Z",
  "message": "Proxy 'myapp' status changed from Starting to Running"
}
```

## Notes

- **Generic type.** Use `type: generic` (or omit `type` entirely) — Gotify accepts JSON message payloads.
- **Token in URL vs header.** Both `?token=` in the URL and `X-Gotify-Key` header work. The header approach keeps tokens out of server logs.
- **Priority.** Gotify supports message priority (1–10) but TSDProxy does not set it. If you need priority, use a Gotify plugin or a reverse proxy that injects the `priority` field into the JSON body.
- **Self-hosted only.** Gotify is self-hosted — there is no cloud service. Make sure your Gotify instance is reachable from the TSDProxy container.
