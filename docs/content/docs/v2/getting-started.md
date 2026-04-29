---
title: Getting Started
weight: 1
prev: /docs
---

## Quick Start

Using Docker Compose, you can easily configure the proxy to your Tailscale
containers. Here’s an example of how you can configure your services using
Docker Compose:

{{% steps %}}

### Create a TSDProxy docker-compose.yaml

```yaml docker-compose.yml
services:
  tsdproxy:
    image: almeidapaulopt/tsdproxy:2
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
      - datadir:/data
      - <PATH_TO_YOUR_CONFIG_DIR>:/config
    restart: unless-stopped
    ports:
      - "8080:8080"
    extra_hosts:
      - "host.docker.internal:host-gateway"
volumes:
  datadir:
```

> [!IMPORTANT]
> The `extra_hosts` entry maps `host.docker.internal` to the Docker host gateway.
> This allows TSDProxy to detect the Docker host IP for routing traffic to containers.

### Start the TSDProxy container

```bash
docker compose up -d
```

### Configure TSDProxy

After the TSDProxy container is started, a configuration file
`/config/tsdproxy.yaml` is created and populated with the following:

```yaml  {filename="/config/tsdproxy.yaml"}
defaultProxyProvider: default
docker:
  local: # name of the docker target provider
    host: unix:///var/run/docker.sock # host of the docker socket or daemon
    targetHostname: host.docker.internal # hostname or IP of docker server (ex: host.docker.internal or 172.31.0.1)
    defaultProxyProvider: default # name of which proxy provider to use
lists: {}
tailscale:
  providers:
    default: # name of the provider
      authKey: "" # optional, define authkey here
      authKeyFile: "" # optional, use this to load authkey from file. If this is defined, Authkey is ignored
      controlUrl: https://controlplane.tailscale.com # use this to override the default control URL
  dataDir: /data/
http:
  hostname: 0.0.0.0
  port: 8080
log:
  level: info # set logging level info, error or trace
  json: false # set to true to enable json logging
proxyAccessLog: true # set to true to enable container access log
```

#### Edit the configuration file

1. Change your docker host if you are not using the socket.
2. Restart the service if you changed the configuration.

```bash
docker compose restart
```

### Run a sample service

Here we’ll use the nginx image to serve a sample service.
The container name is `sample-nginx`, expose port 8111, and add the
`tsdproxy.enable` label.

```bash
docker run -d --name sample-nginx -p 8111:80 --label "tsdproxy.enable=true" nginx:latest
```

### Open Dashboard

1. Visit the dashboard at http://<IP_ADDRESS>:8080.
2. Sample-nginx should appear in the dashboard. Click the button and
authenticate with Tailscale.
3. After authentication, the proxy will be enabled.

> [!IMPORTANT]
> The first time you run the proxy, it will take a few seconds to start, because
> it needs to connect to the Tailscale network, generate the certificates, and start
> the proxy.

{{% /steps %}}
