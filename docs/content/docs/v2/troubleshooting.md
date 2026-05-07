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
7. Network issues: use `tsdproxy.autodetect: "false"` label and specify port explicitly

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

Enable in ACL, add `tailscale_funnel` port option. See
[Funnel Security]({{< ref "/docs/v2/security/funnel#troubleshooting" >}}) for details.

### Proxy stuck "Authenticating"

Verify OAuth credentials or AuthKey. Check logs.
See [Authentication Methods]({{< ref "/docs/v2/security/auth-methods" >}}) for setup.

### Enabling debug logging

```yaml
log:
  level: trace
```

### pprof debug profiling

Set the `TSDPROXY_PPROF` environment variable to `"true"` before starting
TSDProxy to enable Go profiling endpoints:

```yaml
services:
  tsdproxy:
    image: almeidapaulopt/tsdproxy:2
    environment:
      TSDPROXY_PPROF: "true"
```

This exposes the following endpoints:

| Endpoint | Purpose |
|----------|---------|
| `/debug/pprof/` | Profile index |
| `/debug/pprof/cmdline` | Command line |
| `/debug/pprof/profile` | CPU profile |
| `/debug/pprof/symbol` | Symbol table |
| `/debug/pprof/trace` | Execution trace |

> [!WARNING]
> pprof endpoints expose internal runtime data. Never enable in production.

{{% /steps %}}
