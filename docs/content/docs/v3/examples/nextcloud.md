---
title: Nextcloud
weight: 3
prev: /docs/examples/home-assistant
next: /docs/examples/grafana-stack
---

Expose a Nextcloud instance on your Tailscale network with automatic HTTPS
using Services mode.

## docker-compose.yml

```yaml {filename="docker-compose.yml"}
services:
  tsdproxy:
    image: almeidapaulopt/tsdproxy:2
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

  nextcloud:
    image: nextcloud:latest
    container_name: nextcloud
    environment:
      - PUID=1000
      - PGID=1000
      - OVERWRITEPROTOCOL=https
    volumes:
      - nextcloud-html:/var/www/html
      - nextcloud-data:/var/www/html/data
    labels:
      tsdproxy.enable: "true"
      tsdproxy.name: "cloud"
      tsdproxy.dash.label: "Nextcloud"
      tsdproxy.dash.icon: "si/nextcloud"
      # HTTPS on 443 -> container port 80 (HTTP)
      tsdproxy.port.1: "443/https:80/http"
    restart: unless-stopped

volumes:
  tsdproxy-data:
  nextcloud-html:
  nextcloud-data:
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
| `tsdproxy.enable` | `"true"` | Enable proxying for this container |
| `tsdproxy.name` | `"cloud"` | Service name. Your instance will be at `cloud.<tailnet-name>.ts.net` |
| `tsdproxy.dash.label` | `"Nextcloud"` | Display name in the TSDProxy dashboard |
| `tsdproxy.dash.icon` | `"si/nextcloud"` | Dashboard icon (from Simple Icons). See [icons]({{< ref "/docs/v3/advanced/icons" >}}) for available options |
| `tsdproxy.port.1` | `"443/https:80/http"` | HTTPS on 443, forwarding to the container's HTTP port 80 |

## Access

After starting the containers, Nextcloud is available at:

```
https://cloud.<tailnet-name>.ts.net
```

## Notes

- **OVERWRITEPROTOCOL.** The `OVERWRITEPROTOCOL=https` environment variable tells Nextcloud to generate URLs using HTTPS. Without it, Nextcloud generates HTTP links internally, which break when accessed through the Tailscale HTTPS proxy. This is the most common issue people hit when running Nextcloud behind a reverse proxy.
- **Trusted domains.** Nextcloud may require you to add the Tailscale hostname to its trusted domains list. Add it through the Nextcloud admin settings, or set it in `config.php` inside the `nextcloud-html` volume:
  ```php
  'trusted_domains' =>
    array (
      0 => 'localhost',
      1 => 'cloud.<tailnet-name>.ts.net',
    ),
  ```
- **Data volume separation.** The compose file separates `nextcloud-html` (application files) from `nextcloud-data` (user files). This makes backups easier and lets you update the image without losing data.
- **Database.** This example uses SQLite (Nextcloud's default). For better performance with multiple users, add a separate database service (MariaDB or PostgreSQL) and configure Nextcloud to use it. The database does not need its own Tailscale proxy since it communicates with Nextcloud over the Docker network.
- **Environment variables.** Create a `.env` file alongside the compose file:
  ```text {filename=".env"}
  TS_CLIENT_ID=your_client_id
  TS_CLIENT_SECRET=your_client_secret
  ```
