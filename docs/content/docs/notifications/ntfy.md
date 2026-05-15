---
title: ntfy
weight: 1
prev: /docs/notifications
next: /docs/notifications/gotify
---

Send TSDProxy status alerts to [ntfy](https://ntfy.sh) — a push notification service for your phone and desktop.

## Basic Configuration

```yaml {filename="/config/tsdproxy.yaml"}
webhooks:
  - url: "https://ntfy.sh/your-topic"
    type: ntfy
    events:
      - Running
      - Stopped
      - Error
```

Replace `your-topic` with your ntfy topic name. TSDProxy sends the notification body as plain text:

```
Proxy: myapp
Status: Running
Previous: Starting
```

## With Authentication

If your ntfy topic is protected, add an access token or basic auth via headers:

```yaml {filename="/config/tsdproxy.yaml"}
webhooks:
  - url: "https://ntfy.sh/your-topic"
    type: ntfy
    headers:
      Authorization: "Bearer tk_your_access_token"
```

For basic auth:

```yaml {filename="/config/tsdproxy.yaml"}
webhooks:
  - url: "https://ntfy.sh/your-topic"
    type: ntfy
    headers:
      Authorization: "Basic dXNlcjpwYXNz"
```

## Custom Title and Priority

ntfy uses HTTP headers to set message metadata. Use the `headers` field to customize the notification appearance:

```yaml {filename="/config/tsdproxy.yaml"}
webhooks:
  - url: "https://ntfy.sh/your-topic"
    type: ntfy
    headers:
      Title: "TSDProxy Alert"
      Priority: "high"
      Tags: "rotating_light"
```

Supported ntfy headers:

| Header | Example | Purpose |
|--------|---------|---------|
| `Title` | `TSDProxy Alert` | Notification title |
| `Priority` | `high` | Urgency: `min`, `low`, `default`, `high`, `max` |
| `Tags` | `rotating_light,computer` | Emoji tags (comma-separated) |
| `Click` | `https://myapp.tailnet.ts.net` | URL opened when notification is tapped |
| `Actions` | `view, Open Dashboard, https://host:8080` | Action buttons |
| `Icon` | `https://example.com/icon.png` | Notification icon URL |

## Self-Hosted ntfy Server

Point the URL to your own instance:

```yaml {filename="/config/tsdproxy.yaml"}
webhooks:
  - url: "https://ntfy.example.com/your-topic"
    type: ntfy
    headers:
      Authorization: "Bearer tk_your_access_token"
      Priority: "default"
    events:
      - Running
      - Stopped
      - Error
```

## Filtering by Status

Only send notifications for the events you care about:

```yaml {filename="/config/tsdproxy.yaml"}
webhooks:
  - url: "https://ntfy.sh/tsdproxy-errors"
    type: ntfy
    headers:
      Priority: "high"
      Tags: "x"
    events:
      - Error
```

## Notes

- **Plain text format.** ntfy receives the message body as plain text (`Content-Type: text/plain`), not JSON.
- **Custom headers are passed through.** Any header you set in the `headers` map is included in the HTTP request to ntfy. You can use any ntfy-supported header.
- **Multiple topics.** To send to different topics with different priorities, add multiple webhook entries:
  ```yaml {filename="/config/tsdproxy.yaml"}
  webhooks:
    - url: "https://ntfy.sh/tsdproxy-all"
      type: ntfy
    - url: "https://ntfy.sh/tsdproxy-critical"
      type: ntfy
      headers:
        Priority: "max"
      events:
        - Error
  ```
