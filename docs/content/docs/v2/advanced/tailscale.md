---
title: Tailscale
next: /docs/scenarios
---


This document guides you through the different authentication and configuration
options for Tailscale with TSDProxy.

## Authentication Methods

TSDProxy supports three authentication methods with Tailscale: OAuth,
OAuth (manual), and AuthKey.

### OAuth

{{% steps %}}

#### Prerequisites

1. Generate an OAuth client at [https://login.tailscale.com/admin/settings/oauth](https://login.tailscale.com/admin/settings/oauth).
2. Define tags for services. Tags can be defined in the provider, applying to
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

#### Restart

Restart TSDProxy to apply the changes.

> [!Tip]
> If the proxy fails to authenticate after restarting, check the error logs.
> Ensure the tags are correct and the OAuth client is enabled.

#### Docker Compose with Environment Variables

Instead of mounting a YAML config file, you can pass OAuth credentials via
environment variables:

```yaml {filename="docker-compose.yaml"}
services:
  tsdproxy:
    image: almeidapaulopt/tsdproxy:latest
    environment:
      - TSDPROXY_TAILSCALE_PROVIDERS_DEFAULT_CLIENTID=${TS_CLIENTID}
      - TSDPROXY_TAILSCALE_PROVIDERS_DEFAULT_CLIENTSECRET=${TS_SECRET}
      - TSDPROXY_TAILSCALE_PROVIDERS_DEFAULT_TAGS=tag:tsdproxy
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
      - data:/data
    ports:
      - 8080:8080
volumes:
  data:
```

> [!Tip]
> See [Environment Variable Overrides](../../serverconfig/#environment-variable-overrides)
> for the full naming convention.

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

## Prevent Duplicate Machines

When TSDProxy restarts and the data directory has been lost (e.g. non-persistent
Docker volume), Tailscale creates a new machine instead of reconnecting the
existing one. This results in duplicate machines in your tailnet, often with a
`-1` suffix.

The `preventDuplicates` option (default: `false`) tells TSDProxy to query the
Tailscale API before creating a new node. If an existing device with the same
hostname and matching tags is found **and is offline**, it is deleted first so
the new node can take its place.

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

### Safety checks

A device is only deleted when **all** of these conditions are true:

- It has the same hostname as the proxy being created
- It has matching tags
- It is currently offline (`ConnectedToControl` is false)
- The local tsnet state file is missing (no existing identity to reuse)

Online devices are never deleted.

## Identity Headers

TSDProxy forwards Tailscale identity via HTTP headers: `X-Tailscale-User`, `X-Tailscale-Name`, `X-Tailscale-Profile-Picture`, `X-Forwarded-For`.

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
| Authenticating | Waiting for auth |
| Running | Active |
| Stopping | Shutting down |
| Stopped | Removed |
| Error | Fatal error |
