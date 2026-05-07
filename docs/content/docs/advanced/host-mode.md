---
title: Service with host network_mode
prev: /docs/advanced/docker-secrets
---

When a container runs with `network_mode: host`, it shares the host's network
stack directly — no Docker bridge, no port mapping. This means TSDProxy cannot
auto-detect the target URL through Docker's published ports, because there are
none.

Use the `no_autodetect` port option (or the `tsdproxy.autodetect: "false"` label)
to tell TSDProxy to connect directly to the specified port instead of probing.

{{% steps %}}

### Add port labels

```yaml
labels:
  tsdproxy.enable: "true"
  tsdproxy.port.1: "443/https:8080/http, no_autodetect"
```

For HTTPS targets with self-signed certificates:

```yaml
labels:
  tsdproxy.enable: "true"
  tsdproxy.port.1: "443/https:8443/https, no_autodetect, no_tlsvalidate"
```

Alternatively, disable auto-detection at the container level:

```yaml
labels:
  tsdproxy.enable: "true"
  tsdproxy.autodetect: "false"
  tsdproxy.port.1: "443/https:8080/http"
```

### Restart

After restart, the proxy is accessible via Tailscale.

{{% /steps %}}

## When to use this

- Services that need full host network access (e.g., media servers, VPN clients)
- Services that bind directly to host ports
- When Docker port mapping is not used

## Limitations

- `no_autodetect` disables TSDProxy's connectivity probing for that port.
  The target port must be correct and reachable from the TSDProxy container.
- If TSDProxy itself runs in host network mode, use `127.0.0.1` as the target
  or configure via a [Lists provider]({{< ref "/docs/providers/lists" >}})
  with `http://127.0.0.1:<port>` as the target.
