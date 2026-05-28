---
title: Docker Labels Reference
prev: /docs/providers/docker
next: /docs/providers/lists
weight: 4
---

Quick reference for every Docker label TSDProxy supports. For detailed explanations, see the [Docker provider page]({{< ref "/docs/providers/docker" >}}).

## Required Labels

| Label | Value | Description |
|-------|-------|-------------|
| `tsdproxy.enable` | `"true"` | Enable proxying for this container |

This is the only label you need. TSDProxy will use the container name as the Tailscale hostname and auto-detect the first exposed port.

## Container Labels

| Label | Default | Description |
|-------|---------|-------------|
| `tsdproxy.name` | container name | Tailscale hostname |
| `tsdproxy.proxyprovider` | default provider | Proxy provider to use |
| `tsdproxy.autodetect` | `"true"` | Auto-detect the target URL |
| `tsdproxy.containeraccesslog` | `"true"` | Enable access logging |
| `tsdproxy.ephemeral` | `"false"` | Create an ephemeral Tailscale node |
| `tsdproxy.runwebclient` | `"false"` | Enable Tailscale web client on port 5252 |
| `tsdproxy.tsnet_verbose` | `"false"` | Enable verbose Tailscale logging |
| `tsdproxy.authkey` | — | Per-container auth key |
| `tsdproxy.authkeyfile` | — | Path to a file containing the auth key |
| `tsdproxy.tags` | — | Comma-separated Tailscale tags (OAuth only) |
| `tsdproxy.identity_headers` | `"true"` | Inject identity headers into upstream requests. Set to `"false"` to disable. Client-supplied headers are always stripped. |
| `tsdproxy.auto_restart` | `"true"` | Enable automatic re-resolution on backend failure |
| `tsdproxy.health_check_enabled` | `"true"` | Enable health probes. Set to `"false"` to disable all health monitoring for this container |
| `tsdproxy.health_check_interval` | `"30"` | Seconds between health probes |
| `tsdproxy.health_check_failures` | `"3"` | Consecutive failures before re-resolution |
| `tsdproxy.health_check_cooldown` | `"0"` | Fixed cooldown in seconds (0 = exponential backoff) |

## Dashboard Labels

| Label | Default | Description |
|-------|---------|-------------|
| `tsdproxy.dash.visible` | `"true"` | Show this proxy in the dashboard |
| `tsdproxy.dash.label` | proxy name | Display label in the dashboard |
| `tsdproxy.dash.icon` | auto-detected | Icon in `library/name` format. See [icons]({{< ref "/docs/advanced/icons" >}}) |
| `tsdproxy.dash.category` | — | Category for grouping proxies in the dashboard |

## Port Configuration

### Syntax

**Proxy a port:**

```
tsdproxy.port.<index>: "<proxy port>/<protocol>:<target port>/<protocol>[, <options>]"
```

**Short format** (auto-detects the target port):

```
tsdproxy.port.<index>: "<proxy port>/<protocol>"
```

**Redirect:**

```
tsdproxy.port.<index>: "<proxy port>/<protocol>-><redirect URL>"
```

- **index** starts at 1. Use separate indices for multiple ports.
- **proxy port** is the port exposed on the Tailscale network (e.g. 443, 80, 22).
- **protocol** is `http`, `https`, or `tcp`.
- **target port** is the container port to proxy to.
- **redirect URL** is a full URL like `https://example.com`.

**Port range** (forward multiple consecutive ports):

```
tsdproxy.port.<index>: "<start>-<end>/<protocol>:<start>-<end>/<protocol>[, <options>]"
```

- Both sides can be ranges. If both are ranges, they must have the same number of ports.
- One side can be a single port (reused for each port in the range).
- Maximum 1000 ports per range.
- Not supported with redirect syntax (`->`).

### Port Options

Append these after a comma to any proxy port config:

| Option | Description |
|--------|-------------|
| `no_tlsvalidate` | Disable TLS certificate validation on the target |
| `tailscale_funnel` | Expose the port publicly via Tailscale Funnel |
| `no_autodetect` | Disable auto-detection of the target URL for this port |

### Common Patterns

**HTTPS proxy to an HTTP backend:**

```yaml
tsdproxy.port.1: "443/https:80/http"
```

**HTTP redirect to HTTPS:**

```yaml
tsdproxy.port.1: "80/http->https://myapp.tailnet-name.ts.net"
```

**Self-signed certificate on the backend:**

```yaml
tsdproxy.port.1: "443/https:443/https, no_tlsvalidate"
```

**TCP proxy (SSH):**

```yaml
tsdproxy.port.1: "22/tcp:22/tcp"
```

**TCP proxy (database):**

```yaml
tsdproxy.port.1: "5432/tcp:5432/tcp"
```

**Multiple ports** (HTTPS + TCP):

```yaml
tsdproxy.port.1: "443/https:80/http"
tsdproxy.port.2: "22/tcp:22/tcp"
```

**Tailscale Funnel** (public internet access):

```yaml
tsdproxy.port.1: "443/https:80/http, tailscale_funnel"
```

**Host network mode** (skip auto-detect, explicit target):

```yaml
tsdproxy.port.1: "443/https:8080/http, no_autodetect"
```

**Port range** (WebRTC UDP):

```yaml
tsdproxy.port.1: "56000-56002/udp:56000-56002/udp"
```

**Port range** (all to one target port):

```yaml
tsdproxy.port.1: "50000-50099/tcp:8080/tcp"
```

## Complete Example

```yaml
services:
  nextcloud:
    image: nextcloud:latest
    container_name: nextcloud
    labels:
      tsdproxy.enable: "true"
      tsdproxy.name: "cloud"
      tsdproxy.ephemeral: "true"
      tsdproxy.containeraccesslog: "false"
      tsdproxy.tags: "tag:personal,tag:storage"
      tsdproxy.dash.label: "Nextcloud"
      tsdproxy.dash.icon: "si/nextcloud"
      # HTTPS to the container's HTTP port, with Funnel enabled
      tsdproxy.port.1: "443/https:80/http, tailscale_funnel"
      # HTTP redirect to HTTPS
      tsdproxy.port.2: "80/http->https://cloud.tailnet-name.ts.net"
```

## Legacy Labels (v1)

These labels still work but are deprecated. See the [legacy section on the Docker provider page]({{< ref "/docs/providers/docker#legacy-labels-v1" >}}) for details.

| Deprecated Label | Use Instead |
|------------------|-------------|
| `tsdproxy.container_port` | `tsdproxy.port.*` |
| `tsdproxy.scheme` | Protocol in `tsdproxy.port.*` |
| `tsdproxy.tlsvalidate` | `no_tlsvalidate` port option |
| `tsdproxy.funnel` | `tailscale_funnel` port option |
