---
title: One TSDProxy, three containers, one shared Tailscale connection
---

Three containers share one Tailscale machine using SNI routing, each with its own custom domain.

## TSDProxy Config

```yaml {filename="/config/tsdproxy.yaml"}
defaultProxyProvider: shared

dnsProviders:
  cloudflare:
    provider: cloudflare
    apiToken: "your-cloudflare-api-token"

tlsProviders:
  acme:
    provider: acme
    email: "admin@example.com"

defaultDNSProvider: cloudflare
defaultTLSProvider: acme
cleanupDNS: true
cleanupTLS: true

docker:
  local:
    host: unix:///var/run/docker.sock
    targetHostname: host.docker.internal
    defaultProxyProvider: shared

tailscale:
  providers:
    shared:
      clientId: "your_client_id"
      clientSecret: "your_client_secret"
      tags: "tag:shared-proxy"
      shared: true
      hostname: "shared-proxy"
  dataDir: /data/

http:
  hostname: 0.0.0.0
  port: 8080

log:
  level: info
```

## Docker Compose

```yaml
services:
  webapp:
    image: nginx:alpine
    labels:
      tsdproxy.enable: "true"
      tsdproxy.name: "webapp"
      tsdproxy.domain: "webapp.example.com"
      tsdproxy.port.1: "443/https:80/http"

  portainer:
    image: portainer/portainer-ee
    labels:
      tsdproxy.enable: "true"
      tsdproxy.name: "portainer"
      tsdproxy.domain: "portainer.example.com"
      tsdproxy.port.1: "443/https:9000/http"

  memos:
    image: neosmemo/memos:stable
    labels:
      tsdproxy.enable: "true"
      tsdproxy.name: "memos"
      tsdproxy.domain: "memos.example.com"
      tsdproxy.port.1: "443/https:5230/http"
```

## How it works

All three containers share the `shared` Tailscale provider, which creates a single Tailscale machine named `shared-proxy`. Each container gets its own custom domain (`webapp.example.com`, `portainer.example.com`, `memos.example.com`). DNS CNAME records point each domain to the same Tailscale machine, and TLS connections are routed by SNI to the correct container. Only HTTPS ports are supported in shared mode. The shared Tailscale machine starts when the first container appears and stops when the last one is removed.
