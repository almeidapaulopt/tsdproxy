---
title: Proxmox
prev: /docs/providers/incus
next: /docs/providers/lists
weight: 6
---

TSDProxy can proxy Proxmox VE guests — QEMU virtual machines and LXC
containers — on your Tailscale network. Guests are discovered by polling the
cluster resources API and configured through a `tsdproxy:` YAML block in the
guest **notes** (VMs) or **description** (containers), the only free-form
field Proxmox offers.

{{% steps %}}

### How to enable?

Add a `proxmox` section to `/config/tsdproxy.yaml`. Each entry is one target
provider connected to a Proxmox VE API endpoint, authenticated with an API
token:

```yaml  {filename="/config/tsdproxy.yaml"}
proxmox:
  pve:
    url: https://pve.example.com:8006
    apiToken: tsdproxy@pve!tsdproxy=e14b8a0c-1111-4bbb-9ccc-2222dddd3333
    tlsCaCertFile: /config/proxmox/pve-root-ca.pem
    defaultProxyProvider: default
```

The token can also be loaded from a file — useful to keep secrets out of the
config file:

```yaml  {filename="/config/tsdproxy.yaml"}
proxmox:
  pve:
    url: https://pve.example.com:8006
    apiTokenFile: /config/proxmox/token
```

> [!NOTE]
> Proxmox VE has no push event API. TSDProxy polls the cluster resources
> endpoint every `pollIntervalSeconds` (default `10`) and diffs the guest
> states — starting, stopping and config changes are picked up within one
> poll cycle.

### Creating an API token

TSDProxy authenticates with a Proxmox API token in the format
`<user>@<realm>!<tokenid>=<secret>`. It only needs read access to cluster
resources, guest configs and the QEMU guest agent interface API.

{{% steps %}}

#### 1. Create a dedicated read-only user (recommended)

On any Proxmox node (or via the web UI under **Datacenter → Permissions**):

```bash {title="on the Proxmox host"}
pveum user add tsdproxy@pve --comment "TSDProxy"
pveum acl modify / --users tsdproxy@pve --roles PVEAuditor
```

The `PVEAuditor` role grants exactly the read access TSDProxy needs and
nothing more.

#### 2. Create the token

```bash {title="on the Proxmox host"}
pveum user token add tsdproxy@pve tsdproxy -privsep 0
```

The command prints the token **value** (a UUID) — combine it with the full
token ID into the config value:

```
tsdproxy@pve!tsdproxy=<printed-value>
```

> [!WARNING]
> The value is only shown at creation time. If you lose it, generate a new
> one with `pveum user token update tsdproxy@pve tsdproxy -newvalue 1`.

#### 3. Trust the server certificate

Proxmox VE serves its API with a self-signed certificate by default. Copy
the Proxmox CA certificate to the TSDProxy machine:

```bash {title="on your workstation"}
scp root@pve.example.com:/etc/pve/pve-root-ca.pem /config/proxmox/pve-root-ca.pem
```

Without it you would have to set `tlsInsecureSkipVerify: true`, which is
discouraged: the connection is still encrypted, but TSDProxy can no longer
detect a man-in-the-middle or a server reinstall.

> [!TIP]
> If the PVE web UI certificate is issued by a public CA (ACME), you can
> omit `tlsCaCertFile`.

#### 4. Verify the token

From the machine running TSDProxy:

```bash {title="on the TSDProxy machine"}
curl -k -H "Authorization: PVEAPIToken=tsdproxy@pve!tsdproxy=<value>" \
  https://pve.example.com:8006/api2/json/version
```

TSDProxy performs the same check at startup and logs a clear error when the
token or the URL is wrong.

{{% /steps %}}

### Enable a guest

Edit the guest's **notes** (VMs) or **description** (containers) in the
Proxmox web UI and add a `tsdproxy:` YAML block. A guest is enabled as soon
as the block carries any data — an explicit `enable: false` opts out.

Two styles are accepted and can be mixed:

```yaml {title="guest notes/description"}
# Docker/Incus style — flat keys, port label grammar
tsdproxy:
  port:
    443: 443/https:8080/http
  dash.icon: jellyfin
```

```yaml {title="guest notes/description"}
# list provider style — same keys as a lists file entry
tsdproxy:
  hostname: myapp
  ports:
    "443/https":
      targets:
        - "http://10.0.0.5:8080"
      tailscale:
        funnel: true
```

> [!NOTE]
> The whole notes field must be valid YAML — prefix free-text prose with
> `#` comment markers. Multi-line notes delivered base64-encoded by older
> PVE releases are decoded automatically.

The guest becomes available at `https://<hostname>.<tailnet-name>.ts.net`.
The hostname defaults to the guest name; VMs and containers behave
identically.

### Flat config keys

Flat keys use the `tsdproxy.` prefix and mirror the [Docker
labels](/docs/providers/docker-reference/) semantics:

| Key | Description |
|-----|-------------|
| `tsdproxy.enable` | Set to `false` to disable an enabled guest (presence of the block enables it) |
| `tsdproxy.name` | Tailscale hostname (defaults to the guest name) |
| `tsdproxy.port.<N>` | Port config, same format as Docker: `443/https:80/http`, ranges and redirects supported |
| `tsdproxy.proxyprovider` | Override the Tailscale proxy provider |
| `tsdproxy.ephemeral` | Ephemeral Tailscale node |
| `tsdproxy.runwebclient` | Enable the Tailscale web client |
| `tsdproxy.tsnet_verbose` | Verbose tsnet logging |
| `tsdproxy.authkey` / `tsdproxy.authkeyfile` | Per-guest Tailscale auth key |
| `tsdproxy.tags` | Tailscale ACL tags |
| `tsdproxy.identity_headers` | Inject identity headers (default: enabled) |
| `tsdproxy.containeraccesslog` | Enable access log |
| `tsdproxy.auto_restart` | Auto-restart on failure (default: provider setting) |
| `tsdproxy.health_check_*` | Health check tuning (enabled, interval, failures, cooldown) |
| `tsdproxy.ratelimit.enabled` / `.rps` / `.burst` | Per-proxy rate limiting |
| `tsdproxy.dash.visible` / `.label` / `.icon` / `.category` | Dashboard appearance |
| `tsdproxy.domain` | Custom FQDN (shared/exposure modes with external DNS) |
| `tsdproxy.dnsprovider` / `tsdproxy.tlsprovider` | Per-proxy DNS / TLS providers |

### List provider style

Entries inside the `tsdproxy:` block accept the same keys as a [lists
provider](/docs/providers/lists/) file: `hostname`, `proxyProvider`,
`domain`, `dnsProvider`, `tlsProvider`, `identityHeaders`, `dashboard`,
`tailscale` and `ports` with per-port `targets`, `tlsValidate`,
`isRedirect` and `tailscale.funnel`.

When a port entry has no `targets`, the target URL is generated from the
guest address (see below) instead of an explicit URL — the list format
without hardcoded IPs:

```yaml {title="guest notes/description"}
tsdproxy:
  ports:
    "443/https": {}          # → http(s)://<guest-address>:443
    "8080/tcp:9090/tcp": {}  # → tcp://<guest-address>:9090
```

Flat keys are canonical: when both styles set the same field, the flat key
wins.

Port options `no_tlsvalidate` and `tailscale_funnel` (or the list-style
`tlsValidate: false` and `funnel: true`) require the provider options
`allowTlsValidateDisable` and `allowGuestFunnel` respectively, matching the
Docker provider's security defaults.

### Target resolution

1. `targetHostname` from the provider config with the port's target port, if set
2. Otherwise the guest's first usable address:
   - **LXC containers** — addresses from the Proxmox interfaces API
   - **QEMU VMs** — addresses reported by the [QEMU guest
     agent](https://pve.proxmox.com/wiki/Qemu-guest-agent) via
     `network-get-interfaces`; the agent must be installed in the guest OS
     and enabled in the VM options (`agent: 1`)

VMs without a running guest agent resolve no addresses — set
`targetHostname` or use explicit list-style `targets` for them.

### Live updates

Guest notes are re-read on every poll. Changing `tsdproxy` data restarts the
proxy automatically — only the `tsdproxy` block is compared, so editing
unrelated notes never restarts proxies. Stopping, starting, deleting or
disabling a guest tears the proxy down on the next poll.

### Provider options

| Option | Default | Description |
|--------|---------|-------------|
| `url` | — | Proxmox VE API URL (**required**) |
| `apiToken` / `apiTokenFile` | — | API token `<user>@<realm>!<tokenid>=<secret>` (**required**) |
| `tlsCaCertFile` | — | CA certificate to verify the PVE API (recommended for self-signed setups) |
| `tlsInsecureSkipVerify` | `false` | Skip server certificate verification (discouraged) |
| `node` | — | Only watch guests on this node (multi-node clusters) |
| `pollIntervalSeconds` | `10` | Poll interval for guest discovery |
| `targetHostname` | — | Fallback target host for all guests of this provider |
| `defaultProxyProvider` | — | Tailscale proxy provider for guests that don't set one |
| `allowGuestFunnel` | `false` | Allow the `tailscale_funnel` port option |
| `allowTlsValidateDisable` | `false` | Allow the `no_tlsvalidate` port option |
| `healthCheckEnabled` / `.interval` / `.failures` / `.cooldown` | `true` / `30` / `3` / `0` | Health check defaults for all guests |
| `rateLimitEnabled` / `.rps` / `.burst` | `true` / `100` / `200` | Rate limit defaults for all guests |
| `autoRestart` | `true` | Auto-restart failed proxies |

{{% /steps %}}
