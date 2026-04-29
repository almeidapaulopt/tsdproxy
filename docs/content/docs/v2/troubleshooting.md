---
title: Troubleshooting (v2)
prev: /docs/v2/advanced
weight: 400
---

## Docker provider

1. Verify `tsdproxy.enable=true`
2. Check port labels: [Port config](../providers/docker/#port-configuration)
3. For HTTPS targets: `tsdproxy.port.1: "443/https:443/https"`
4. Self-signed certs: add `no_tlsvalidate` option
5. Check firewall
6. Same Docker network as TSDProxy
7. Network issues: use `no_autodetect` + specify port

## Lists provider

1. Config is case-sensitive: [Verify files](../providers/lists/#proxy-list-file-options)
2. Check file path in `lists:` config

## Common Errors

{{% steps %}}

### TLS certificate errors (self-signed)

**Docker:** `tsdproxy.port.1: "443/https:443/https, no_tlsvalidate"`
**Lists:** Set `tlsValidate: false` on the port

### Network timeout

Firewall fix: `sudo ufw allow in from 172.17.0.0/16`

### Funnel doesn't work

Enable in ACL, add `tailscale_funnel` port option.

### Proxy stuck "Authenticating"

Verify OAuth credentials or AuthKey. Check logs.

### Enabling debug logging

```yaml
log:
  level: trace
```

### pprof debug

```bash
TSDPROXY_PPROF=true tsdproxy
```

Exposes `/debug/pprof/` — not for production.

{{% /steps %}}
