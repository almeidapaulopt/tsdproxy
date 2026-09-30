---
title: Slack
weight: 4
prev: /docs/notifications/discord
next: /docs/notifications/generic
---

Send TSDProxy status alerts to a [Slack](https://slack.com) channel using an incoming webhook.

## Setup

1. Create a [Slack app](https://api.slack.com/apps) with incoming webhooks enabled
2. Or use the legacy [Incoming Webhooks](https://slack.com/apps/A0F7XDUAZ) integration
3. Copy the webhook URL — it looks like `https://hooks.slack.com/services/T00/B00/xxx`

## Configuration

```yaml {filename="/config/tsdproxy.yaml"}
webhooks:
  - url: "https://hooks.slack.com/services/YOUR/WEBHOOK/URL"
    type: slack
    events:
      - Running
      - Stopped
      - Error
```

## Message Format

TSDProxy sends a Block Kit formatted message:

- **Fallback text:** `TSDProxy: Proxy 'myapp' status changed to Running`
- **Block content:**
  ```
  *TSDProxy Status Update*
  Proxy: `myapp`
  Status: `Running`
  Previous: `Starting`
  ```

## Filtering to Critical Events

```yaml {filename="/config/tsdproxy.yaml"}
webhooks:
  - url: "https://hooks.slack.com/services/YOUR/WEBHOOK/URL"
    type: slack
    events:
      - Error
```

## Notes

- **@mention safety.** If a proxy name contains `@`, TSDProxy inserts a zero-width space to prevent accidental mentions.
- **Multiple channels.** Create separate Slack webhooks for each channel and add them as separate entries in the `webhooks` list.
