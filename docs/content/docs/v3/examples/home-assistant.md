---
title: Home Assistant
weight: 2
prev: /docs/examples/jellyfin
next: /docs/examples/nextcloud
---

Expose Home Assistant with its web interface and an optional TCP port for
add-ons that need direct access. Uses Services mode for automatic FQDN
assignment.

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
    restart: unless-stopped

volumes:
  tsdproxy-data:
  ha-config:
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
| `tsdproxy.name` | `"homeassistant"` | Service name. Reaches `homeassistant.<tailnet-name>.ts.net` |
| `tsdproxy.port.1` | `"443/https:8123/http"` | HTTPS on 443, forwarding to Home Assistant's web UI on port 8123 |

## Access

After starting the containers, Home Assistant is available at:

```
https://homeassistant.<tailnet-name>.ts.net
```

## Adding TCP Ports for Add-ons

Some Home Assistant add-ons communicate over raw TCP. For example, if you run
a Zigbee coordinator add-on that listens on port 5555, add a second port entry:

```yaml
labels:
  tsdproxy.enable: "true"
  tsdproxy.name: "homeassistant"
  tsdproxy.port.1: "443/https:8123/http"
  # TCP proxy for add-on communication
  tsdproxy.port.2: "5555/tcp:5555/tcp"
```

See [TCP Proxy & SSH]({{< ref "/docs/v3/advanced/tcp-proxy" >}}) for more details on TCP proxying.

## Notes

- **Host networking.** Home Assistant sometimes recommends `network_mode: host` for features like mDNS discovery. If you use host networking, TSDProxy cannot auto-detect the target port. Use `no_autodetect` and specify the port explicitly:
  ```yaml
  labels:
    tsdproxy.enable: "true"
    tsdproxy.name: "homeassistant"
    tsdproxy.autodetect: "false"
    tsdproxy.port.1: "443/https:8123/http, no_autodetect"
  ```
  See [Host Mode]({{< ref "/docs/v3/advanced/host-mode" >}}) for more.
- **HTTP redirects.** Services mode does not support HTTP redirect ports. If you need HTTP→HTTPS redirects, use per-proxy mode for this container by setting `tsdproxy.proxyprovider` to a non-services provider.
- **Config volume.** The `ha-config` volume stores all Home Assistant configuration, including integrations, automations, and secrets. Back it up regularly.
- **Environment variables.** Create a `.env` file alongside the compose file:
  ```text {filename=".env"}
  TS_CLIENT_ID=your_client_id
  TS_CLIENT_SECRET=your_client_secret
  ```
