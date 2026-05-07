---
title: Grafana + Prometheus Stack
weight: 4
prev: /docs/v2/examples/nextcloud
---

A monitoring stack with Grafana and Prometheus. Each service gets its own Tailscale hostname, and the Prometheus metrics port stays private within the Docker network.

## docker-compose.yml

```yaml {filename="docker-compose.yml"}
services:
  tsdproxy:
    image: almeidapaulopt/tsdproxy:2
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
      - tsdproxy-data:/data
      - ./config:/config
    ports:
      - "8080:8080"
    extra_hosts:
      - "host.docker.internal:host-gateway"
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

Each service has its own set of labels. This means each one gets its own Tailscale machine and hostname.

**Grafana:**

| Label | Value | Purpose |
|-------|-------|---------|
| `tsdproxy.enable` | `"true"` | Enable proxying |
| `tsdproxy.name` | `"grafana"` | Tailscale hostname: `grafana.<tailnet>.ts.net` |
| `tsdproxy.port.1` | `"443/https:3000/http"` | HTTPS on 443, forwarding to Grafana's HTTP port 3000 |

**Prometheus:**

| Label | Value | Purpose |
|-------|-------|---------|
| `tsdproxy.enable` | `"true"` | Enable proxying |
| `tsdproxy.name` | `"prometheus"` | Tailscale hostname: `prometheus.<tailnet>.ts.net` |
| `tsdproxy.port.1` | `"443/https:9090/http"` | HTTPS on 443, forwarding to Prometheus's HTTP port 9090 |

## Access

After authenticating both proxies through the dashboard:

- **Grafana:** `https://grafana.<tailnet-name>.ts.net`
- **Prometheus:** `https://prometheus.<tailnet-name>.ts.net`

## Notes

- **Separate hostnames.** Each service is a distinct Tailscale machine. This means you can share Grafana with a teammate without giving them access to Prometheus, or vice versa, using Tailscale ACLs.
- **Internal communication.** Prometheus scrapes Grafana's metrics over the Docker network (`grafana:3000`), not through the Tailscale proxy. This keeps internal traffic fast and avoids unnecessary hops.
- **Prometheus config.** The `prometheus.yml` file is mounted read-only into the container. Edit it on the host and restart Prometheus to pick up changes.
- **Admin password.** The `GF_SECURITY_ADMIN_PASSWORD` variable sets the initial Grafana admin password. Change it from `changeme` before deploying.
- **Adding exporters.** To monitor the Docker host or other machines, add node-exporter or other scrape targets to `prometheus.yml`. The exporters do not need Tailscale proxies since Prometheus reaches them over the Docker network.
- **Data persistence.** Both `grafana-storage` and `prometheus-data` use named volumes so dashboards, configurations, and metrics survive container restarts.
