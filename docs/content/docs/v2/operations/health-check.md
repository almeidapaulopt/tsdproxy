---
title: Health Check
prev: /docs/v2/operations
weight: 1
---

TSDProxy exposes a health endpoint for monitoring.

## Endpoint

```
GET /health/ready/
```

**Healthy (200):** `{"status":"OK"}`
**Not ready (503):** `{"status":"NOK"}`

## Docker HEALTHCHECK

The Docker image includes a HEALTHCHECK using the `healthcheck` binary that
calls the endpoint. The healthcheck binary reads `TSDPROXY_HTTP_PORT` to
determine which port to query. This environment variable is set automatically
by the main server binary from the `http.port` config value, so no manual
configuration is needed.

## External Monitoring

```bash
curl -f http://tsdproxy:8080/health/ready/
```

Replace `8080` with your configured `http.port` if different.
