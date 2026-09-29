---
title: Incus
prev: /docs/providers/docker-reference
next: /docs/providers/proxmox
weight: 5
---

TSDProxy can proxy Incus instances (containers and virtual machines) on your
Tailscale network. Instances are discovered via `user.tsdproxy.*` instance
configuration keys — the Incus equivalent of Docker labels, since Incus
reserves the `user.*` key namespace for free-form configuration.

{{% steps %}}

### How to enable?

Add an `incus` section to `/config/tsdproxy.yaml`. Each entry is one target
provider connected to an Incus daemon and project:

```yaml  {filename="/config/tsdproxy.yaml"}
incus:
  local:
    # Local unix socket. Leave empty to auto-detect
    # (/run/incus/unix.socket or /var/lib/incus/unix.socket).
    socket: /var/lib/incus/unix.socket
    project: default
    defaultProxyProvider: default
```

For a remote Incus daemon over HTTPS:

```yaml  {filename="/config/tsdproxy.yaml"}
incus:
  remote:
    url: https://incus.example.com:8443
    tlsClientCertFile: /config/incus/client.crt
    tlsClientKeyFile: /config/incus/client.key
    tlsServerCertFile: /config/incus/server.crt # optional, trust the remote CA
    project: production
```

### Connecting to a remote Incus

The Incus API uses mutual TLS: tsdproxy needs a **client certificate trusted by
the remote server** (for authentication) and the **server certificate** (to
verify the server, since Incus self-signs by default). Here is how to get both.

{{% steps %}}

#### 1. Expose the Incus daemon on the network

On the Incus host, make the API listen on the network (default port 8443) and
allow it through the firewall:

```bash
incus config set core.https_address :8443
```

#### 2. Create a client certificate trusted by the server

**Option A — trust token (recommended):** generate a token on the Incus host,
optionally restricted to a single project since tsdproxy only needs to read
instances, state and events:

```bash {title="on the Incus host"}
incus config trust add tsdproxy --restricted --projects production
```

Then, on any machine with the `incus` CLI (your workstation works), redeem the
token. This generates a client certificate and registers it with the server:

```bash {title="on your workstation"}
incus remote add tsdproxy-remote <paste-token>
```

The CLI stores the generated pair at `~/.config/incus/client.crt` and
`~/.config/incus/client.key`. Copy both files to the machine running TSDProxy
(e.g. `/config/incus/`).

> [!NOTE]
> If the Incus server is behind NAT, redeem the token with
> `incus remote add tsdproxy-remote <token> <external-address>` — the token
> embeds the server's local addresses, which may not be reachable from outside.

**Option B — manual certificate:** generate a pair with OpenSSL and register
the certificate on the Incus host:

```bash {title="on your workstation"}
openssl req -x509 -newkey rsa:4096 -sha256 -days 3650 -nodes \
  -keyout client.key -out client.crt -subj "/CN=tsdproxy"
```

```bash {title="on the Incus host"}
incus config trust add-certificate client.crt
```

#### 3. Get the server certificate

Incus self-signs its server certificate, so tsdproxy needs a copy to verify
the connection. On the Incus host it lives at `/var/lib/incus/server.crt` —
copy it to the TSDProxy machine (e.g. `/config/incus/server.crt`).

Without it you would have to set `tlsInsecureSkipVerify: true`, which is
discouraged: the connection is still encrypted, but tsdproxy can no longer
detect a man-in-the-middle or a server reinstall.

> [!TIP]
> If the server certificate is issued by a public CA (Incus
> [ACME support](https://linuxcontainers.org/incus/docs/main/howto/server_expose/),
> e.g. Let's Encrypt), you can omit `tlsServerCertFile`.

#### 4. Point tsdproxy at the remote

Use the YAML snippet above with the three files. Verify the client pair works
from the machine you redeemed the token on (it uses the same certificate
tsdproxy will use):

```bash
incus list tsdproxy-remote: --project production
```

To revoke a client later: `incus config trust remove <fingerprint>` on the
Incus host.

{{% /steps %}}

> [!NOTE]
> `socket` and `url` are mutually exclusive. `socket` and `project` may be
> empty — the socket is then auto-detected and the `default` project is used.

When TSDProxy runs inside a Docker container on the same host as Incus,
mount the Incus socket and set `targetHostname` to a host reachable from the
container:

```yaml  {filename="/config/tsdproxy.yaml"}
incus:
  local:
    socket: /var/lib/incus/unix.socket
    targetHostname: host.docker.internal
```

### Enable an instance

Set `user.tsdproxy.enable=true` on any Incus instance:

```bash
incus config set myapp user.tsdproxy.enable=true
incus config set myapp user.tsdproxy.port.1=443/https:80/http
```

The instance becomes available at `https://myapp.<tailnet-name>.ts.net`.
Virtual machines work identically — VM state addresses are used the same way
as container addresses.

### Config keys

All keys use the `user.tsdproxy.` prefix and mirror the [Docker
labels](/docs/providers/docker-reference/) semantics:

| Key | Description |
|-----|-------------|
| `user.tsdproxy.enable` | Set to `true` to expose the instance (required) |
| `user.tsdproxy.name` | Tailscale hostname (defaults to the instance name) |
| `user.tsdproxy.port.<N>` | Port config, same format as Docker: `443/https:80/http`, ranges and redirects supported |
| `user.tsdproxy.proxyprovider` | Override the Tailscale proxy provider |
| `user.tsdproxy.ephemeral` | Ephemeral Tailscale node |
| `user.tsdproxy.runwebclient` | Enable the Tailscale web client |
| `user.tsdproxy.tsnet_verbose` | Verbose tsnet logging |
| `user.tsdproxy.authkey` / `user.tsdproxy.authkeyfile` | Per-instance Tailscale auth key |
| `user.tsdproxy.tags` | Tailscale ACL tags |
| `user.tsdproxy.identity_headers` | Inject identity headers (default: enabled) |
| `user.tsdproxy.containeraccesslog` | Enable access log |
| `user.tsdproxy.auto_restart` | Auto-restart on failure (default: provider setting) |
| `user.tsdproxy.health_check_*` | Health check tuning (enabled, interval, failures, cooldown) |
| `user.tsdproxy.ratelimit.enabled` / `.rps` / `.burst` | Per-proxy rate limiting |
| `user.tsdproxy.dash.visible` / `.label` / `.icon` / `.category` | Dashboard appearance |
| `user.tsdproxy.domain` | Custom FQDN (shared/exposure modes with external DNS) |
| `user.tsdproxy.dnsprovider` / `user.tsdproxy.tlsprovider` | Per-proxy DNS / TLS providers |

Port options `no_tlsvalidate` and `tailscale_funnel` require the provider
options `allowTlsValidateDisable` and `allowInstanceFunnel` respectively,
matching the Docker provider's security defaults.

### Target resolution

Incus has no published ports, so the target is resolved as:

1. `targetHostname` from the provider config with the port's target port, if set
2. The instance's first global IPv4 address (IPv6 fallback) from the instance
   state — eth0 preferred

After an instance starts, TSDProxy polls its state for up to 10 seconds
waiting for DHCP/cloud-init to assign an address.

### Live updates

Editing `user.tsdproxy.*` keys triggers a proxy restart automatically —
only tsdproxy keys are compared, so unrelated config changes (profiles,
volatile keys, devices) never restart proxies:

```bash
incus config set myapp user.tsdproxy.port.1=443/https:8080/http
```

### Freeze / unfreeze

Freezing an instance (`incus freeze`) suspends all its processes, so
TSDProxy tears the proxy down — the Tailscale machine disappears while the
instance is frozen. Unfreezing (`incus unfreeze`) re-creates it
automatically. Instances that were already frozen when TSDProxy starts are
skipped until unfrozen.

### Tailscale exposure modes

Incus instances work with all Tailscale exposure modes — per-proxy, shared,
and Services/VIP (`services: true` on the Tailscale provider). When using
Services mode:

- Do not set `user.tsdproxy.domain` — VIP Services assign FQDNs automatically
- UDP ports are not supported (`/tcp`, `/http`, `/https` only)
- The instance hostname (or `user.tsdproxy.name`) becomes the VIP service
  name (`svc:<hostname>`)
- Per-instance `user.tsdproxy.authkey` is ignored — the shared services node
  authenticates via OAuth

### Provider configuration reference

| Option | Default | Description |
|--------|---------|-------------|
| `socket` | *(auto-detect)* | Path to the local Incus unix socket |
| `url` | *(empty)* | Remote Incus HTTPS URL (mutually exclusive with `socket`) |
| `tlsClientCertFile` / `tlsClientKeyFile` | *(empty)* | TLS client certificate pair for remote connections |
| `tlsServerCertFile` | *(empty)* | Remote server CA certificate |
| `tlsInsecureSkipVerify` | `false` | Skip TLS verification (not recommended) |
| `project` | `default` | Incus project to watch |
| `targetHostname` | *(empty)* | Route traffic via this host instead of instance addresses |
| `defaultProxyProvider` | global default | Tailscale proxy provider for this target provider |
| `autoRestart` | `true` | Default `user.tsdproxy.auto_restart` for instances |
| `healthCheckEnabled` / `Interval` / `Failures` / `Cooldown` | `true` / `30` / `3` / `0` | Health check defaults |
| `rateLimitEnabled` / `Rps` / `Burst` | `true` / `100` / `200` | Rate limit defaults |
| `allowInstanceFunnel` | `false` | Allow instances to request Tailscale Funnel |
| `allowTlsValidateDisable` | `false` | Allow instances to disable TLS validation |

{{% /steps %}}
