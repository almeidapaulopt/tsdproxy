---
title: How TSDProxy Works
weight: 0
prev: /docs/v2
next: /docs/v2/getting-started
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
[Tailscale provider settings]({{< ref "/docs/v2/advanced/tailscale" >}}),
authentication happens automatically. Otherwise, the dashboard shows an
"Authenticating" status and gives you a link to log in through your browser.

### TLS certificate is obtained

After authentication, TSDProxy requests a TLS certificate via Let's Encrypt
(tied to the MagicDNS name). Your service is now reachable at
`https://your-container.tailnet-name.ts.net`.

### Traffic starts flowing

The reverse proxy begins forwarding requests from the Tailscale node to the
container's internal port. Your service is live on your tailnet.

{{% /steps %}}

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

See [Backup and Restore]({{< ref "/docs/v2/operations/backup" >}}) for
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
[Server Configuration]({{< ref "/docs/v2/serverconfig" >}}) page.
