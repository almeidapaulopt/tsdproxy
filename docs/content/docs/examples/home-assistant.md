---
title: Home Assistant
weight: 2
prev: /docs/examples/jellyfin
next: /docs/examples/nextcloud
---

Expose Home Assistant with both its web interface and an additional TCP port for add-ons that need direct access.

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

  homeassistant:
    image: ghcr.io/home-assistant/home-assistant:stable
    container_name: homeassistant
    volumes:
      - ha-config:/config
      - /etc/localtime:/etc/localtime:ro
    labels:
      tsdproxy.enable: "true"
      tsdproxy.name: "homeassistant"
      # HTTPS on 443 -> container port 8123 (HTTP)
      tsdproxy.port.1: "443/https:8123/http"
      # HTTP on 80 -> redirect to HTTPS
      tsdproxy.port.2: "80/http->https://homeassistant.<tailnet-name>.ts.net"
    restart: unless-stopped

volumes:
  tsdproxy-data:
  ha-config:
```

## Labels Explained

| Label | Value | Purpose |
|-------|-------|---------|
| `tsdproxy.enable` | `"true"` | Enable proxying for this container |
| `tsdproxy.name` | `"homeassistant"` | Tailscale hostname. Reaches `homeassistant.<tailnet>.ts.net` |
| `tsdproxy.port.1` | `"443/https:8123/http"` | HTTPS on 443, forwarding to Home Assistant's web UI on port 8123 |
| `tsdproxy.port.2` | `"80/http->https://homeassistant.<tailnet-name>.ts.net"` | Redirect HTTP requests to the HTTPS URL |

> [!NOTE]
> Replace `<tailnet-name>` in the redirect URL with your actual Tailscale tailnet name. You can find it in the [Tailscale admin console](https://login.tailscale.com/admin/settings/general).

## Access

After authenticating through the dashboard, Home Assistant is available at:

```
https://homeassistant.<tailnet-name>.ts.net
```

## Adding TCP Ports for Add-ons

Some Home Assistant add-ons communicate over raw TCP. For example, if you run a Zigbee coordinator add-on that listens on port 5555, add a third port entry:

```yaml
labels:
  tsdproxy.enable: "true"
  tsdproxy.name: "homeassistant"
  tsdproxy.port.1: "443/https:8123/http"
  tsdproxy.port.2: "80/http->https://homeassistant.<tailnet-name>.ts.net"
  # TCP proxy for add-on communication
  tsdproxy.port.3: "5555/tcp:5555/tcp"
```

See [TCP Proxy & SSH]({{< ref "/docs/advanced/tcp-proxy" >}}) for more details on TCP proxying.

## Notes

- **Host networking.** Home Assistant sometimes recommends `network_mode: host` for features like mDNS discovery. If you use host networking, TSDProxy cannot auto-detect the target port. Use `no_autodetect` and specify the port explicitly:
  ```yaml
  labels:
    tsdproxy.enable: "true"
    tsdproxy.name: "homeassistant"
    tsdproxy.autodetect: "false"
    tsdproxy.port.1: "443/https:8123/http, no_autodetect"
  ```
  See [Host Mode]({{< ref "/docs/advanced/host-mode" >}}) for more.
- **Config volume.** The `ha-config` volume stores all Home Assistant configuration, including integrations, automations, and secrets. Back it up regularly.
- **Multiple ports.** This example shows three ports (HTTPS, HTTP redirect, and an optional TCP port) on a single Tailscale machine. This is the multi-port capability that TSDProxy provides out of the box.
