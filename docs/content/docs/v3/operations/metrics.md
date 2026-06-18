---
title: Metrics & Grafana
prev: /docs/v3/operations
weight: 4
---

TSDProxy exposes a [Prometheus](https://prometheus.io/) metrics endpoint with
per-proxy request counters, latency histograms, connection gauges, health
status, and TLS certificate lifetime. A pre-built Grafana dashboard is served
at a well-known path for one-command provisioning.

## Metrics Endpoint

```http
GET /metrics
```

The endpoint is protected by admin middleware. Monitoring tools must
authenticate with an API key.

> [!IMPORTANT]
> Configure an API key in `tsdproxy.yaml` (`apiKey` or `apiKeyFile`) and pass
> it to Prometheus via `bearer_token_file`. See
> [Server Configuration]({{< ref "/docs/v3/serverconfig" >}}) for details.

### Prometheus Scrape Config

```yaml {filename="prometheus.yml"}
global:
  scrape_interval: 15s

scrape_configs:
  - job_name: tsdproxy
    metrics_path: /metrics
    bearer_token_file: /etc/prometheus/tsdproxy-api-key
    static_configs:
      - targets: ["tsdproxy:8080"]
```

Replace `tsdproxy:8080` with your TSDProxy host and configured `http.port`.

### Exposed Metrics

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `tsdproxy_proxies` | Gauge | — | Total active proxies |
| `tsdproxy_proxy_requests_total` | Counter | `proxy`, `port`, `code` | Total proxied requests |
| `tsdproxy_proxy_request_duration_seconds` | Histogram | `proxy`, `port`, `code` | Request latency |
| `tsdproxy_proxy_requests_in_flight` | Gauge | `proxy`, `port` | Current in-flight requests |
| `tsdproxy_proxy_status` | Gauge | `proxy`, `status` | One-hot status enum (one label set per proxy = 1) |
| `tsdproxy_proxy_up` | Gauge | `proxy` | Health: `1` up, `0` down, `-1` unknown |
| `tsdproxy_proxy_connections_active` | Gauge | `proxy`, `port` | Active TCP connections |
| `tsdproxy_udp_clients_active` | Gauge | `proxy`, `port` | Concurrent UDP clients |
| `tsdproxy_cert_expiry_seconds` | Gauge | `proxy` | TLS certificate remaining lifetime (seconds) |

## Pre-built Grafana Dashboard

TSDProxy serves a ready-to-use Grafana dashboard and a Prometheus datasource
template at well-known paths. These endpoints require **no authentication** —
they are static provisioning assets meant to be fetched once by init
containers or setup scripts.

```http
GET /-/grafana/dashboard.json
GET /-/grafana/datasource.yaml
```

{{% steps %}}

### Method 1 — Import via Grafana UI

1. Download the dashboard JSON:

   ```bash
   curl -o tsdproxy-dashboard.json http://tsdproxy:8080/-/grafana/dashboard.json
   ```

2. In Grafana, go to **Dashboards → New → Import**.
3. Upload `tsdproxy-dashboard.json`.
4. Select your Prometheus datasource when prompted.
5. Click **Import**.

### Method 2 — Provision via files (automated)

Copy both files into Grafana's provisioning directories. Grafana picks them up
on startup — no UI interaction needed.

```bash {filename="setup-grafana.sh"}
# Datasource
curl -s http://tsdproxy:8080/-/grafana/datasource.yaml \
  -o /etc/grafana/provisioning/datasources/tsdproxy.yaml

# Dashboard provider config
cat > /etc/grafana/provisioning/dashboards/tsdproxy.yaml <<'EOF'
apiVersion: 1
providers:
  - name: TSDProxy
    type: file
    options:
      path: /var/lib/grafana/dashboards
EOF

# Dashboard
curl -s http://tsdproxy:8080/-/grafana/dashboard.json \
  -o /var/lib/grafana/dashboards/tsdproxy.json
```

Restart Grafana after placing the files.

> [!NOTE]
> The served `datasource.yaml` defaults to
> `http://localhost:9090`. Edit the `url` field if your Prometheus instance
> runs elsewhere.

### Method 3 — Import via Grafana HTTP API

For environments without file access (managed Grafana, Grafana Cloud):

```bash {filename="import-dashboard.sh"}
GRAFANA_URL="http://grafana:3000"
GRAFANA_AUTH="admin:admin"

curl -s http://tsdproxy:8080/-/grafana/dashboard.json \
  | jq '{dashboard: ., overwrite: true}' \
  | curl -s -u "$GRAFANA_AUTH" \
      -H "Content-Type: application/json" \
      -d @- \
      "$GRAFANA_URL/api/dashboards/db"
```

Requires [`jq`](https://stedolan.github.io/jq/) and a Grafana service account
or basic auth credentials.

{{% /steps %}}

## Docker Compose Example

Using an init container to fetch the dashboard automatically:

```yaml {filename="docker-compose.yml"}
services:
  grafana-init:
    image: curlimages/curl
    command: >
      sh -c '
        curl -s http://tsdproxy:8080/-/grafana/dashboard.json -o /dashboards/tsdproxy.json &&
        curl -s http://tsdproxy:8080/-/grafana/datasource.yaml -o /datasources/tsdproxy.yaml
      '
    volumes:
      - grafana-dashboards:/dashboards
      - grafana-datasources:/datasources
    depends_on:
      - tsdproxy

  grafana:
    image: grafana/grafana
    volumes:
      - grafana-dashboards:/var/lib/grafana/dashboards
      - grafana-datasources:/etc/grafana/provisioning/datasources
    environment:
      - GF_DASHBOARDS_DEFAULT_HOME_DASHBOARD_PATH=/var/lib/grafana/dashboards/tsdproxy.json
    depends_on:
      grafana-init:
        condition: service_completed_successfully

volumes:
  grafana-dashboards:
  grafana-datasources:
```

## Related

- [API Reference → Metrics]({{< ref "/docs/v3/operations/api#metrics" >}}) — endpoint details
- [Grafana + Prometheus Stack]({{< ref "/docs/v3/examples/grafana-stack" >}}) — running Grafana itself behind TSDProxy
