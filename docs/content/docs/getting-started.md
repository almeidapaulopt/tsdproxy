---
title: Getting Started
weight: 1
prev: /docs
---

## Quick Start

Using Docker Compose, you can easily configure the proxy to your Tailscale
containers. Here's an example of how to configure your services using
Docker Compose.

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
      authKey: ""
      authKeyFile: ""
      controlUrl: https://controlplane.tailscale.com
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
> Edit the config file to add your AuthKey if you want automated authentication.
> See [Authentication Methods]({{< ref "/docs/security/auth-methods" >}}) for details.

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
2. Sample-nginx should appear in the dashboard. Click the button and
   authenticate with Tailscale.
3. After authentication, the proxy will be enabled.

> [!WARNING]
> The example config has `adminAllowLocalhost: true`, which allows unauthenticated
> dashboard access from the Docker bridge network. This is convenient for getting
> started, but should be disabled in production. Remove or set it to `false` and
> access the dashboard through a Tailscale proxy instead.
> See [Admin Allowlist]({{< ref "/docs/security/admin-allowlist" >}}) for details.

{{% /steps %}}
