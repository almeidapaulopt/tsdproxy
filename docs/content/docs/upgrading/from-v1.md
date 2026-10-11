---
title: Upgrading from v1 to v2
prev: /docs/upgrading
weight: 1
---

This guide helps migrate from TSDProxy v1 to v2.

## Before You Upgrade

1. Back up `/config/` directory
2. Read the [changelog](../../changelog)
3. Plan for downtime — proxies will restart

## Step 1: Update Image

```yaml
services:
  tsdproxy:
    image: almeidapaulopt/tsdproxy:2
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

### Check Docker backend connectivity

Successful Tailscale registration and certificate provisioning do not confirm
that TSDProxy can reach the application backend. If applications time out with
HTTP 502 after upgrading, check the Docker provider settings in `tsdproxy.yaml`.

For reachable container networks, enable target URL probing under `docker.local`:

```yaml {filename="/config/tsdproxy.yaml"}
docker:
  local:
    tryDockerInternalNetwork: true
```

TSDProxy must be able to reach the target container's network and application
port, normally by sharing a Docker network with the container. Enabling probing
does not make isolated networks reachable. A container label
`tsdproxy.autodetect: "false"` overrides this provider setting; remove or update
that label if you want the container to use probing.

In a reported migration, commenting out an unsuitable
`targetHostname: host.docker.internal` and enabling `tryDockerInternalNetwork`
restored access. Omitting `targetHostname` still applies its default
(`172.31.0.1`); it does not clear the effective hostname or enable automatic
discovery on its own. If probing fails, the fallback hostname and port must
still be reachable.

If you use host-based access instead, set `targetHostname` to a Docker host
address that resolves and is reachable from TSDProxy, and publish the
application's port on that host so TSDProxy can reach it. Step 1's
`host.docker.internal:host-gateway` mapping supplies the hostname mapping; it
does not publish application ports or guarantee connectivity through firewalls
or port bindings.

See [Docker server configuration]({{< ref "/docs/serverconfig#trydockerinternalnetwork" >}})
for provider settings and per-container overrides.

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

Check Dashboard at `http://<IP>:8080`, then open an application's proxied URL
and confirm it returns the expected response. A healthy Tailscale node and a
valid certificate alone are not enough to verify the upgrade.

If HTTP 502 persists, inspect the TSDProxy logs (for example,
`docker compose logs tsdproxy`) for target URL auto-detection attempts and the
selected backend address and port. Check that this backend is reachable from
TSDProxy's container or network, using the connectivity guidance in Step 2.

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

See [Server Configuration]({{< ref "/docs/serverconfig#hostname" >}}) for
details.

### Dashboard authentication

All dashboard and API endpoints now require authentication. If you access the
dashboard via Docker port mapping (not through a Tailscale proxy), enable
localhost access:

```yaml
adminAllowLocalhost: true
```

Then configure an `admins` list or `apiKey` and disable localhost access. See
[Admin Allowlist]({{< ref "/docs/security/admin-allowlist" >}}) for full
instructions.

See the [changelog]({{< ref "/docs/changelog" >}}) for the complete list of
changes.

## Rolling Back

```bash
docker compose down
# Restore config backup
# Change image tag back to almeidapaulopt/tsdproxy:1
docker compose up -d
```
