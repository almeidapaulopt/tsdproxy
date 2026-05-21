---
title: Admin Allowlist
prev: /docs/security
weight: 4
---

Restrict access to sensitive dashboard actions by configuring an admin allowlist.
With the allowlist enabled, only specific Tailscale users (or API key holders)
can restart, pause, resume, or reauth proxies — even if they have access to the
tailnet. All other tailnet users can view proxy status and preferences (viewer role).

## How It Works

TSDProxy identifies the caller using Tailscale's `WhoIs` API, which resolves
the peer identity from the connection's source IP via the tailnet control plane.
This mechanism is **not spoofable by the client** — it does not rely on headers,
cookies, or any data the browser sends.

Alternatively, callers can authenticate using an API key via the `Authorization:
Bearer <key>` header. API keys grant full admin access.

Each Tailscale user has a stable, tailnet-scoped numeric ID (`UserProfile.ID`)
that cannot change. The allowlist compares this ID against a configured list,
using the login name (`UserProfile.LoginName`) only for display.

The identity is resolved differently depending on how the request reaches
the dashboard:

| Request path | Resolution method |
|---|---|
| Direct tsnet connection | `WhoIs(remoteAddr)` on the proxy's local Tailscale client |
| Through the `dash-dev` reverse proxy | `x-tsdproxy-id` header, set by the in-process reverse proxy after stripping client-supplied headers |

> [!NOTE]
> Non-Tailscale connections (direct TCP to port 8080 without going through
> a Tailscale proxy) cannot resolve an identity. The allowlist rejects such
> requests unless `adminAllowLocalhost` is explicitly enabled for bootstrapping.

## Configuration

Add the allowlist to your `tsdproxy.yaml`:

```yaml {filename="/config/tsdproxy.yaml"}
# Admin allowlist — only these Tailscale UserProfile.IDs can call admin endpoints.
# Use /api/whoami through a Tailscale connection to discover your ID.
admins:
  - "12345"  # alice@github
  - "67890"  # bob@example.com

# Permit localhost requests to bypass the allowlist (for bootstrapping).
# Only enable this temporarily — any process on the host can then call
# admin endpoints.
adminAllowLocalhost: false
```

### Fields

| Field | Type | Default | Description |
|---|---|---|---|
| `admins` | `[]string` | (empty) | List of Tailscale `UserProfile.ID` values. All tailnet users can view the dashboard; only listed IDs can perform admin actions. |
| `adminAllowLocalhost` | `bool` | `false` | When `true`, requests from `127.0.0.0/8` or `::1` bypass the allowlist. Intended for bootstrapping only. |
| `apiKey` | `string` | (empty) | Static API key for non-Tailscale authentication. Grants full admin access. |
| `apiKeyFile` | `string` | (empty) | Path to a file containing the API key. Takes precedence over `apiKey`. |

> [!NOTE]
> All dashboard and API endpoints now require authentication by default. Every
> request must present a valid Tailscale identity, API key, or come from localhost
> with `adminAllowLocalhost` enabled. This is a change from previous versions where
> an empty `admins` list left endpoints unprotected.

## Bootstrapping the Allowlist

You need your `UserProfile.ID` to add yourself as an admin. Visit the
[`/api/whoami`]({{< ref "/docs/operations/api#whoami" >}}) endpoint
through a Tailscale connection to discover your ID:

```
https://<your-dashboard-node>.<tailnet>.ts.net/api/whoami
```

> [!TIP]
> If you're setting up the allowlist for the first time, temporarily set
> `adminAllowLocalhost: true` so you can reach `/api/whoami` from the host.
> Remove it once you've added your ID.

When a non-admin user attempts an admin action, TSDProxy logs the caller's
identity at `warn` level. Check the logs after a failed attempt to find the ID.

## Protected Endpoints

The allowlist protects state-changing endpoints (restart, pause, resume,
reauth, webhook test). All tailnet users can view proxy status, browse the
dashboard, and manage their own preferences (viewer role). Only admins
(users in the `admins` list or authenticated via API key) see the Actions
and Logs tabs in the proxy detail modal.

See the [API reference]({{< ref "/docs/operations/api" >}}) for the full
endpoint list and authentication requirements.

## Security Considerations

### Use IDs, not login names

`UserProfile.ID` is a stable numeric identifier scoped to your tailnet. Login
names can change — users rename accounts, switch email providers, migrate SSO
identities, or change GitHub handles. An allowlist keyed on login names would
break on any of these changes.

The YAML comment syntax (`# alice@github`) after each ID is a human-readable
annotation that TSDProxy ignores — it's purely for operator convenience.

### Tagged and shared nodes

TSDProxy rejects tagged device identities. A container running with Tailscale
ACL tags has a pseudo-user profile (`"tagged-devices"`) that could otherwise
appear as a valid identity. The allowlist explicitly excludes tagged nodes.

Nodes shared from another tailnet carry a foreign `UserProfile.ID`. While ID
collisions across tailnets are extremely unlikely, TSDProxy resolves identity
from the connection itself — a foreign user must be on your tailnet to reach
the dashboard through a Tailscale connection.

### Funnel caveat

If a proxy is ever exposed via [Tailscale Funnel]({{< ref "/docs/security/funnel" >}}),
requests arrive from the public internet without a Tailscale identity.
`WhoIs` returns an empty result for funneled requests, and the admin
allowlist rejects them. Admin endpoints must remain behind Tailscale
authentication.

> [!CAUTION]
> Never expose the dashboard through Funnel. Admin endpoints must always
> be accessed through a Tailscale-authenticated connection.

### Defense in depth

Even if your Tailscale ACLs restrict which nodes can reach the dashboard,
keep the in-app allowlist enabled. ACLs and application authorization
should both enforce access — a misconfigured ACL should not silently
grant admin access.

### Reverse proxy header trust

When the dashboard is accessed through the built-in `dash-dev` reverse
proxy, identity is forwarded via the `x-tsdproxy-id` header. The reverse
proxy **strips all client-supplied `x-tsdproxy-*` headers** before setting
the resolved identity values, preventing header injection attacks.
The admin middleware only accepts the `x-tsdproxy-id` header from localhost
connections — the reverse proxy forwards locally within the same process.

## API Key Authentication

For non-Tailscale clients (scripts, CI pipelines, monitoring tools), configure
an API key in `tsdproxy.yaml`:

```yaml {filename="/config/tsdproxy.yaml"}
apiKey: "your-secret-api-key"
# or via file:
# apiKeyFile: "/run/secrets/tsdproxy-api-key"
```

Include the key in requests:

```bash
curl -H "Authorization: Bearer your-secret-api-key" \
  http://localhost:8080/api/v1/proxies
```

API keys grant full admin access to all endpoints. If both `apiKey` and
`apiKeyFile` are set, `apiKeyFile` takes precedence.

> [!CAUTION]
> API keys are equivalent to admin credentials. Store them securely, use
> `apiKeyFile` with Docker secrets or a secrets manager, and rotate regularly.

## Viewer Role

All tailnet users who can reach the dashboard have viewer-level access:

- **View** proxy status, health, uptime, and port configuration
- **Browse** the dashboard with search, filtering, and grouping
- **Manage** their own preferences (dark mode, view layout, sort order, pinned proxies)

Only users in the `admins` list (or authenticated via API key) can:

- Restart, pause, resume, or reauth proxies
- View access logs in the proxy detail modal
- Test webhooks

Non-admin users do not see the Actions or Logs tabs in the proxy detail modal.

## Security Advisory (GHSA-j8rq-87gr-gm9q)

A security vulnerability was fixed that allowed unauthenticated access to
management endpoints. The fix includes:

- **All endpoints now require authentication** — previously, an empty `admins`
  list left all endpoints unprotected. Now every request must present a valid
  Tailscale identity, API key, or come from localhost with `adminAllowLocalhost`
- **Per-process auth token** — prevents `x-tsdproxy-*` header spoofing from
  localhost by generating a random token at startup and validating with
  constant-time comparison
- **Default bind address changed** — `http.hostname` defaults to `127.0.0.1`
  instead of `0.0.0.0`, reducing the attack surface when TSDProxy starts

If upgrading from a previous version, you may need to:

1. Set `http.hostname: 0.0.0.0` if you expose the dashboard externally
2. Configure `admins` list or `apiKey` if you relied on the previously
   unauthenticated access

## Troubleshooting

### "admin access requires a Tailscale connection" (403)

You are accessing an admin endpoint from a non-Tailscale connection
(e.g., direct browser to `localhost:8080`). Access the dashboard through
your Tailscale proxy URL (e.g., `https://dash-dev.<tailnet>.ts.net`).

If you need local access for bootstrapping, temporarily set
`adminAllowLocalhost: true`.

### "access denied" (403)

Your `UserProfile.ID` is not in the `admins` list. Visit `/api/whoami`
through a Tailscale connection to confirm your ID, then add it to the config.

### User avatar not showing in dashboard

If your profile picture doesn't appear, ensure you are accessing the
dashboard through a Tailscale connection — direct TCP access (port 8080
from outside the Tailscale network) cannot resolve user identity, so
the dashboard has no profile information to display.
