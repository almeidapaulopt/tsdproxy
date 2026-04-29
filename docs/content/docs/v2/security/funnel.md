---
title: Tailscale Funnel Security
prev: /docs/v2/security
weight: 3
---

Funnel exposes services to the public internet. Use with caution.

## Enabling

**Docker:**
```yaml
labels:
  tsdproxy.port.1: "443/https:80/http, tailscale_funnel"
```

**Lists:**
```yaml
public-service:
  ports:
    443/https:
      targets:
        - http://localhost:8080
      tailscale:
        funnel: true
```

## ACL

```json
"nodeAttrs": [{
  "target": ["autogroup:member", "tag:server"],
  "attr": ["funnel"]
}]
```

> [!CAUTION]
> Funnel bypasses Tailscale authentication. Ensure your backend has its own auth.
