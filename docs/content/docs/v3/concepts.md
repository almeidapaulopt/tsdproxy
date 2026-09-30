---
title: How TSDProxy Works
weight: 0
prev: /docs
next: /docs/getting-started
---

## Core Concepts

TSDProxy is built around four ideas. Understanding them makes everything else click.

### Target Provider

A target provider is the source of things you want to proxy. It watches for
changes and reports them to the rest of the system. Two target providers exist
today:

- **Docker** connects to the Docker daemon (via socket or TCP) and monitors
  containers. When a container with `tsdproxy.enable=true` appears, the target
  provider picks it up.
- **List** reads a YAML file describing static proxy targets. Useful for
  services that don't run in Docker, or for quick testing.

Each target provider can have its own defaults, like which proxy provider to
use or how to reach the Docker host.

### Proxy Provider

A proxy provider creates the actual network endpoint. Right now the only
implementation is **Tailscale**, which spins up a `tsnet.Server` for each proxy
and obtains TLS certificates automatically.

The provider pattern is designed to be pluggable. If a different tunnel or VPN
backend is added later, it would be a new proxy provider.

### Proxy

A proxy is a single running instance: one Tailscale machine forwarding traffic
to one target (a container or a list entry). Each proxy has its own hostname,
TLS certificate, and port mappings.

### ProxyManager

The ProxyManager is the orchestrator. It subscribes to events from all target
providers and decides what to do: create a proxy when a new target appears,
remove one when a target goes away, or restart a proxy when its configuration
changes.

## Data Flow

```
Docker Container ──labels──► TargetProvider (Docker)
                                     │
                                     ▼  TargetEvent
                               ProxyManager ◄── config
                                     │
                                     ▼  creates Proxy
                               ProxyProvider (Tailscale)
                                     │
                                     ▼  spins up
                               tsnet.Server (Tailscale node)
                                     │
                                     ▼  reverse proxy
                               HTTP/TCP → Container Port
```

Target providers watch for containers (or list entries) and emit events. The
ProxyManager receives those events, resolves the configuration, picks a proxy
provider, and creates a Proxy. The Proxy starts a Tailscale `tsnet.Server`,
gets a certificate, and begins reverse-proxying traffic.

## What Happens When You Add a Label

{{% steps %}}

### Add the label

You label a container with `tsdproxy.enable=true` (plus any optional labels
like `tsdproxy.name` or `tsdproxy.proxyprovider`).

### TargetProvider detects the container

The Docker target provider listens to the Docker event stream. When it sees a
container with the right label, it reads all `tsdproxy.*` labels and builds a
proxy configuration.

### ProxyManager receives a TargetEvent

The target provider emits a `TargetEvent` to the ProxyManager, carrying the
full proxy config (hostname, ports, Tailscale settings).

### ProxyManager creates a Proxy

The ProxyManager resolves which proxy provider to use (see
[Configuration Resolution](#configuration-resolution) below), then calls the
provider to create a new Proxy instance.

### Proxy starts a Tailscale node

The Proxy spins up a `tsnet.Server`, which registers a new machine on your
Tailscale network. This is an actual Tailscale node, not a virtual host.

### Authentication

If you configured an AuthKey or OAuth credentials in the
[Tailscale provider settings]({{< ref "/docs/v3/advanced/tailscale" >}}),
authentication happens automatically. Otherwise, the dashboard shows an
"Authenticating" status and gives you a link to log in through your browser.

### TLS certificate is obtained

After authentication, TSDProxy obtains a TLS certificate. By default this is
via Tailscale's built-in certificate provisioning for MagicDNS names
(`*.ts.net`). Your service is now reachable at
`https://your-container.tailnet-name.ts.net`.

If a custom domain is configured, TSDProxy uses an external ACME provider
(like Let's Encrypt) with DNS-01 challenge instead. See
[Exposure Modes](#exposure-modes) below for details.

### Traffic starts flowing

The reverse proxy begins forwarding requests from the Tailscale node to the
container's internal port. Your service is live on your tailnet.

{{% /steps %}}

## Exposure Modes

TSDProxy supports four ways to expose your containers. Each mode differs in
how DNS, TLS certificates, and Tailscale connections are managed.

| Mode | Tailscale connections | DNS | TLS certificates | Use case |
|------|-----------------------|-----|------------------|----------|
| **Tailscale-only** | One per proxy | MagicDNS (automatic) | Tailscale (automatic) | Default. Simplest setup. |
| **Custom domains** | One per proxy | External provider | ACME / Let's Encrypt | Custom domain names per service. |
| **Shared Tailscale** | One shared server | External provider | ACME / Let's Encrypt | Fewer Tailscale machines, custom domains. |
| **Services/VIP** | One shared server | Tailscale-assigned (automatic) | Tailscale (automatic) | Fewer machines, auto-assigned FQDNs. |

### Mode 1: Tailscale-only

This is the default. No extra configuration needed.

Each container gets its own Tailscale machine with an automatic MagicDNS
hostname and TLS certificate. Services are reachable at
`https://<name>.<tailnet>.ts.net`.

```yaml
labels:
  tsdproxy.enable: "true"
```

### Mode 2: Custom domains with per-proxy Tailscale

Each container still gets its own Tailscale machine, but you use your own domain
names instead of `.ts.net`.

You need a DNS provider (e.g. Cloudflare) and an ACME TLS provider in your
config. TSDProxy automatically creates CNAME records pointing to the Tailscale
machine and provisions Let's Encrypt certificates via DNS-01 challenge.

```yaml {filename="/config/tsdproxy.yaml"}
dnsProviders:
  cloudflare:
    provider: cloudflare
    apiToken: "your-token"
tlsProviders:
  acme:
    provider: acme
    email: "admin@example.com"
defaultDNSProvider: cloudflare
defaultTLSProvider: acme
```

```yaml
labels:
  tsdproxy.enable: "true"
  tsdproxy.domain: "app.example.com"
```

See [Custom Domains]({{< ref "/docs/v3/advanced/custom-domains" >}}) for the full
setup guide.

### Mode 3: Shared Tailscale with custom domains

Multiple containers share a single Tailscale machine. Connections are routed by
TLS SNI (Server Name Indication) to the right container based on the domain
name. Only HTTPS ports are supported in this mode.

This mode requires `shared: true` and a `hostname:` on the Tailscale provider
config, plus a custom domain on each proxy. You end up with fewer Tailscale
machines in your tailnet, since all domains point to one shared machine.

```yaml {filename="/config/tsdproxy.yaml"}
tailscale:
  providers:
    shared:
      shared: true
      hostname: "shared-proxy"
      clientId: "your_client_id"
      clientSecret: "your_client_secret"
```

See [Shared Tailscale]({{< ref "/docs/v3/advanced/tailscale#shared-tailscale" >}})
for full configuration details.

### Mode 4: Services/VIP

Multiple containers share a single Tailscale machine using Tailscale VIP
Services. Each service gets an auto-assigned FQDN from Tailscale (e.g.
`myapp.tailnet-name.ts.net`). No custom domain configuration or external DNS
provider is needed.

This mode requires `services: true` on the Tailscale provider config, plus
OAuth credentials (`clientId` + `clientSecret`). It does not support custom
domains or UDP — only HTTPS, HTTP, and TCP ports.

```yaml {filename="/config/tsdproxy.yaml"}
tailscale:
  providers:
    services:
      clientId: "your_client_id"
      clientSecret: "your_client_secret"
      tags: "tag:services"
      services: true
      hostname: "shared-services"
```

See [Services Mode]({{< ref "/docs/v3/advanced/tailscale#services-mode" >}})
for full configuration details.

### Shared Mode Data Flow

```
Docker Containers ──labels──► TargetProvider (Docker)
                                       │
                                       ▼  TargetEvent
                                 ProxyManager ◄── config
                                       │
                                       ▼  creates SharedProxy
                                 SharedServer (one tsnet.Server)
                                       │
                                       ▼  SNI routing
                                 VirtualListener per domain
                                       │
                                       ▼  DNS + ACME TLS
                                 https://app1.example.com → container 1
                                 https://app2.example.com → container 2
```

### Services Mode Data Flow

```
Docker Containers ──labels──► TargetProvider (Docker)
                                       │
                                       ▼  TargetEvent
                                 ProxyManager ◄── config
                                       │
                                       ▼  creates ServiceProxy
                                 ServicesServer (one tsnet.Server)
                                       │
                                       ▼  VIP Services API
                                 ServiceListener per service
                                       │
                                       ▼  auto-assigned FQDN
                                 https://app1.tailnet.ts.net → container 1
                                 https://app2.tailnet.ts.net → container 2
```

## Data Directory

TSDProxy stores persistent state under the data directory (default: `/data/`).
The layout looks like this:

```
/data/
  └── {provider-name}/        ← one folder per Tailscale proxy provider
      └── {hostname}/          ← one folder per proxy
          ├── tailscaled.state ← Tailscale node keys
          ├── cert-*.crt       ← TLS certificate
          ├── cert-*.key       ← TLS private key
          └── tsdproxy.yaml    ← cached OAuth tokens
```

> [!IMPORTANT]
> Losing the data directory means TSDProxy creates **new** Tailscale machines
> on next start. The old machines remain registered in your Tailscale admin
> console as stale nodes. You'll need to remove them manually.

See [Backup and Restore]({{< ref "/docs/v3/operations/backup" >}}) for
guidance on backing up this data.

## Configuration Resolution

Settings can be defined at multiple levels. TSDProxy resolves them from most
specific to least specific.

### General settings (hostname, ports, access log)

These come from the target provider. For Docker, that means container labels:

```
Per-proxy label (tsdproxy.name, tsdproxy.port, etc.)
    ↓ fallback
Target provider defaults
    ↓ fallback
Global defaults (built-in constants)
```

### Proxy provider selection

When TSDProxy needs to pick which Tailscale provider handles a proxy, it
follows this order:

```
Per-proxy label (tsdproxy.proxyprovider)
    ↓ not set
Target provider default (defaultProxyProvider in docker/lists config)
    ↓ not set
Global default (defaultProxyProvider in tsdproxy.yaml)
    ↓ not set
First available proxy provider
```

> [!TIP]
> If you only have one Tailscale provider (the common case), you don't need to
> set `defaultProxyProvider` anywhere. TSDProxy picks the only one available.

For full configuration options, see the
[Server Configuration]({{< ref "/docs/v3/serverconfig" >}}) page.
