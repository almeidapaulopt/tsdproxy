# internal/targetproviders/docker

Docker target provider: watches Docker daemon for containers with `tsdproxy.*` labels, resolves target URLs via 5-step fallback chain, generates per-proxy config.

## STRUCTURE

| File | Role |
|------|------|
| `docker.go` | `Client` (TargetProvider impl). `WatchEvents()` subscribes to Docker Events API (start/die, filtered by `tsdproxy.enable=true`). `startAllProxies()` initial scan. Swarm support. |
| `container.go` | `container` struct. `getPorts()` label parsing, `getTargetURL()` 5-step resolution chain, `newProxyConfig()` builds per-proxy `model.Config`. |
| `consts.go` | All label constants (`tsdproxy.*` prefix), port option constants, timing constants. |
| `autodetect.go` | Auto-detect probing: `tryConnectContainer`, `tryInternalPort`, `tryPublishedPort`, `dial`. Used in resolveByProbing step. |
| `utils.go` | Label parsing helpers: `getLabelBool`, `getLabelString`, `getLabelInt`, `getAuthKeyFromAuthFile`. |
| `legacy.go` | Legacy label support: `tsdproxy.container_port`, `tsdproxy.scheme`, `tsdproxy.tlsvalidate`, `tsdproxy.funnel`. |
| `errors.go` | Custom error types: `NoValidTargetFoundError`, `ErrNoPortFoundInContainer`. |
| `container_test.go` | Tests covering all resolution strategies. |

## LABEL SCHEMA

All labels prefixed with `tsdproxy.`. Constants in `consts.go`.

**Core**: `enable` (required bool), `name` (hostname), `proxyprovider`, `autodetect`, `auto_restart`, `identity_headers`, `containeraccesslog`.

**Ports**: `tsdproxy.port.<N>` — format: `<proxy_port>/<proxy_protocol>:<target_port>/<target_protocol>[,<option>]`. Options: `no_tlsvalidate`, `tailscale_funnel`, `no_autodetect`. Supports ranges: `2222-2230/tcp:2222-2230/tcp`.

**Tailscale**: `ephemeral`, `runwebclient`, `tsnet_verbose`, `authkey`, `authkeyfile`, `tags`.

**Health**: `health_check_enabled`, `health_check_interval` (1-86400s), `health_check_failures` (1-100), `health_check_cooldown` (0-86400s).

**DNS/TLS**: `domain` (FQDN), `dnsprovider`, `tlsprovider`.

**Dashboard**: `dash.visible`, `dash.label`, `dash.icon`, `dash.category`.

**Legacy**: `container_port`, `scheme`, `tlsvalidate`, `funnel`.

## PORT FORMAT (parsed by `model.NewPortLongLabel`)

| Format | Example | Notes |
|--------|---------|-------|
| Full spec | `443/https:80/http` | Protocol on both sides |
| Port-only | `443:80` | Defaults: https proxy, http target |
| Redirect | `81/http->https://myservice.ts.net` | HTTP redirect to URL |
| Range | `2222-2230/tcp:2222-2230/tcp` | Expands to individual PortConfigs |

## TARGET URL RESOLUTION (5-step fallback in `getTargetURL`)

Protocol-agnostic — identical for HTTP, TCP, UDP.

1. **resolveSelfHost** — container hostname == OS hostname → `127.0.0.1:internalPort`
2. **resolveByProbing** — dial container IPs and gateways (5 retries, 5s sleep). Only if `autodetect=true`.
3. **resolvePublished** — `defaultTargetHostname:publishedPort` (e.g., `host.docker.internal:port`)
4. **resolveViaGateway** — Docker network gateway + published port (bridge-mode only)
5. **resolveContainerIP** — direct container IP + internal port (bridge-mode only, last resort)

Steps 4-5 skipped for host-network containers. IPs/gateways sorted by network name, default bridge preferred.

## EVENT WATCHING

`WatchEvents()` uses Docker Events API with filters:
- `label=tsdproxy.enable=true`
- `type=container`
- `event=start,die`

`start` → `ActionStartProxy`, `die` → `ActionStopProxy`. `startAllProxies()` runs in parallel goroutine for already-running containers.

## GOTCHAS

- Port labels use arbitrary keys (`tsdproxy.port.1`, `tsdproxy.port.web`) — key is just an identifier, not a port number.
- `resolveByProbing` can be slow (5 retries × 5s = 25s worst case). Disabled by default in some configs.
- Host-network containers skip gateway/containerIP resolution — only self-host, probing, and published steps apply.
- Swarm services merge `Service.Endpoint.Ports` with container port bindings.
