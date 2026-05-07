---
title: Nextcloud
weight: 3
prev: /docs/examples/home-assistant
next: /docs/examples/grafana-stack
---

Expose a Nextcloud instance on your Tailscale network with automatic HTTPS.

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

## Labels Explained

| Label | Value | Purpose |
|-------|-------|---------|
| `tsdproxy.enable` | `"true"` | Enable proxying for this container |
| `tsdproxy.name` | `"cloud"` | Tailscale hostname. Your instance will be at `cloud.<tailnet>.ts.net` |
| `tsdproxy.dash.label` | `"Nextcloud"` | Display name in the TSDProxy dashboard |
| `tsdproxy.dash.icon` | `"si/nextcloud"` | Dashboard icon (from Simple Icons). See [icons]({{< ref "/docs/advanced/icons" >}}) for available options |
| `tsdproxy.port.1` | `"443/https:80/http"` | HTTPS on 443, forwarding to the container's HTTP port 80 |

## Access

After authenticating through the dashboard, Nextcloud is available at:

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
- **HTTPS redirect.** If you want HTTP requests to redirect to HTTPS, add a second port:
  ```yaml
  tsdproxy.port.2: "80/http->https://cloud.<tailnet-name>.ts.net"
  ```
