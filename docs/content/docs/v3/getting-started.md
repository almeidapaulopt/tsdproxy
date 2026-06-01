---
title: Getting Started
weight: 1
prev: /docs
---

## Quick Start

Using Docker Compose, you can easily configure the proxy to your Tailscale
containers. Here's an example of how to configure your services using
Docker Compose with the recommended **Services mode**.

{{% steps %}}

### Create a TSDProxy docker-compose.yaml

```yaml {filename="docker-compose.yml"}
services:
  tsdproxy:
    image: almeidapaulopt/tsdproxy:2
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
      - datadir:/data
      - ./config:/config
    restart: unless-stopped
    ports:
      - "8080:8080"
    extra_hosts:
      - "host.docker.internal:host-gateway"
    environment:
      TSDPROXY_TAILSCALE_DEFAULT_CLIENTID: "${TS_CLIENT_ID}"
      TSDPROXY_TAILSCALE_DEFAULT_CLIENTSECRET: "${TS_CLIENT_SECRET}"

volumes:
  datadir:
```

Create a `.env` file in the same directory with your Tailscale OAuth credentials:

```text {filename=".env"}
TS_CLIENT_ID=your_client_id
TS_CLIENT_SECRET=your_client_secret
```

> [!IMPORTANT]
> The `extra_hosts` entry maps `host.docker.internal` to the Docker host gateway.
> This allows TSDProxy to detect the Docker host IP for routing traffic to containers.

> [!TIP]
> Generate OAuth credentials at
> [https://login.tailscale.com/admin/settings/oauth](https://login.tailscale.com/admin/settings/oauth).
> Assign tags to the OAuth client (e.g. `tag:tsdproxy`) — Tailscale requires all
> OAuth-generated keys to have tags.

### Start the TSDProxy container

```bash
docker compose up -d
```

### Configure TSDProxy

After the TSDProxy container is started, a configuration file
`/config/tsdproxy.yaml` is created and populated with the following:

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
      preventDuplicates: "auto"
  dataDir: /data/

http:
  hostname: 0.0.0.0
  port: 8080

log:
  level: info
  json: false
  proxyAccessLog: true
```

> [!IMPORTANT]
> Edit the config file to add your OAuth credentials and tags. Then restart:
>
> ```bash
> docker compose restart
> ```

### Run a sample service

Here we'll use the nginx image to serve a sample service.
The container name is `sample-nginx`, expose port 8111, and add the
`tsdproxy.enable` label.

```bash
docker run -d --name sample-nginx -p 8111:80 --label "tsdproxy.enable=true" nginx:latest
```

### Open Dashboard

1. Visit the dashboard at `http://<IP_ADDRESS>:8080`.
2. Sample-nginx should appear in the dashboard and start automatically
   (no manual authentication needed with OAuth).
3. After the proxy is running, the service is available at
   `https://sample-nginx.<tailnet-name>.ts.net`.

> [!IMPORTANT]
> All dashboard endpoints require authentication. When accessing via Docker port
> mapping (not through a Tailscale proxy), enable
> `adminAllowLocalhost: true` in your config. In Docker, this trusts requests
> from the Docker bridge network automatically.
> See [Admin Allowlist]({{< ref "/docs/v3/security/admin-allowlist" >}}) for details.

> [!IMPORTANT]
> The first time you run the proxy, it will take a few seconds to start, because
> it needs to connect to the Tailscale network, create the VIP Service, and start
> the proxy.

{{% /steps %}}

## Next Steps

- Browse the [Examples]({{< ref "/docs/v3/examples" >}}) for ready-to-use Docker Compose
  configurations for Jellyfin, Home Assistant, Nextcloud, and Grafana
- Learn about all [Docker Labels]({{< ref "/docs/v3/providers/docker-reference" >}})
  for advanced port configuration
- See [Services Mode]({{< ref "/docs/v3/advanced/tailscale#services-mode" >}}) for
  full configuration details and constraints
- Read about [other exposure modes]({{< ref "/docs/v3/concepts#exposure-modes" >}})
  if you need custom domains or UDP support
