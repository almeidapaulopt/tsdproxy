---
title: Jellyfin
weight: 1
prev: /docs/why-tsdproxy
next: /docs/examples/home-assistant
---

Expose a Jellyfin media server on your Tailscale network with automatic HTTPS
using Services mode.

## docker-compose.yml

```yaml {filename="docker-compose.yml"}
services:
  tsdproxy:
    image: almeidapaulopt/tsdproxy:dev
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
      - tsdproxy-data:/data
      - ./config:/config
    ports:
      - "8080:8080"
    extra_hosts:
      - "host.docker.internal:host-gateway"
    environment:
      TSDPROXY_TAILSCALE_DEFAULT_CLIENTID: "${TS_CLIENT_ID}"
      TSDPROXY_TAILSCALE_DEFAULT_CLIENTSECRET: "${TS_CLIENT_SECRET}"
    restart: unless-stopped

  jellyfin:
    image: jellyfin/jellyfin:latest
    container_name: jellyfin
    environment:
      - PUID=1000
      - PGID=1000
    volumes:
      - jellyfin-config:/config
      - jellyfin-cache:/cache
      - /path/to/media:/media:ro
    labels:
      tsdproxy.enable: "true"
      tsdproxy.name: "jellyfin"
      # HTTPS on 443 -> container port 8096 (HTTP)
      tsdproxy.port.1: "443/https:8096/http"
    restart: unless-stopped

volumes:
  tsdproxy-data:
  jellyfin-config:
  jellyfin-cache:
```

## tsdproxy.yaml

```yaml {filename="/config/tsdproxy.yaml"}
defaultProxyProvider: default

docker:
  local:
    host: unix:///var/run/docker.sock
    targetHostname: host.docker.internal
    defaultProxyProvider: default

tailscale:
  providers:
    default:
      clientId: "your_client_id"
      clientSecret: "your_client_secret"
      tags: "tag:tsdproxy"
      services: true
      hostname: "tsdproxy"
      autoApproveDevices: true
      preventDuplicates: true
      autoProvisionAcl: true
  dataDir: /data/

http:
  hostname: 0.0.0.0
  port: 8080

log:
  level: info
  proxyAccessLog: true
```

## Labels Explained

| Label | Value | Purpose |
|-------|-------|---------|
| `tsdproxy.enable` | `"true"` | Tell TSDProxy to proxy this container |
| `tsdproxy.name` | `"jellyfin"` | Service name. Your server will be at `jellyfin.<tailnet-name>.ts.net` |
| `tsdproxy.port.1` | `"443/https:8096/http"` | Expose HTTPS on port 443, forwarding to the container's HTTP port 8096 |

## Access

After starting the containers, Jellyfin is available at:

```
https://jellyfin.<tailnet-name>.ts.net
```

No manual authentication needed — Services mode with OAuth handles it automatically.

## Notes

- **Media volumes.** Replace `/path/to/media` with the path to your media library on the host. The `:ro` flag mounts it read-only, which is fine for playback. If you want Jellyfin to manage library files (download subtitles, write metadata), remove `:ro`.
- **Port 8096.** Jellyfin listens on port 8096 by default. TSDProxy terminates TLS at the Tailscale level, so the connection from TSDProxy to Jellyfin is plain HTTP.
- **Hardware transcoding.** If you want VA-API or NVENC transcoding, pass the appropriate device to the Jellyfin container (e.g. `devices: ["/dev/dri:/dev/dri"]`). This has no interaction with TSDProxy.
- **No Funnel needed.** Jellyfin is typically private to your tailnet. If you want to share it publicly, switch to per-proxy mode for this container and add `tailscale_funnel` to the port config. Services mode does not support Funnel.
- **Environment variables.** Create a `.env` file alongside the compose file:
  ```text {filename=".env"}
  TS_CLIENT_ID=your_client_id
  TS_CLIENT_SECRET=your_client_secret
  ```
