---
title: Service with host network_mode
---

If running in `network_mode: host`, use port labels with `no_autodetect`:

{{% steps %}}

### Add port labels

```yaml
labels:
  tsdproxy.enable: "true"
  tsdproxy.port.1: "443/https:8080/http, no_autodetect"
```

For HTTPS:

```yaml
labels:
  tsdproxy.enable: "true"
  tsdproxy.port.1: "443/https:8443/https, no_autodetect, no_tlsvalidate"
```

### Restart

After restart, the proxy is accessible via Tailscale.

{{% /steps %}}
