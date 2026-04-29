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

The Docker image includes a HEALTHCHECK using the `healthcheck` binary that calls the endpoint.

## External Monitoring

```bash
curl -f http://tsdproxy:8080/health/ready/
```
