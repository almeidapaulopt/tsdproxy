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

### Create Tailscale OAuth credentials

1. Go to the [Tailscale OAuth clients settings](https://login.tailscale.com/admin/settings/oauth).
2. Click **+ Credential**.
3. Select **OAuth** and give it a description (e.g. "TSDProxy").
4. Under **Scopes**, enable the required permissions:
   - **General/Services**: `write` (to create and manage Tailscale services/VIP)
   - **Devices/Core**: `write` (to create and manage Tailscale machines)
   - **Keys/Auth Keys**: `write` (to generate single use keys for services)
   - **Policy/ACL**: `read` (to verify ACL tags are configured)
   - **Policy/ACL**: `write` (optional — to auto-provision ACL tags when `autoProvisionAcl: true`)
5. Assign tags to the OAuth client (e.g. `tag:example`) — Tailscale requires all OAuth-generated keys to have tags.
6. Click **Generate client** and copy the **Client ID** and **Client Secret** — you'll need them in the config file.

> [!WARNING]
> Store the Client Secret securely. It is only shown once.

### Create a TSDProxy docker-compose.yaml

```yaml {filename="docker-compose.yml"}
services:
  tsdproxy:
    image: almeidapaulopt/tsdproxy:dev
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
      - datadir:/data
      - ./config:/config
    restart: unless-stopped
    ports:
      - "8080:8080"
    extra_hosts:
      - "host.docker.internal:host-gateway"
    labels:
      tsdproxy.enable: "true"
      tsdproxy.name: "dash"
      tsdproxy.dash.visible: "false"

volumes:
  datadir:
```

> [!IMPORTANT]
> The `extra_hosts` entry maps `host.docker.internal` to the Docker host gateway.
> This allows TSDProxy to detect the Docker host IP for routing traffic to containers.

### Configure TSDProxy

Create the configuration file `./config/tsdproxy.yaml` with the following:

```yaml {filename="./config/tsdproxy.yaml"}
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

dashboard:
  adminAllowLocalhost: true

log:
  level: info
  json: false
  proxyAccessLog: true
```

> [!IMPORTANT]
> Add your OAuth credentials (from Step 1) to the config file before starting.

### Start the TSDProxy container

```bash
docker compose up -d
```

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

> [!WARNING]
> The example config has `adminAllowLocalhost: true`, which allows unauthenticated
> dashboard access from the Docker bridge network. This is convenient for getting
> started, but should be disabled in production. Remove or set it to `false` and
> access the dashboard through a Tailscale proxy instead.
> See [Admin Allowlist]({{< ref "/docs/v3/security/admin-allowlist" >}}) for details.

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
