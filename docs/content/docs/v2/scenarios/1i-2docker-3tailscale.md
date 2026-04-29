---
title: One TSDProxy, two Docker servers, three Tailscale providers
---

Containers in SRV1 use 'default', SRV2 use 'account2'. webserver1 and memos override to 'withtags'.

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
    defaultProxyProvider: account2
tailscale:
  providers:
    default:
      authKey: "KEY1"
    withtags:
      authKey: "KEY2"
    account2:
      authKey: "KEY3"
```

## Server 1

```yaml
services:
  webserver1:
    image: nginx
    labels:
      tsdproxy.enable: "true"
      tsdproxy.name: "webserver1"
      tsdproxy.proxyprovider: "withtags"
      tsdproxy.port.1: "443/https:80/http"

  portainer:
    image: portainer/portainer-ee
    labels:
      tsdproxy.enable: "true"
      tsdproxy.name: "portainer"
      tsdproxy.port.1: "443/https:9000/http"
```

## Server 2

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
      tsdproxy.proxyprovider: "withtags"
      tsdproxy.port.1: "443/https:5230/http"
```
