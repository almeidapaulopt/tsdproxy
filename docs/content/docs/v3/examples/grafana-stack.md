---
title: Grafana + Prometheus Stack
weight: 4
prev: /docs/examples/nextcloud
---

A monitoring stack with Grafana and Prometheus using Services mode. Both
services share a single Tailscale machine with auto-assigned FQDNs.

## docker-compose.yml

```yaml {filename="docker-compose.yml"}
services:
  tsdproxy:
    image: almeidapaulopt/tsdproxy:dev
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

  grafana:
    image: grafana/grafana:latest
    container_name: grafana
    environment:
      - GF_SECURITY_ADMIN_USER=admin
      - GF_SECURITY_ADMIN_PASSWORD=changeme
      - GF_USERS_ALLOW_SIGN_UP=false
    volumes:
      - grafana-storage:/var/lib/grafana
    labels:
      tsdproxy.enable: "true"
      tsdproxy.name: "grafana"
      tsdproxy.dash.label: "Grafana"
      tsdproxy.dash.icon: "si/grafana"
      # HTTPS on 443 -> container port 3000 (HTTP)
      tsdproxy.port.1: "443/https:3000/http"
    restart: unless-stopped

  prometheus:
    image: prom/prometheus:latest
    container_name: prometheus
    volumes:
      - ./prometheus.yml:/etc/prometheus/prometheus.yml:ro
      - prometheus-data:/prometheus
    labels:
      tsdproxy.enable: "true"
      tsdproxy.name: "prometheus"
      tsdproxy.dash.label: "Prometheus"
      tsdproxy.dash.icon: "si/prometheus"
      # HTTPS on 443 -> container port 9090 (HTTP)
      tsdproxy.port.1: "443/https:9090/http"
    restart: unless-stopped

volumes:
  tsdproxy-data:
  grafana-storage:
  prometheus-data:
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
      preventDuplicates: true
      autoProvisionAcl: true
  dataDir: /data/

http:
  hostname: 0.0.0.0
  port: 8080

log:
  level: info
  proxyAccessLog: true
```

## prometheus.yml

Create this file alongside your `docker-compose.yml`:

```yaml {filename="prometheus.yml"}
global:
  scrape_interval: 15s

scrape_configs:
  - job_name: "prometheus"
    static_configs:
      - targets: ["localhost:9090"]

  - job_name: "grafana"
    static_configs:
      - targets: ["grafana:3000"]

  # Add your own targets here
  # - job_name: "node-exporter"
  #   static_configs:
  #     - targets: ["node-exporter:9100"]
```

## Labels Explained

Each service has its own set of labels. With Services mode, both share one
Tailscale machine but get separate auto-assigned FQDNs.

**Grafana:**

| Label | Value | Purpose |
|-------|-------|---------|
| `tsdproxy.enable` | `"true"` | Enable proxying |
| `tsdproxy.name` | `"grafana"` | Service name: `grafana.<tailnet-name>.ts.net` |
| `tsdproxy.port.1` | `"443/https:3000/http"` | HTTPS on 443, forwarding to Grafana's HTTP port 3000 |

**Prometheus:**

| Label | Value | Purpose |
|-------|-------|---------|
| `tsdproxy.enable` | `"true"` | Enable proxying |
| `tsdproxy.name` | `"prometheus"` | Service name: `prometheus.<tailnet-name>.ts.net` |
| `tsdproxy.port.1` | `"443/https:9090/http"` | HTTPS on 443, forwarding to Prometheus's HTTP port 9090 |

## Access

After starting the containers:

- **Grafana:** `https://grafana.<tailnet-name>.ts.net`
- **Prometheus:** `https://prometheus.<tailnet-name>.ts.net`

## Notes

- **One machine, multiple FQDNs.** Services mode uses a single Tailscale machine for all proxies. Each service gets its own auto-assigned FQDN. This is more efficient than per-proxy mode where each service would create a separate Tailscale machine.
- **Internal communication.** Prometheus scrapes Grafana's metrics over the Docker network (`grafana:3000`), not through the Tailscale proxy. This keeps internal traffic fast and avoids unnecessary hops.
- **Prometheus config.** The `prometheus.yml` file is mounted read-only into the container. Edit it on the host and restart Prometheus to pick up changes.
- **Admin password.** The `GF_SECURITY_ADMIN_PASSWORD` variable sets the initial Grafana admin password. Change it from `changeme` before deploying.
- **Adding exporters.** To monitor the Docker host or other machines, add node-exporter or other scrape targets to `prometheus.yml`. The exporters do not need Tailscale proxies since Prometheus reaches them over the Docker network.
- **Data persistence.** Both `grafana-storage` and `prometheus-data` use named volumes so dashboards, configurations, and metrics survive container restarts.
- **Environment variables.** Create a `.env` file alongside the compose file:
  ```text {filename=".env"}
  TS_CLIENT_ID=your_client_id
  TS_CLIENT_SECRET=your_client_secret
  ```
