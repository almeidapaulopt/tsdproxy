---
title: Discord
weight: 3
prev: /docs/notifications/gotify
next: /docs/notifications/slack
---

Send TSDProxy status alerts to a [Discord](https://discord.com) channel using a webhook integration.

## Setup

1. Open your Discord server settings → **Integrations** → **Webhooks**
2. Create a new webhook (or select an existing one)
3. Copy the webhook URL — it looks like `https://discord.com/api/webhooks/123456789/abcdef...`

## Configuration

```yaml {filename="/config/tsdproxy.yaml"}
webhooks:
  - url: "https://discord.com/api/webhooks/123456789/abcdef"
    type: discord
    events:
      - Running
      - Stopped
      - Error
```

## Message Format

TSDProxy sends a rich Discord embed with color-coded status:

- **Green** (`#57F287`) — proxy is `Running`
- **Red** (`ED4245`) — proxy is `Error` or `Stopped`
- **Grey-blue** (`#5865F2`) — all other statuses

The embed includes the proxy name, current status, previous status, and timestamp.

## Filtering to Errors Only

```yaml {filename="/config/tsdproxy.yaml"}
webhooks:
  - url: "https://discord.com/api/webhooks/123456789/abcdef"
    type: discord
    events:
      - Error
      - Stopped
```

## Notes

- **@mention safety.** If a proxy name contains `@`, TSDProxy inserts a zero-width space to prevent accidental pings in Discord.
- **Thread support.** To send to a specific thread, append `?thread_id=<id>` to the webhook URL.
- **Multiple channels.** Create separate Discord webhooks for each channel and add them as separate entries in the `webhooks` list.
