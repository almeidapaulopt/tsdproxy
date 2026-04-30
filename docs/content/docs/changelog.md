---
title: Changelog
weight: 500
---

{{% steps %}}

### 2.0.0-beta5

#### New features

- Auto-detect `host.docker.internal` when generating default config
- Support for Docker internal networks via `tryDockerInternalNetwork` config option

#### Fixes

- Fix memory leak: events channel not closed on proxy shutdown, leaking goroutines and object graphs
- Fix SSE streaming: reverse proxy now flushes immediately so Server-Sent Events reach the client
- Fix TCP goroutine leak: port handler connections were not cleaned up on shutdown
- Fix Docker event watcher panic: unbuffered channel sends blocked forever after consumer exit
- Fix dashboard SSE client race: closing message channel caused send-on-closed-channel panics
- Fix redirect ports silently dropped when configured via Docker labels
- Fix containers with no published ports returning error when internal port is known
- Fix OAuth cached key reused across proxies with different tags or ephemeral settings
- Fix healthcheck binary using hardcoded port 8080 — now reads `TSDPROXY_HTTP_PORT` from config
- Fix broken cross-page links in documentation site
- Improve \"invalid key\" error message to mention hardware attestation and expired keys
- Warn when tsnet state is stale (e.g. after changing ephemeral) with actionable guidance
- Tailscale watcher nil-deref on shutdown
- TLS certificate prefetch for faster proxy startup
- Readiness ordering: HTTP server starts only after proxy manager is ready
- Config file watcher survives atomic replacement (e.g., `docker compose cp`)
- Race conditions in proxy lifecycle (start/stop ordering)
- Hardened auth-key file path validation (symlink and non-regular file rejection)

#### Changes

- Migrated to Tailscale v2 client library
- Unified icon download pipeline into reproducible JS script
- Dark mode theme variable renamed internally
- Dependency updates: tailscale.com v1.84.0, OpenTelemetry v1.36.0, templ v0.3.865, Docker client v28.x

### 2.0.0-beta4

#### New features

- Multiple ports in each tailscale hosts
- Enable multiple redirects
- Proxies can use http and https
- OAuth autentication without using the dashboard
- Assign Tags on Tailscale hosts
- Dashboard gets updated in real-time
- Search in the dashboard
- Dashboard proxies are sorted in alphabetically order
- Add support for Docker Swarm stacks
- Tailscale user profile in top-right of Dashboard
- Pass Tailscale identity headers to destination service

#### Breaking changes

- Files provider is now Lists ( key in /config/tsdproxy.yaml changed to
**lists:** instead of files:)
- Lists are now a different yaml file to support multiple ports and redirects,
please [Lists](../v2/providers/lists)

#### Deprecated Docker labels

- tsdproxy.autodetect
- tsdproxy.container_port
- tsdproxy.funnel
- tsdproxy.scheme
- tsdproxy.tlsvalidate

### 1.4.0

#### New features

- OAuth authentication using the Dashboard.
- Dashboard has now proxy status.
- Icons and Labels can be used to customize the Dashboard.

#### Fixes

- Error on port when autodetect is disabled.

### 1.3.0

#### Breaking changes

Configuration files are now validated and doesn't allow invalid configuration keys
[Verify valid configuration keys](../serverconfig/#sample-configuration-file).

#### New features

- Generate TLS certificates for containers when starting proxies.
- Configuration files are now validated.

### 1.2.0

#### New features

Dashboard finally arrived.

### 1.1.2

#### Fixes

Reload Proxy List Files when changes.

#### New features

- Quicker start with different approach to start proxies in docker
- Add support for targets with self-signed certificates.

### 1.1.1

#### New Docker container labels

##### tsdproxy.autodetect

If TSDProxy, for any reason, can't detect the container's network you can
disable it.

##### tsdproxy.scheme

If a container uses https, use tsdproxy.scheme=https label.

### 1.1.0

#### New File Provider

TSDProxy now supports a new file provider. It's useful if you want to proxy URL
without Docker.
Now you can use TSDProxy even without Docker.

### 1.0.0

#### New Autodetection function for containers network

TSDProxy now tries to connect to the container using docker internal
ip addresses and ports. It's more reliable and faster, even in container without
exposed ports.

#### New configuration method

TSDProxy still supports the Environment variable method. But there's much more
power with the new configuration yaml file.

#### Multiple Tailscale servers

TSDProxy now supports multiple Tailscale servers. This option is useful if you
have multiple Tailscale accounts, if you want to group containers with the same
AUTHKEY or if you want to use different servers for different containers.

#### Multiple Docker servers

TSDProxy now supports multiple Docker servers. This option is useful if you have
multiple Docker instances and don't want to deploy and manage TSDProxy on each one.

#### New installation scenarios documentation

Now there is a new  [scenarios]({{< ref "/docs/scenarios/_index.md" >}}) section.

#### New logs

Now logs are more readable and easier to read and with context.

#### New Docker container labels

**tsdproxy.proxyprovider** is the label that defines the Tailscale proxy
provider. It's optional.

#### TSDProxy can now run standalone

With the new configuration file, TSDProxy can be run standalone.
Just run tsdproxyd --config ./config .

#### New flag --config

This new flag allows you to specify a configuration file. It's useful if you
want to use as a command line tool instead of a container.

```bash
tsdproxyd --config ./config/tsdproxy.yaml
```

{{% /steps %}}
