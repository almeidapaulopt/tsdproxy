---
title: Custom Domains
prev: /docs/advanced/docker-secrets
next: /docs/advanced/environment-variables
weight: 7
---

TSDProxy can serve your containers on custom domains instead of the default
`<name>.<tailnet>.ts.net`. A pluggable DNS + TLS system manages CNAME records
and certificates automatically.

## Overview

Without custom domains, every proxy is reachable at
`https://<name>.<tailnet>.ts.net`. With custom domains enabled, a proxy can be
reached at any domain you own (e.g. `app.example.com`).

TSDProxy handles the full lifecycle:

1. Creates a DNS CNAME pointing your domain to the Tailscale machine
2. Provisions a TLS certificate for the domain
3. Serves HTTPS traffic on the custom domain
4. Cleans up DNS records when the proxy stops (configurable)

The dashboard shows DNS and TLS status badges (`pending`, `active`, `error`) for
proxies with custom domains, so you can monitor the setup progress in real time.

### How it works

```
Container with tsdproxy.domain label
        |
        v
  DNS Provider (Cloudflare / MagicDNS)
    - Creates CNAME: app.example.com -> myapp.tailnet.ts.net
    - Validates DNS propagation
        |
        v
  TLS Provider (Tailscale / ACME)
    - Tailscale: CertPair for .ts.net domains
    - ACME: certmagic + DNS-01 via configured DNS provider
        |
        v
  Proxy serves on https://app.example.com
```

## Quick Start

{{% steps %}}

### Configure a DNS provider

Add a Cloudflare DNS provider in your `tsdproxy.yaml`:

```yaml {filename="/config/tsdproxy.yaml"}
dnsProviders:
  cloudflare:
    provider: cloudflare
    apiToken: "your-cloudflare-api-token"

defaultDNSProvider: cloudflare
```

> [!TIP]
> Store the API token in a file for better security:
> ```yaml
> dnsProviders:
>   cloudflare:
>     provider: cloudflare
>     apiTokenFile: "/run/secrets/cloudflare-token"
> ```

### Configure a TLS provider

Add an ACME TLS provider for automatic certificate provisioning:

```yaml {filename="/config/tsdproxy.yaml"}
tlsProviders:
  acme:
    provider: acme
    email: "admin@example.com"

defaultTLSProvider: acme
```

### Add the domain label to your container

```yaml
services:
  myapp:
    image: nginx:alpine
    labels:
      tsdproxy.enable: "true"
      tsdproxy.name: "myapp"
      tsdproxy.domain: "app.example.com"
```

TSDProxy creates the CNAME, provisions the certificate, and starts proxying on
`https://app.example.com`.

{{% /steps %}}

## DNS Providers

DNS providers manage the CNAME record that points your custom domain to the
Tailscale machine. TSDProxy supports the following providers:

### Cloudflare

Manages CNAME records and ACME TXT records via the Cloudflare API. Requires an
API token with `Zone:DNS:Edit` and `Zone:Zone:Read` permissions.

```yaml {filename="/config/tsdproxy.yaml"}
dnsProviders:
  cloudflare:
    provider: cloudflare
    apiToken: "your-cloudflare-api-token"
```

| Field | Required | Description |
|-------|----------|-------------|
| `provider` | yes | Must be `cloudflare` |
| `apiToken` | yes | Cloudflare API token |
| `apiTokenFile` | no | Path to a file containing the API token (overrides `apiToken`) |

> [!TIP]
> Create the API token in the Cloudflare dashboard under
> **My Profile > API Tokens**. Use the "Edit zone DNS" template.

### MagicDNS

The default DNS provider. A no-op provider used when the domain is a
`.ts.net` Tailscale domain. No CNAME management is needed because Tailscale
handles DNS resolution internally.

MagicDNS is used automatically and does not need explicit configuration. When
`tsdproxy.domain` ends in `.ts.net`, TSDProxy selects MagicDNS regardless of
the `defaultDNSProvider` setting.

## TLS Providers

TLS providers provision and manage certificates for your custom domains.

### Tailscale

Wraps Tailscale's built-in `CertPair` for `.ts.net` domains. Used automatically
when the domain ends in `.ts.net`. No configuration required — the Tailscale TLS
provider is resolved per-proxy using the Tailscale local client, regardless of
the config entry name.

```yaml {filename="/config/tsdproxy.yaml"}
tlsProviders:
  my-ts-tls:
    provider: tailscale

defaultTLSProvider: my-ts-tls
```

> [!NOTE]
> The Tailscale TLS provider only works with `.ts.net` domains. For custom
> domains, use the ACME provider. The config entry name can be anything —
> TSDProxy detects the provider type from the `provider: tailscale` field.

### ACME

Uses [certmagic](https://github.com/caddyserver/certmagic) with DNS-01
challenge to provision certificates from Let's Encrypt (or another ACME
compatible CA). The DNS-01 challenge uses the configured DNS provider
(e.g. Cloudflare) to create TXT records.

```yaml {filename="/config/tsdproxy.yaml"}
tlsProviders:
  acme:
    provider: acme
    email: "admin@example.com"
```

| Field | Required | Default | Description |
|-------|----------|---------|-------------|
| `provider` | yes | - | Must be `acme` |
| `email` | yes | - | Email for ACME account registration |
| `ca` | no | Let's Encrypt Production | ACME directory URL |
| `certStorage` | no | data directory | Path to store certificates |

> [!CAUTION]
> The ACME provider uses the default DNS provider (or per-proxy DNS provider)
> to create the DNS-01 challenge TXT records. Make sure a DNS provider capable
> of TXT records (e.g. Cloudflare) is configured.

## Complete Example

A full `tsdproxy.yaml` with custom domain support:

```yaml {filename="/config/tsdproxy.yaml"}
defaultProxyProvider: default

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
  dataDir: /data/

http:
  hostname: 0.0.0.0
  port: 8080

log:
  level: info
```

A container using the custom domain:

```yaml
services:
  webapp:
    image: nginx:alpine
    labels:
      tsdproxy.enable: "true"
      tsdproxy.name: "webapp"
      tsdproxy.domain: "app.example.com"
      tsdproxy.dash.icon: "si/nginx"
      tsdproxy.port.1: "443/https:80/http"
```

A list proxy using the custom domain:

```yaml {filename="/config/critical.yaml"}
homepage:
  domain: "home.example.com"
  dnsProvider: cloudflare
  tlsProvider: acme
  ports:
    443/https:
      targets:
        - http://192.168.1.10:3000
```

## Cleanup

When a proxy stops, TSDProxy can automatically remove the DNS CNAME record it
created. This is controlled by the `cleanupDNS` setting:

```yaml {filename="/config/tsdproxy.yaml"}
cleanupDNS: true
```

Defaults to `true`. When enabled, the CNAME record is deleted when the proxy
shuts down. Set to `false` to keep DNS records after the proxy stops. TLS
certificates are cached by certmagic and reused on next start.

## Troubleshooting

{{% details title="Domain without DNS or TLS provider configured" %}}

If you set `tsdproxy.domain` but do not configure a DNS and TLS provider, the
proxy will start without the custom domain. TSDProxy checks both per-proxy
settings and global defaults — you need at least one of:

- Per-proxy: `tsdproxy.dnsprovider` / `tsdproxy.tlsprovider` labels (or
  `dnsProvider` / `tlsProvider` in list files)
- Global: `defaultDNSProvider` / `defaultTLSProvider` in `tsdproxy.yaml`

Check the logs for errors like:

```
domain "app.example.com" set but DNS provider not specified
```

Ensure `dnsProviders` and `tlsProviders` are defined in `tsdproxy.yaml`, and
that `defaultDNSProvider` and `defaultTLSProvider` point to valid provider
names.

{{% /details %}}

{{% details title="Cloudflare API token permissions" %}}

The Cloudflare API token needs at minimum:
- **Zone - DNS - Edit** (to create/delete CNAME and TXT records)
- **Zone - Zone - Read** (to list zones)

If certificate provisioning fails with a DNS-01 challenge error, verify the
token has the correct permissions.

{{% /details %}}

{{% details title="DNS propagation delay" %}}

After creating a CNAME record, TSDProxy validates DNS propagation before
provisioning the TLS certificate. If propagation is slow, the proxy may take
longer to become ready. This is normal and resolves automatically.

{{% /details %}}

{{% details title="Custom domain setup takes time on startup" %}}

TSDProxy waits for the Tailscale proxy to fully initialize before setting up
DNS. This ensures the CNAME target is correct. If the proxy takes longer than
60 seconds to get a URL, the domain setup will time out and the proxy will run
without the custom domain. Check the Tailscale auth status and network
connectivity.

{{% /details %}}

{{% details title="Cloudflare zone not found" %}}

TSDProxy automatically detects the Cloudflare zone by searching from the full
domain down to the root. This handles multi-part TLDs like `.co.uk` or
`.com.br`. If you see "no cloudflare zone found", verify:

1. The domain's DNS is managed by the same Cloudflare account as the API token
2. The API token has `Zone:Zone:Read` permission

{{% /details %}}
