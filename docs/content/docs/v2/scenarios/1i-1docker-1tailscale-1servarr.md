---
title: Servarr stack with network_mode service:vpn
prev: /docs/v2/scenarios/
---

Prowlarr using `network_mode: "service:vpn"` — TSDProxy connects via the VPN container's network alias.

## Config

```yaml {filename="/config/tsdproxy.yaml"}
lists:
  media:
    filename: /config/media.yaml
    defaultProxyProvider: default
```

```yaml {filename="/config/media.yaml"}
prowlarr:
  ports:
    443/https:
      targets:
        - http://prowlarr:9696
```

## Compose

```yaml
services:
  vpn:
    image: qmcgaw/gluetun
    networks:
      tailscale:
        aliases:
          - prowlarr

  prowlarr:
    image: lscr.io/linuxserver/prowlarr:latest
    network_mode: "service:vpn"
```
