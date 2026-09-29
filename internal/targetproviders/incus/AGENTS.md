# internal/targetproviders/incus

Incus target provider: watches an Incus daemon (containers AND VMs) for instances with `user.tsdproxy.*` config keys, resolves target URLs from instance state addresses, generates per-proxy config.

## STRUCTURE

| File | Role |
|------|------|
| `incus.go` | `Client` (TargetProvider impl). `WatchEvents()` subscribes to lifecycle events via `GetEventsByType(["lifecycle"])`. `handleEvent` maps actions to target events. `buildProxyConfig` + `waitForInstanceState` (polls for global address after start, DHCP delay). `handleInstanceUpdated` compares `user.tsdproxy.*` keys for live config reload. |
| `incus_client.go` | `APIClient` + `EventListener` interfaces — Incus SDK abstractions. `serverClient` adapter wraps `incusclient.InstanceServer` (SDK's `GetEventsByType` returns concrete `*EventListener`, hence the adapter). `connect()` handles unix socket vs remote HTTPS + TLS material + project selection. |
| `instance.go` | `instance` struct. `setInstanceNetwork` (eth0-first, IPv4-first, global-scope only), `getPorts` (same label format as Docker), `getTargetURL` (targetHostname override or first instance IP), `newProxyConfig` builds `model.Config`. |
| `consts.go` | All `user.tsdproxy.*` config key constants, timing constants, port options. |
| `utils.go` | Config parsing helpers: `getConfigBool/String/Int`, `getAuthKeyFromAuthFile` (secretstring), `instanceEnabled`. |
| `errors.go` | Sentinel errors: `ErrInstanceNotEnabled`, `ErrInstanceNotRunning`, `ErrNoAddressFound`, `ErrSocketAndURL`, `ErrTLSClientMaterial`. |
| `incus_test.go` | Unit tests with `mockAPIClient` (implements APIClient). Covers IP selection, target URL, config mapping, event classification, update diffing. |
| `goleak_test.go` | `goleak.VerifyTestMain` + shared `testAssets`. |

## CONFIG KEY SCHEMA

Incus reserves `user.*` for free-form keys; all tsdproxy keys are prefixed `user.tsdproxy.`. Same names as Docker labels after the prefix swap (`user.tsdproxy.port.<N>`, `user.tsdproxy.dash.icon`, `user.tsdproxy.ratelimit.enabled`, ...). NOT supported (Docker-only concepts): `autodetect`, `no_autodetect` port option, legacy keys (`container_port`, `scheme`, `tlsvalidate`, `funnel`).

## KEY DESIGN DECISIONS

- **No server-side event filtering**: Incus can't filter events by config key (unlike Docker label filters). Each lifecycle event triggers a `GetInstance` fetch; non-enabled instances are dropped silently — never emit events for them or ProxyManager error-logs spam.
- **instance-updated ≠ blind restart**: update events fire on ANY config/device change (volatile.* churn is frequent). `tsdproxyConfigChanged` compares only `user.tsdproxy.*` subsets before emitting Restart. Newly enabled → Start, newly disabled → Stop.
- **instance-deleted uses tracking**: config is gone after deletion, so `emitIfTracked` (the provider's instances map, populated by AddTarget) decides whether to emit Stop.
- **instance-stopped checks config**: stopped instances keep their config, so the enable check still works for stop events.
- **freeze/unfreeze maps to stop/start**: `instance-paused` emits Stop (frozen processes can't serve; config stays readable for the enable check), `instance-resumed` emits Start. Resume is also the only re-entry point for instances frozen while tsdproxy was down (initial scan skips non-Running instances).
- **State polling after start**: `waitForInstanceState` polls up to 5×2s for a global address (cloud-init/DHCP delay when instance-started fires).
- **Target resolution** (no published ports in Incus): `targetHostname` provider config with target port → else first global IPv4 (IPv6 fallback, `net.JoinHostPort` for brackets), eth0 preferred.

## GOTCHAS

- `api.Instance.Config` is a promoted field from embedded `InstancePut` — struct literals with it require go1.27; tests use `inst.Config = ...` assignment instead (see `testAPIInstance`).
- `incusapi.Instance.Type` is a plain string, NOT `incusapi.InstanceType`.
- Events MUST be consumed via `AddChannel` (v7), never `AddHandler`: handler callbacks fire in unordered per-event goroutines (`go target.function(event)`), which can invert rapid stop/start sequences for the same instance. The channel delivers FIFO in a single serial loop and auto-closes when the listener ends — that close replaces a dedicated `Wait()` goroutine for disconnect detection.
- `Close()` calls both `listener.Disconnect()` and `incus.Disconnect()` (v7 `InstanceServer.Disconnect` kills SDK background goroutines).
- `ErrStreamDisconnected` on clean listener exit feeds the ProxyManager reconnect loop — same contract as the Docker provider.
- Sentinels from `targetproviders`: `DeleteProxy` wraps `ErrTargetNotFound`; stream errors wrap `ErrStreamDisconnected`.
- Remote HTTPS requires cert+key as a pair; socket + url are mutually exclusive (validated in `connect`).
