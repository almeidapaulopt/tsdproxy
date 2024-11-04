---
title: Getting Started
weight: 1
prev: /docs
next: /docs/services
---

## Quick Start

Using Docker Compose, you can easily configure the proxy to your Tailscale containers. Here’s an example of how you can configure your services using Docker Compose:

{{% steps %}}

### Create a TSDProxy docker-compose.yaml

```yaml docker-compose.yml
services:
  tailscale-docker-proxy:
    image: almeidapaulopt/tsdproxy:latest
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
      - datadir:/data
    restart: unless-stopped
    environment:
      # Get AuthKey from your Tailscale account
      - TSDPROXY_AUTHKEY=tskey-auth-SecretKey 
      # Address of docker server (access to example.com ports)
      - TSDPROXY_HOSTNAME=192.168.1.1 
      - DOCKER_HOST=unix:///var/run/docker.sock 

volumes:
  datadir:
```

### Start the TSDProxy container

```bash
docker compose up -d
```

### Run a sample service

Here we’ll use the nginx image to serve a sample service.
The container name is `sample-nginx`, expose port 8181, and add the `tsdproxy.enable` label.

```bash
docker run -d --name sample-nginx -p 8181:80 \
--label "tsdproxy.enable=true" nginx:latest
```

### Test the sample service

```bash
curl https://sample-nginx.FUNNY-NAME.ts.net
```

{{< callout type="info" >}}
Note that you need to replace `FUNNY-NAME` with the name of your network.
{{< /callout >}}

{{< callout type="warning" >}}
The first time you run the proxy, it will take a few seconds to start, because it
needs to connect to the Tailscale network, generate the certificates, and start the proxy.
{{< /callout >}}

{{% /steps %}}
