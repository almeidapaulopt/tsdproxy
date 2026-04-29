---
title: One TSDProxy, two Docker servers, one Tailscale provider
prev: /docs/v2/scenarios/
---

## TSDProxy Config

```yaml {filename="/config/tsdproxy.yaml"}
defaultProxyProvider: default
docker:
  srv1:
    host: unix:///var/run/docker.sock
    defaultProxyProvider: default
  srv2:
    host: tcp://174.17.0.1:2376
    targetHostname: 174.17.0.1
    defaultProxyProvider: default
tailscale:
  providers:
    default:
      authKey: "YOUR_AUTH_KEY_HERE"
```

## Server 1 Services

```yaml
services:
  webserver1:
    image: nginx
    labels:
      tsdproxy.enable: "true"
      tsdproxy.name: "webserver1"
      tsdproxy.port.1: "443/https:80/http"

  portainer:
    image: portainer/portainer-ee
    labels:
      tsdproxy.enable: "true"
      tsdproxy.name: "portainer"
      tsdproxy.port.1: "443/https:9000/http"
```

## Server 2 Services

```yaml
services:
  webserver2:
    image: nginx
    labels:
      tsdproxy.enable: "true"
      tsdproxy.name: "webserver2"
      tsdproxy.port.1: "443/https:80/http"

  memos:
    image: neosmemo/memos:stable
    labels:
      tsdproxy.enable: "true"
      tsdproxy.name: "memos"
      tsdproxy.port.1: "443/https:5230/http"
```
