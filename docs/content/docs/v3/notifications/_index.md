---
title: Notifications
prev: /docs/providers
next: /docs/notifications/ntfy
weight: 4
---

Send alerts when proxies change status — start, stop, or encounter errors.

TSDProxy delivers webhook notifications to popular services. Each recipe below shows the exact configuration for a specific platform.

{{< cards >}}
  {{< card link="ntfy" title="ntfy" icon="bell"
    subtitle="Push notifications to your phone or desktop"
  >}}
  {{< card link="gotify" title="Gotify" icon="speakerphone"
    subtitle="Self-hosted notification server"
  >}}
  {{< card link="discord" title="Discord" icon="chat"
    subtitle="Rich embeds with color-coded status"
  >}}
  {{< card link="slack" title="Slack" icon="chat-alt"
    subtitle="Block Kit formatted messages"
  >}}
  {{< card link="generic" title="Generic Webhook" icon="globe"
    subtitle="JSON payload for any HTTP endpoint"
  >}}
{{< /cards >}}

## How It Works

TSDProxy emits an event every time a proxy changes status. You choose which statuses trigger a notification using the `events` filter.

### Available Events

| Event | Meaning |
|-------|---------|
| `Initializing` | Proxy is being created |
| `Starting` | Tailscale node is starting up |
| `Authenticating` | Waiting for Tailscale authentication |
| `Running` | Proxy is up and serving traffic |
| `Stopping` | Proxy is shutting down |
| `Stopped` | Proxy has been stopped |
| `Error` | Proxy encountered an error |
| `Paused` | Proxy is paused |

Event names are case-insensitive. If `events` is omitted, all status changes are sent.

### Configuration

All notifications are configured in the `webhooks` section of `/config/tsdproxy.yaml`:

```yaml {filename="/config/tsdproxy.yaml"}
webhooks:
  - url: "https://example.com/webhook"
    type: generic              # ntfy | gotify | discord | slack | generic
    events:                    # optional — send all events if omitted
      - Running
      - Stopped
      - Error
    headers:                   # optional custom HTTP headers
      Authorization: "Bearer token123"
```

### Multiple Webhooks

You can configure multiple webhooks to notify different services or topics simultaneously:

```yaml {filename="/config/tsdproxy.yaml"}
webhooks:
  - url: "https://ntfy.sh/alerts"
    type: ntfy
    events: [Running, Error, Stopped]

  - url: "https://discord.com/api/webhooks/123/abc"
    type: discord
    events: [Error]
```

### Testing

Send a test webhook from the API:

```bash
curl -X POST http://localhost:8080/api/webhook/test
```

This fires a test event with proxy name `test-proxy`, status `Running`, and previous status `Stopped` to all configured webhooks.
