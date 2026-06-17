---
title: Upgrading from v1 to v2
prev: /docs/upgrading
weight: 1
---

This guide helps migrate from TSDProxy v1 to v2.

> [!TIP]
> If you are on v2 and want to upgrade to v3 with the recommended Services mode,
> see [Upgrading from v2 to v3]({{< ref "/docs/v3/upgrading/from-v2" >}}).

## Before You Upgrade

1. Back up `/config/` directory
2. Read the [changelog](../../changelog)
3. Plan for downtime — proxies will restart

## Step 1: Update Image

```yaml
services:
  tsdproxy:
    image: almeidapaulopt/tsdproxy:dev
```

Add `extra_hosts` if missing:
```yaml
    extra_hosts:
      - "host.docker.internal:host-gateway"
```

## Step 2: Migrate Config

**Rename `files:` to `lists:`** in `tsdproxy.yaml`.

**Update list files** from v1 format to v2 multi-port format:

```yaml
# v1
nas1:
  url: https://192.168.1.2:5001

# v2
nas1:
  ports:
    443/https:
      targets:
        - https://192.168.1.2:5001
```

## Step 3: Migrate Docker Labels

| V1 | V2 |
|----|-----|
| `tsdproxy.container_port: 8080` | `tsdproxy.port.1: "443/https:8080/http"` |
| `tsdproxy.scheme: https` | `tsdproxy.port.1: "443/https:443/https"` |
| `tsdproxy.tlsvalidate: false` | `tsdproxy.port.1: "443/https:80/http, no_tlsvalidate"` |
| `tsdproxy.funnel: true` | `tsdproxy.port.1: "443/https:80/http, tailscale_funnel"` |

## Step 4: Restart & Verify

```bash
docker compose pull && docker compose up -d
```

Check Dashboard at `http://<IP>:8080`.

## Post-Upgrade: v2.2.0 Breaking Changes

If you are upgrading to v2.2.0 or later, review these breaking changes:

### Dashboard bind address

The default `http.hostname` changed from `0.0.0.0` to `127.0.0.1`. If you
expose the dashboard externally (Docker port mapping, reverse proxy), add to
your config:

```yaml
http:
  hostname: 0.0.0.0
```

See [Server Configuration]({{< ref "/docs/v3/serverconfig#hostname" >}}) for
details.

### Dashboard authentication

All dashboard and API endpoints now require authentication. If you access the
dashboard via Docker port mapping (not through a Tailscale proxy), enable
localhost access:

```yaml
adminAllowLocalhost: true
```

Then configure an `admins` list or `apiKey` and disable localhost access. See
[Admin Allowlist]({{< ref "/docs/v3/security/admin-allowlist" >}}) for full
instructions.

See the [changelog]({{< ref "/docs/v3/changelog" >}}) for the complete list of
changes.

## Rolling Back

```bash
docker compose down
# Restore config backup
# Change image tag back to almeidapaulopt/tsdproxy:1
docker compose up -d
```
