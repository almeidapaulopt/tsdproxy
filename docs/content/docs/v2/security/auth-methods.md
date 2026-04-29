---
title: Authentication Methods
prev: /docs/v2/security
weight: 2
---

TSDProxy supports three authentication methods for Tailscale proxies.

## Comparison

| Feature | OAuth | OAuth (Manual) | AuthKey |
|---------|-------|---------------|---------|
| Setup complexity | Medium | Low | Low |
| Requires tags | Yes | No | Optional |
| Auto-renewal | Yes | Manual | Automatic |
| Headless operation | Yes | No | Yes |

## Method 1: OAuth (Recommended)

```yaml
tailscale:
  providers:
    default:
      clientId: "your_client_id"
      clientSecret: "your_client_secret"
      tags: "tag:example"
```

OAuth uses `all:write` scope to automatically authenticate proxies.
Keys are cached at `{dataDir}/{provider}/{hostname}/tsdproxy.yaml`.

## Method 2: OAuth (Manual)

Leave authKey empty. Proxies show "Authenticating" in the Dashboard.
Click to authenticate via Tailscale OAuth flow.

## Method 3: AuthKey

```yaml
tailscale:
  providers:
    default:
      authKey: "tskey-auth-xxxxx"
```

Or via file: `authKeyFile: "/run/secrets/authkey"`

## Provider Resolution Priority

1. `tsdproxy.proxyprovider` label on container
2. `defaultProxyProvider` in target provider config
3. `defaultProxyProvider` at top level
4. First available provider
