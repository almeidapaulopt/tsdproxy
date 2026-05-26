---
title: Troubleshooting (v2)
prev: /docs/security
next: /docs/faq
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
[Funnel Security]({{< ref "/docs/security/funnel#troubleshooting" >}}) for details.

### Proxy stuck "Authenticating"

Verify OAuth credentials or AuthKey. Check logs.
See [Authentication Methods]({{< ref "/docs/security/auth-methods" >}}) for setup.

### Dashboard unreachable after upgrading to v2.2.0

v2.2.0 changed the default `http.hostname` from `0.0.0.0` to `127.0.0.1` for
security (see [GHSA-j8rq-87gr-gm9q](https://github.com/almeidapaulopt/tsdproxy/security/advisories/GHSA-j8rq-87gr-gm9q)).
If you expose the dashboard via Docker port mapping (`ports: "8080:8080"`), the
server only listens on localhost **inside** the container — unreachable from the host.

**Fix:** set `hostname` explicitly in your `tsdproxy.yaml`:

```yaml
http:
  hostname: 0.0.0.0
  port: 8080
```

### "Access requires a Tailscale connection" on dashboard

v2.2.0 requires authentication on all dashboard endpoints. If you access the
dashboard through Docker port mapping (not via a Tailscale proxy), there is no
Tailscale identity to authenticate with.

**Fix:** enable localhost access in your `tsdproxy.yaml`:

```yaml
adminAllowLocalhost: true
```

> ⚠️ Anyone who can reach port 8080 on your host will have admin access.
> If the port is exposed to a network, consider restricting it or using an
> [API key]({{< ref "/docs/serverconfig#api-key-authentication" >}}) instead.

See [Admin Allowlist]({{< ref "/docs/security/admin-allowlist" >}}) for details.

### Enabling debug logging

```yaml
log:
  level: trace
```

{{% /steps %}}
