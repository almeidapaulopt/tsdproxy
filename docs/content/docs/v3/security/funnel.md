---
title: Tailscale Funnel Security
prev: /docs/security
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

## ACL Requirements

Funnel must be enabled in your Tailscale ACL before use. Add the following to
your policy:

```json
"nodeAttrs": [{
  "target": ["autogroup:member", "tag:server"],
  "attr": ["funnel"]
}]
```

See Tailscale's [Funnel documentation](https://tailscale.com/kb/1223/funnel#requirements-and-limitations)
for full requirements and limitations.

## Limitations

- **Only HTTPS (port 443) is supported** — Funnel does not expose raw TCP ports
- **TLS is handled by Tailscale** — the public URL uses Tailscale's certificate
- **Public URL format**: `https://<hostname>.tailnet-name.ts.net`
- **No Tailscale authentication** — Funnel bypasses tailnet membership checks

> [!CAUTION]
> Funnel bypasses Tailscale authentication. Anyone on the internet can reach
> your service. Ensure your backend has its own authentication.

## Troubleshooting

### Funnel doesn't work

1. Verify Funnel is enabled in your Tailscale ACL (see above)
2. Check that the port option `tailscale_funnel` is set
3. Only HTTPS ports can use Funnel — TCP ports are not supported
4. Check TSDProxy logs for Funnel-related errors

### "Funnel not available" error

Ensure your Tailscale account has Funnel enabled. The `nodeAttrs` ACL entry
must include the tags used by your proxy.
