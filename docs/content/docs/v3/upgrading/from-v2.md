---
title: Upgrading from v2 to v3
prev: /docs/upgrading
weight: 2
---

This guide helps migrate from TSDProxy v2 to v3, which introduces the
recommended **Services mode** — fewer Tailscale machines, auto-assigned FQDNs,
and no external DNS or TLS provider required.

## What Changed in v3

### Recommended: Services mode

v3 introduces **Services mode** using the Tailscale VIP Services API. All
proxies share a single Tailscale machine, and each gets an auto-assigned FQDN
from Tailscale (e.g. `myapp.tailnet-name.ts.net`).

Benefits over per-proxy mode:

- **Fewer Tailscale machines** — one shared machine instead of one per container
- **No external DNS provider** — FQDNs are auto-assigned by Tailscale
- **No external TLS provider** — certificates handled automatically
- **Simpler config** — no `dnsProviders`, `tlsProviders`, or custom domains needed

### New config options

| Option | Description |
|--------|-------------|
| `services` | Enable VIP Services mode on a Tailscale provider |
| `autoApproveDevices` | Auto-approve device registration (recommended with Services mode) |
| `authRetry` | Configurable retry policy for tsnet startup failures |
| `reconcileInterval` | Periodic device reconciliation to clean up stale devices |
| `clientSecretFile` | Load OAuth client secret from a file |
| `preventDuplicates` | Now a boolean — warns if enabled without OAuth credentials |

### New proxy statuses

Three new statuses for better observability:

- **`AuthFailed`** — authentication permanently failed
- **`DeviceConflict`** — hostname collision with an existing device
- **`Reconciling`** — device reconciler is cleaning up stale devices

### Constraints

Services mode does not support:

- Custom domains — FQDNs are auto-assigned by Tailscale
- UDP — only HTTPS, HTTP, and TCP ports
- HTTP redirects — use per-proxy mode if you need redirects

## Before You Upgrade

1. **Back up** your `/config/` and `/data/` directories
2. **Generate OAuth credentials** at [https://login.tailscale.com/admin/settings/oauth](https://login.tailscale.com/admin/settings/oauth) — Services mode requires OAuth
3. **Set tags** on the OAuth client — all services using Services mode need tags
4. Read the [changelog]({{< ref "/docs/v3/changelog" >}})

## Migration Steps

### Step 1: Update the config file

Replace your existing Tailscale provider config with Services mode:

```yaml {filename="/config/tsdproxy.yaml" hl_lines="7-12"}
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
  dataDir: /data/

http:
  hostname: 0.0.0.0
  port: 8080

log:
  level: info
  proxyAccessLog: true
```

> [!TIP]
> You can omit `clientId` and `clientSecret` from the config file and set them
> via environment variables instead:
> ```yaml
> environment:
>   TSDPROXY_TAILSCALE_DEFAULT_CLIENTID: "${TS_CLIENT_ID}"
>   TSDPROXY_TAILSCALE_DEFAULT_CLIENTSECRET: "${TS_CLIENT_SECRET}"
> ```
> See [Environment Variables]({{< ref "/docs/v3/advanced/environment-variables" >}}) for details.

### Step 2: Remove DNS and TLS providers (if no longer needed)

If you were using external DNS and TLS providers solely for Tailscale proxies,
you can remove them:

```yaml
# Remove these if all proxies use Services mode:
# dnsProviders:
#   cloudflare:
#     ...
# tlsProviders:
#   acme:
#     ...
# defaultDNSProvider: cloudflare
# defaultTLSProvider: acme
```

If you still need custom domains for some proxies, keep the DNS/TLS providers
and create a second Tailscale provider for those proxies.

### Step 3: Remove custom domain labels

Services mode auto-assigns FQDNs. Remove `tsdproxy.domain`, `tsdproxy.dnsprovider`,
and `tsdproxy.tlsprovider` labels from containers using Services mode:

```yaml
# Before (v2 with custom domain)
labels:
  tsdproxy.enable: "true"
  tsdproxy.name: "myapp"
  tsdproxy.domain: "app.example.com"
  tsdproxy.dnsprovider: "cloudflare"
  tsdproxy.tlsprovider: "acme"

# After (v3 Services mode)
labels:
  tsdproxy.enable: "true"
  tsdproxy.name: "myapp"
```

### Step 4: Restart

```bash
docker compose pull && docker compose up -d
```

### Step 5: Verify

1. Open the dashboard at `http://<IP>:8080`
2. Check that proxies show the **Running** status
3. Verify each proxy has an auto-assigned FQDN (e.g. `myapp.tailnet-name.ts.net`)
4. Test access via the Tailscale network

## Keeping Per-Proxy Mode

If you prefer to keep the v2 per-proxy mode (one Tailscale machine per
container), it still works — simply omit `services: true` from the provider
config. All v2 features continue to work:

- Per-proxy Tailscale machines with MagicDNS
- Custom domains with external DNS and TLS providers
- Shared Tailscale with SNI routing
- HTTP redirects and UDP ports

You can mix both modes by creating separate Tailscale providers:

```yaml {filename="/config/tsdproxy.yaml"}
tailscale:
  providers:
    # Services mode for most proxies
    default:
      clientId: "your_client_id"
      clientSecret: "your_client_secret"
      tags: "tag:tsdproxy"
      services: true
      hostname: "tsdproxy"
      autoApproveDevices: true

    # Per-proxy mode for proxies that need custom domains or UDP
    perproxy:
      clientId: "your_client_id"
      clientSecret: "your_client_secret"
      tags: "tag:tsdproxy"
```

Then use `tsdproxy.proxyprovider: "perproxy"` on containers that need per-proxy
mode.

## Rolling Back

```bash
docker compose down
# Restore config backup from before the migration
# Change image tag back to almeidapaulopt/tsdproxy:2
docker compose up -d
```

> [!CAUTION]
> Rolling back will not remove the VIP Services created in Tailscale. Remove
> them manually from the [Tailscale admin console](https://login.tailscale.com/admin/machines)
> if needed.
