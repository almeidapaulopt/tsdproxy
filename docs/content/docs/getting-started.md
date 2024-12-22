---
title: Getting Started
weight: 1
prev: /docs
---

## Quick Start

Using Docker Compose, you can easily configure the proxy to your Tailscale containers. Here’s an example of how you can configure your services using Docker Compose:

{{% steps %}}

### Create a TSDProxy docker-compose.yaml

```yaml docker-compose.yml
services:
  tsdproxy:
    image: almeidapaulopt/tsdproxy:latest
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
      - datadir:/data
      - <PATH_TO_YOUR_CONFIG_DIR>:/config
    restart: unless-stopped

volumes:
  datadir:
```

### Start the TSDProxy container

```bash
docker compose up -d
```

### Configure TSDProxy

After the TSDProxy container is started, a configuration file
`/config/tsdproxy.yaml` is created and populated with the following:

```yaml
defaultproxyprovider: default
docker:
  local: # name of the docker provider
    host: unix:///var/run/docker.sock # host of the docker socket or daemon
    targethostname: 172.31.0.1 # hostname or IP of docker server
    defaultproxyprovider: default # name of which proxy provider to use
file: {}
tailscale:
  providers:
    default: # name of the provider
      authkey: your-authkey # define authkey here
      authkeyfile: "" # use this to load authkey from file. If this is defined, Authkey is ignored
      controlurl: https://controlplane.tailscale.com # use this to override the default control URL
  datadir: /data/
http:
  hostname: 0.0.0.0
  port: 8080
log:
  level: info # set logging level info, error or trace
  json: false # set to true to enable json logging
proxyaccesslog: true # set to true to enable container access log
```

#### Edit the configuration file

1. Set your authkey in the file `/config/tsdproxy.yaml`.
2. Change your docker host if you are not using the socket.
3. restart the service.

```bash
docker compose restart
```

### Run a sample service

Here we’ll use the nginx image to serve a sample service.
The container name is `sample-nginx`, expose port 8181, and add the
`tsdproxy.enable` label.

```bash
docker run -d --name sample-nginx -p 8111:80 --label "tsdproxy.enable=true" nginx:latest
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
needs to connect to the Tailscale network, generate the certificates, and start
the proxy.
{{< /callout >}}

{{% /steps %}}
