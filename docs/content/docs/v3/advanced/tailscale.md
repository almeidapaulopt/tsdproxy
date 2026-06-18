---
title: Tailscale
prev: /docs/advanced/icons
next: /docs/scenarios
---


This document guides you through the different authentication and configuration
options for Tailscale with TSDProxy. For a quick comparison of authentication
methods, see [Authentication Methods]({{< ref "/docs/v3/security/auth-methods" >}}).

## Authentication Methods

TSDProxy supports three authentication methods with Tailscale: OAuth,
OAuth (manual), and AuthKey.

### OAuth

{{% steps %}}

#### Prerequisites

1. Generate an OAuth client at [https://login.tailscale.com/admin/settings/oauth](https://login.tailscale.com/admin/settings/oauth).
2. Under **Scopes**, enable the required permissions:
   - **Devices/Core**: `write` (to create and manage Tailscale machines)
   - **Keys/Auth Keys**: `write` (to generate single use keys for services)
   - **General/Services**: `write` (only needed for services/VIP mode)
   - **Policy/ACL**: `read` (to verify ACL tags are configured)
   - **Policy/ACL**: `write` (optional — needed when `autoProvisionAcl: true`)
3. Define tags for services. Tags can be defined in the provider, applying to
all services.

> [!Important]
> All auth keys created from an OAuth client require **tags**. This is a Tailscale requirement.

#### Configuration

Add the OAuth client credentials to the TSDProxy configuration:

```yaml {filename="/config/tsdproxy.yaml"}
tailscale:
  providers:
    default:
      clientId: "your_client_id"
      clientSecret: "your_client_secret"
      tags: "tag:example" # Optional if tags are defined in each proxy
```

> [!TIP]
> To avoid hardcoding `clientId` and `clientSecret` in the config file, you can
> set them via environment variables instead. See
> [Environment Variables]({{< ref "/docs/v3/advanced/environment-variables" >}})
> for details on `TSDPROXY_TAILSCALE_<NAME>_CLIENTID` and
> `TSDPROXY_TAILSCALE_<NAME>_CLIENTSECRET`.

#### Restart

Restart TSDProxy to apply the changes.

> [!Tip]
> If the proxy fails to authenticate after restarting, check the error logs.
> Ensure the tags are correct and the OAuth client is enabled.

{{% /steps %}}

### OAuth (Manual)

{{% steps %}}

#### Disable AuthKey

OAuth authentication mode is enabled when no AuthKey is set in the Tailscale
provider configuration:

```yaml {filename="/config/tsdproxy.yaml"}
tailscale:
  providers:
    default:
      authKey: ""
      authKeyFile: ""
```

The proxy will wait for authentication with Tailscale during startup.

#### Dashboard

Access the TSDProxy dashboard (e.g., `http://192.168.1.1:8080`).

#### Authentication

Click on the proxy with "Authentication" status.

> [!Tip]
> If "Ephemeral" is set to `true`, authentication is required at each TSDProxy restart.

{{% /steps %}}

### AuthKey

{{% steps %}}

#### Generate AuthKey

1. Go to [https://login.tailscale.com/admin/settings/keys](https://login.tailscale.com/admin/settings/keys).
2. Click "Generate auth key".
3. Add a description.
4. Enable "Reusable".
5. Add tags if needed.
6. Click "Generate key".

> [!Warning]
> If tags are added to the key, all proxies initialized with the same AuthKey will receive the same tags. To use different tags, add a new Tailscale provider to the configuration.

#### Configuration

Add the AuthKey to the TSDProxy configuration:

```yaml {filename="/config/tsdproxy.yaml"}
tailscale:
  providers:
    default:
      authKey: "YOUR_GENERATED_KEY_HERE"
      authKeyFile: ""
```

#### Restart

Restart TSDProxy to apply the changes.

{{% /steps %}}

## Funnel

In addition to configuring TSDProxy to enable Funnel, you need to grant
permissions in the Tailscale ACL. See [Troubleshooting](.././troubleshooting/#funnel-doesnt-work)
for more details. Also read Tailscale's [Funnel documentation](https://tailscale.com/kb/1223/funnel#requirements-and-limitations)
for requirements and limitations.

## Tags

- Tags are required for OAuth authentication.
- Tags only work with OAuth authentication.
- Tags can be configured in the provider or service.
- If tags are defined in the provider, they apply to all services.
- If tags are defined in the service, provider tags are ignored.

### ACL Auto-Provisioning

When using OAuth authentication with tags, those tags must exist in your
Tailscale ACL `tagOwners` or the proxy will fail to authenticate. Similarly,
Funnel requires the `funnel` node attribute in your ACL `nodeAttrs`.

TSDProxy can automate both of these:

```yaml {filename="/config/tsdproxy.yaml"}
tailscale:
  providers:
    default:
      clientId: "your_client_id"
      clientSecret: "your_client_secret"
      tags: "tag:my-service"
      autoProvisionAcl: true
```

When `autoProvisionAcl` is `true`, TSDProxy will:

1. **Check** each configured tag against your ACL `tagOwners`.
2. **Add** any missing tags to `tagOwners`, owned by `autogroup:admin`.
3. **Ensure** the `funnel` attribute exists in `nodeAttrs` (if any proxy uses Funnel).

All modifications go through the Tailscale API with ETag-based conflict
detection — if another process modifies the ACL concurrently, TSDProxy fails
safely instead of overwriting.

> [!IMPORTANT]
> Requires OAuth credentials (`clientId` + `clientSecret`) with the
> `policy:write` scope. The OAuth client must be granted this scope at
> [https://login.tailscale.com/admin/settings/oauth](https://login.tailscale.com/admin/settings/oauth).

> [!TIP]
> When `autoProvisionAcl` is `false` (default), TSDProxy performs a read-only
> check and logs a warning if tags are missing. Set it to `true` to fully
> automate ACL setup.

#### What gets added

Given a provider configured with `tags: "tag:my-service"` and
`autoProvisionAcl: true`, TSDProxy transforms an ACL that lacks both the tag
and the Funnel attribute into one that has both.

Before auto-provisioning, a minimal ACL might look like this:

```hujson
{
  "tagOwners": {
    "tag:existing": ["group:devs"],
  },
  "nodeAttrs": [],
}
```

After TSDProxy runs, the same ACL contains a new `tagOwners` entry and a new
`nodeAttrs` entry:

```hujson
{
  "tagOwners": {
    "tag:existing": ["group:devs"],
    "tag:my-service": ["autogroup:admin"],
  },
  "nodeAttrs": [
    {
      "target": ["tag:my-service"],
      "attr": ["funnel"],
    },
  ],
}
```

The new tag is always owned by `autogroup:admin`, which grants management
rights to all admins of the tailnet. Existing entries in `tagOwners` and
`nodeAttrs` are left untouched.

#### Funnel attribute provisioning

When `autoProvisionAcl` is `true`, TSDProxy proactively adds the `funnel`
node attribute on every startup, even if no proxy currently uses Funnel. This
avoids a second startup failure later when you enable Funnel on a container.

The check is global. If any entry in `nodeAttrs` already grants `funnel`,
TSDProxy does nothing. The attribute is only added once, scoped to the first
configured tag (or `autogroup:member` when no tag is set).

> [!NOTE]
> If you run multiple Tailscale providers with different tags, only the first
> provider to auto-provision writes the Funnel attribute, scoped to its tag.
> Subsequent providers skip the step because the attribute is already present.
> Manually broaden the `target` list in the policy file if you need Funnel for
> additional tags.

The operation is idempotent. Re-running TSDProxy against an ACL that already
has the entry leaves it unchanged.

#### Error scenarios and recovery

| Scenario | Symptom in logs | Resolution |
|----------|-----------------|------------|
| Missing `policy:write` scope | Error message names `policy:write` and links to the OAuth settings page | Edit the OAuth client at [https://login.tailscale.com/admin/settings/oauth](https://login.tailscale.com/admin/settings/oauth), add **Policy/ACL: write**, then restart TSDProxy |
| Missing `policy:read` scope | Error message names `policy:read` | Same page, add **Policy/ACL: read** |
| Concurrent ACL modification (ETag mismatch) | `write ACL: ... another process may have modified the policy file concurrently` | TSDProxy fails safe and does not overwrite. Trigger a config reload or restart TSDProxy to re-run auto-provisioning |
| Invalid ACL from a prior manual edit | `ACL validation failed (dry-run)` | Fix the ACL in the Tailscale admin console first. TSDProxy will not write on top of an invalid policy |
| Tailscale API rate limiting | Transient errors during `EnsureTags` or `EnsureFunnelAttribute` | The Tailscale SDK retries internally. Persistent failures surface as a startup error; restart TSDProxy after a brief wait |
| OAuth client revoked or expired | Startup fails with an auth error before ACL provisioning runs | Regenerate the OAuth client or restore its credentials, then restart |

> [!TIP]
> When auto-provisioning fails, TSDProxy does not partially apply changes. The
> provider is not registered, so no proxies using that provider start. Fix the
> underlying issue and restart.

#### Headscale and custom control servers

ACL auto-provisioning uses the Tailscale SaaS policy API
(`PolicyFile().Get`, `Validate`, `Set`). Headscale and other alternative
control servers do not implement this API, so `autoProvisionAcl` has no effect
when `controlURL` points elsewhere. OAuth credentials are also a Tailscale
SaaS feature, so the prerequisite checks fail before ACL provisioning runs.

With Headscale, manage ACLs manually in the headscale policy file. Tags must
exist there before any proxy can authenticate. See
[Headscale / Custom Control Server]({{< ref "/docs/v3/advanced/headscale" >}})
for setup details.

#### Auditing and rollback

Every change TSDProxy makes goes through the Tailscale policy API, which
records a revision. Inspect the results in two ways:

- **Tailscale admin console**: open
  [Access Controls](https://login.tailscale.com/admin/acls) to view the current
  policy and its revision history.
- **CLI**: run `tailscale acl get` to print the current ACL as HuJSON. This
  requires an OAuth client or API access token with the `policy:read` scope.

To roll back, set `autoProvisionAcl: false` and restart TSDProxy. Existing
`tagOwners` and `nodeAttrs` entries remain in your policy. TSDProxy reverts to
read-only mode, logging a warning if a tag is missing instead of adding it.
Remove the auto-provisioned entries themselves by deleting them from the
policy file in the Tailscale admin console.

> [!TIP]
> See the `autoProvisionAcl` option in
> [Server Configuration](../serverconfig/#autoprovisionacl) for the
> configuration reference.

## Prevent Duplicate Machines

When TSDProxy restarts and the data directory has been lost (e.g. non-persistent
Docker volume), Tailscale creates a new machine instead of reconnecting the
existing one. This results in duplicate machines in your tailnet, often with a
`-1` suffix.

The `preventDuplicates` option (default: `false`) tells TSDProxy to query the
Tailscale API before creating a new node. If an existing device with the same
hostname and matching tags is found **and is offline**, it is deleted first so
the new node can take its place.

A boolean option:

| Value | Behavior |
|-------|----------|
| `false` | Do not check for duplicate devices (default) |
| `true` | Check and remove offline duplicates before creating a new node (requires OAuth) |

> [!Warning]
> **This deletes devices from your tailnet.** Deleting a device also removes
> any manual configuration associated with it, including custom ACL rules,
> tags assigned in the Tailscale admin console, and device-specific settings.
> Only enable this if you understand the implications.
> The safest way to prevent duplicates is to use a persistent Docker volume
> for the `dataDir` directory.

### Requirements

- OAuth authentication (`clientId` + `clientSecret`) — the Tailscale API is
  not available with auth keys alone.
- Tags must be configured on the provider.

### Configuration

```yaml {filename="/config/tsdproxy.yaml"}
tailscale:
  providers:
    default:
      clientId: "your_client_id"
      clientSecret: "your_client_secret"
      tags: "tag:example"
      preventDuplicates: true
```

> [!TIP]
> You can omit `clientId` and `clientSecret` from the config file and set
> `TSDPROXY_TAILSCALE_DEFAULT_CLIENTID` and
> `TSDPROXY_TAILSCALE_DEFAULT_CLIENTSECRET` as environment variables instead.

### Safety checks

A device is only deleted when **all** of these conditions are true:

- It has the same hostname as the proxy being created
- It has matching tags
- It is currently offline (`ConnectedToControl` is false)
- The local tsnet state file is missing (no existing identity to reuse)

Online devices are never deleted.

## Certificate Concurrency

When many ephemeral containers restart at once, TSDProxy requests TLS
certificates for all of them simultaneously. The Tailscale local API cannot
handle this thundering herd, resulting in `context deadline exceeded` errors
and failed certificate generation.

The `maxCertConcurrency` option (default: `2`) limits how many certificate
generation requests run in parallel. Requests that exceed the limit wait for
a slot and are logged at `warn` level if delayed by more than one second.

```yaml {filename="/config/tsdproxy.yaml"}
tailscale:
  providers:
    default:
      maxCertConcurrency: 3 # allow up to 3 parallel cert requests
```

> [!Tip]
> The default of `2` is sufficient for most deployments. Increase it only
> if you run 50+ containers and want faster startup at the cost of higher
> load on the Tailscale coordination server. Values below `1` are invalid
> and fall back to the default.

## Identity Headers

TSDProxy resolves the Tailscale identity of each incoming request and forwards
it to your backend services via HTTP headers. All identity headers are stripped
from the incoming request before being set, preventing header injection attacks.

Unauthenticated requests (e.g. via Funnel) will not receive identity headers.

### TSDProxy Headers

| Header | Value |
|--------|-------|
| `x-tsdproxy-username` | Tailscale login name |
| `x-tsdproxy-displayname` | Tailscale display name |
| `x-tsdproxy-profilepicurl` | Tailscale profile picture URL |

### Standard Auth Headers

These headers are recognized by common reverse-proxy-aware backends
(Authelia, OAuth2 Proxy, Traefik, FileBrowser, etc.):

| Header | Value | Used by |
|--------|-------|---------|
| `Remote-User` | Tailscale login name | Apache, Nginx, FileBrowser |
| `X-Forwarded-User` | Tailscale login name | Traefik, Authelia, many apps |
| `X-Auth-Request-User` | Tailscale login name | OAuth2 Proxy |
| `X-Forwarded-Email` | Tailscale login name | Keycloak, Authentik |
| `X-Auth-Request-Email` | Tailscale login name | OAuth2 Proxy |
| `X-Forwarded-Preferred-Username` | Tailscale display name | OpenShift, Kubernetes |

### Standard Proxy Headers

| Header | Value |
|--------|-------|
| `X-Forwarded-For` | Client IP address |
| `X-Forwarded-Host` | Original host header |
| `X-Forwarded-Proto` | Original protocol |

### Usage Example: FileBrowser

FileBrowser supports proxy authentication out of the box. Configure it to read
the `X-Forwarded-User` header set by TSDProxy:

```bash
filebrowser --auth.method=proxy --auth.header=X-Forwarded-User
```

Users will be automatically logged in with their Tailscale login name.

## Shared Tailscale

By default, each proxy gets its own Tailscale connection (tsnet.Server). When you
enable shared mode, multiple proxies share a single Tailscale connection, which is
useful when you want to conserve Tailscale machine quota or centralize DNS and TLS
management.

### How it works

- All shared proxies use one `tsnet.Server` with SNI (Server Name Indication) routing
- Incoming TLS connections are dispatched by domain name to the correct proxy
- Each proxy must have a custom domain set (`tsdproxy.domain` label or `domain` in
  list config) because SNI routing depends on unique domain names
- **Only HTTPS ports are supported** in shared mode — TCP and plain HTTP ports
  cannot be multiplexed by SNI and will be rejected at startup

  > [!NOTE]
  > SNI routing inspects the TLS ClientHello to determine which domain the client
  > is connecting to. Without TLS, there is no SNI to inspect, so multiple proxies
  > cannot share a single listener on the same port. HTTP redirects (`80/http->...`)
  > are also excluded because they would conflict when multiple proxies try to bind
  > port 80 on the shared server. If you need TCP or redirect ports alongside shared
  > mode, use a per-proxy Tailscale provider for those containers instead.
- The shared server starts when the first proxy is created and stops when the last
  proxy is removed

### Configuration

```yaml {filename="/config/tsdproxy.yaml"}
defaultProxyProvider: shared

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

tailscale:
  providers:
    shared:
      clientId: "your_client_id"
      clientSecret: "your_client_secret"
      tags: "tag:shared-proxy"
      shared: true
      hostname: "shared-proxy"
  dataDir: /data/

docker:
  local:
    host: unix:///var/run/docker.sock
    defaultProxyProvider: shared
```

Container labels for shared proxies:

```yaml
services:
  app1:
    image: nginx:alpine
    labels:
      tsdproxy.enable: "true"
      tsdproxy.name: "app1"
      tsdproxy.domain: "app1.example.com"

  app2:
    image: nginx:alpine
    labels:
      tsdproxy.enable: "true"
      tsdproxy.name: "app2"
      tsdproxy.domain: "app2.example.com"
```

### Requirements

> [!TIP]
> To keep `clientId` and `clientSecret` out of the config file, set
> `TSDPROXY_TAILSCALE_SHARED_CLIENTID` and
> `TSDPROXY_TAILSCALE_SHARED_CLIENTSECRET` as environment variables instead.

> [!IMPORTANT]
> Shared Tailscale mode requires a custom domain on every proxy. Without a domain,
> the proxy cannot be routed via SNI and will fail to start. Configure DNS and TLS
> providers as described in [Custom Domains]({{< ref "/docs/v3/advanced/custom-domains" >}}).

### When to use shared mode

- Fewer Tailscale machines in your tailnet
- All domains point to a single Tailscale hostname
- Centralized DNS and TLS management

## Services Mode

Services mode uses the Tailscale VIP Services API to automatically assign FQDNs
to each proxy. Unlike shared mode, no custom domains, external DNS, or TLS
providers are needed — Tailscale handles everything.

### How it works

- All services share one `tsnet.Server` (like shared mode)
- Each proxy is registered as a Tailscale VIP Service
- FQDNs are auto-assigned by Tailscale (e.g. `myapp.tailnet-name.ts.net`)
- **No custom domain support** — you cannot set `tsdproxy.domain`
- **No UDP support** — only HTTPS, HTTP, and TCP ports
- The shared server starts when the first service is created and stops when the
  last service is removed

### Configuration

```yaml {filename="/config/tsdproxy.yaml"}
defaultProxyProvider: services

tailscale:
  providers:
    services:
      clientId: "your_client_id"
      clientSecret: "your_client_secret"
      tags: "tag:services-proxy"
      services: true
      hostname: "shared-services"
      autoApproveDevices: true
  dataDir: /data/

docker:
  local:
    host: unix:///var/run/docker.sock
    defaultProxyProvider: services
```

Container labels for services mode proxies:

```yaml
services:
  app1:
    image: nginx:alpine
    labels:
      tsdproxy.enable: "true"
      tsdproxy.name: "app1"

  app2:
    image: nginx:alpine
    labels:
      tsdproxy.enable: "true"
      tsdproxy.name: "app2"
```

### Requirements

> [!IMPORTANT]
> Services mode requires OAuth credentials (`clientId` + `clientSecret`). Auth
> keys alone do not provide access to the VIP Services API. A `hostname` must
> also be set — this is the shared Tailscale machine name.

> [!TIP]
> Set `autoApproveDevices: true` to automatically approve new device registrations.
> Without this, new devices may require manual approval in the Tailscale admin
> console, which will block the proxy from starting.

### Constraints

- **No custom domains** — FQDNs are auto-assigned by Tailscale from the tailnet name
- **No UDP** — VIP Services do not support UDP traffic
- **HTTPS, HTTP, and TCP only** — all other protocols are rejected at startup
- **Mutually exclusive with `shared`** — a provider cannot use both `shared: true`
  and `services: true`

### Auto-remove conflicting devices

When switching from per-proxy or shared mode to services mode, existing
Tailscale devices may share hostnames with the VIP services being created.
This causes the Tailscale API to return a `409 "name is in use but is not a
service"` error, preventing the proxy from starting.

The `autoRemoveConflicts` option (default: `false`) enables automatic
removal of conflicting devices when this error is encountered. After removing
the device, TSDProxy retries the VIP service creation.

```yaml {filename="/config/tsdproxy.yaml"}
tailscale:
  providers:
    default:
      clientId: "your_client_id"
      clientSecret: "your_client_secret"
      tags: "tag:example"
      services: true
      hostname: "shared-services"
      autoRemoveConflicts: true
```

> [!Warning]
> **This deletes devices from your tailnet.** When a 409 conflict is detected,
> TSDProxy will delete the conflicting device regardless of whether it is
> online or offline, and regardless of its tags. Only enable this if you
> understand the implications.

> [!TIP]
> This option requires OAuth credentials (`clientId` + `clientSecret`) to
> access the Tailscale device API.

### When to use services mode

- You want fewer Tailscale machines without managing external DNS
- Auto-assigned `.ts.net` FQDNs are acceptable for your use case
- You don't need UDP or custom domains

## Proxy Provider Resolution

1. Per-proxy label (tsdproxy.proxyprovider)
2. Target provider default (defaultProxyProvider)
3. Global default (top-level defaultProxyProvider)
4. First available provider

## Proxy Lifecycle

| State | Description |
|-------|-------------|
| Initializing | Being created |
| Starting | Connecting to Tailscale |
| Authenticating | Waiting for auth (visit the auth URL) |
| AwaitingApproval | Registered, waiting for admin approval in Tailscale |
| AuthFailed | Authentication failed (invalid key, bad tags, etc.) |
| DeviceConflict | Hostname collision with an existing Tailscale device |
| Reconciling | Cleaning up stale devices before starting |
| Running | Active |
| Stopping | Shutting down |
| Stopped | Removed |
| Paused | Temporarily disabled |
| Error | Fatal error |

> [!NOTE]
> The **AwaitingApproval** status appears when a node registers with Tailscale
> but an admin needs to approve it in the Tailscale admin console. This is
> separate from **Authenticating**, which means the node has no credentials at
> all and needs the user to visit an authentication URL.

> [!NOTE]
> The **AuthFailed** status indicates a permanent authentication failure
> (invalid auth key, mismatched tags, or expired credentials). The proxy will
> not retry automatically unless `authRetry` is configured. Check the logs for
> the specific error.

> [!NOTE]
> The **DeviceConflict** status means another Tailscale device with the same
> hostname already exists and is online. TSDProxy will not delete online
> devices. Either remove the conflicting device manually from the Tailscale
> admin console, or enable `preventDuplicates` for automatic cleanup of offline
> duplicates.
