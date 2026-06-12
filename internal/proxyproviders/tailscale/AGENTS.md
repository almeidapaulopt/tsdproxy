# internal/proxyproviders/tailscale

Tailscale proxy provider: creates `tsnet.Server` instances in three modes (per-proxy, shared SNI, services/VIP) that reverse-proxy traffic to Docker containers over a Tailscale network.

## THREE SERVER MODES

| Mode | Config | Struct | tsnet topology | Domain routing |
|------|--------|--------|----------------|----------------|
| Per-proxy | `shared:false, services:false` | `Proxy` | One tsnet.Server per container | One domain per server |
| Shared SNI | `shared:true` | `SharedServer` + `SharedProxy` | One shared tsnet.Server, ref-counted | SNI (HTTPS) or HTTP Host header |
| Services/VIP | `services:true` | `ServicesServer` + `ServiceProxy` | One shared tsnet.Server with VIP Services | Tailscale-assigned FQDN per service |

## STRUCTURE

### Core factory

| File | Role |
|------|------|
| `provider.go` | `Client` factory. `New()` from config, `ResolveAuthKey()`, `NewProxy()` branches by mode. Stale state/device cleanup on config mismatch. |
| `auth_manager.go` | `AuthManager` — 5-level auth key resolution chain + OAuth key generation via Tailscale API. |
| `api_client.go` | `APIClientFactory` — OAuth-scoped Tailscale API client creation for device reconciliation + key generation. |
| `tsnet_interface.go` | `TSNetServer` interface — tsnet.Server abstraction (10 methods). Satisfied by `*tsnet.Server`. Enables offline testing of listeners, SNI routing, lifecycle. |

### Per-proxy mode

| File | Role |
|------|------|
| `proxy.go` | `Proxy` — per-proxy `tsnet.Server`. `Start()` via NodeLifecycle, `WatchEvents()`, `GetListener()`/`GetRawTCPListener()`/`GetPacketConn()`. Funnel support. |

### Shared SNI mode

| File | Role |
|------|------|
| `shared_server.go` | `SharedServer` — ref-counted tsnet.Server (via `TSNetServer` interface) with event-loop state machine. Commands: acquire, release, close, watchUpdate, certDone, idleTimeout. Auto-stops after 30s idle. |
| `shared_proxy.go` | `SharedProxy` — facade over SharedServer. `Start()` acquires virtual listeners per port, `Close()` releases them. |
| `port_router.go` | `PortRouter` — SNI (TLS ClientHello peek) or HTTP Host header routing to `VirtualListener`. Zero-copy peek for SNI, byte replay for HTTP. |
| `virtual_listener.go` | `VirtualListener` — `net.Listener` backed by buffered channel (cap 64). Non-blocking dispatch, drops on full. |

### Services/VIP mode

| File | Role |
|------|------|
| `services_server.go` | `ServicesServer` — event-loop state machine using Tailscale VIP Services API. Aggregates port specs per service. `autoApproveDevices` option. |
| `service_proxy.go` | `ServiceProxy` — facade over ServicesServer. Acquires/releases VIP `ServiceListener` instances. |

### Node lifecycle (shared across all modes)

| File | Role |
|------|------|
| `node_lifecycle.go` | `NodeLifecycle` — full lifecycle: resolve auth → clean stale state → reconcile devices → create tsnet → start with retry → get LocalClient → start StatusWatcher. |
| `node_runtime.go` | `NodeRuntime` — runtime handle: `TSNetServer` + LocalClient + context + cancel + URL/AuthURL. |
| `node_config.go` | `NodeConfig` — config struct: hostname, datadir, controlURL, tags, mode, ephemeral. |
| `status_watcher.go` | `StatusWatcher` — polls `local.Client.Status()` every 2s, classifies `BackendState` into `ProxyStatus` events. |
| `state_manager.go` | `StateManager` — persists `stateMeta` YAML alongside tsnet state. Compares on startup; mismatch → full datadir removal. |
| `device_reconciler.go` | `DeviceReconciler` — prevents "-1" suffix duplication. Lists devices by tag, deletes offline duplicates matching hostname pattern. |
| `retry_policy.go` | `RetryPolicy` — 3 attempts, exponential backoff, non-recoverable error detection for tsnet startup. |
| `certs.go` | TLS cert provisioning with retry/backoff (10s initial, 2x growth, 5min max, 6 attempts). Throttled by `certSem`. |

### Test files (13 test files, heavy `t.Parallel()` usage)

| File | Tests |
|------|-------|
| `status_watcher_test.go` | `mockStatusSource` with thread-safe call tracking, BackendState→ProxyStatus classification |
| `services_server_reconcile_test.go` | `mockVIPAPI`, `mockListenerFactory`, event-loop state machine testing via command injection |
| `shared_server_test.go` | SharedServer event loop: acquire/release/close/idle timeout via direct command structs |
| `device_reconciler_test.go` | `mockDeviceLister` with deleted-ID tracking, online/offline duplicate handling |
| `auth_manager_test.go` | `httptest.NewServer` for Tailscale API mock, 5-level auth key resolution chain |
| `state_manager_test.go` | Filesystem-based: `newStateManager`, `touchStateFile`, `writeMetaFile` |
| `whois_cache_test.go` | TTL cache + singleflight dedup |
| `virtual_listener_test.go` | Concurrent dispatch + close safety (goroutine swarm with `atomic` stop flag) |
| `port_router_test.go` | SNI/HTTP Host routing, zero-copy peek, byte replay |
| `tsnet_interface_test.go` | `mockTSNetServer` (function-field based, implements `TSNetServer`). Offline listener, error, and value roundtrip tests. |

### Supporting

| File | Role |
|------|------|
| `whois.go` | Resolves Tailscale identity from `local.Client`. Rejects tagged nodes (prevents spoofing). |
| `whois_cache.go` | `WhoisCache` — TTL (30s) + singleflight dedup. |
| `helpers.go` | `cleanTags` utility: comma-separated tag string parsing. |
| `exposure.go` | `TrafficExposure` interface + `ListenerExposure`, `RawTCPExposure`, `PacketExposure` optional interfaces. Concrete types: `PerProxyExposure`, `SharedSNIExposure`, `ServicesVIPExposure`. |

## AUTH KEY RESOLUTION (5-level chain)

`AuthManager.ResolveKey()` precedence:
1. `cfg.Tailscale.ResolvedAuthKey` — cached from previous resolution
2. Per-proxy `cfg.Tailscale.AuthKey` — static key from label/config
3. **OAuth one-time key** — if ClientID+ClientSecret+Tags configured, calls Tailscale API to create short-lived pre-authorized key
4. Provider-level `c.AuthKey` — static key from provider config
5. Empty — triggers interactive login (user visits auth URL)

Per-proxy mode uses full chain. Shared/Services modes skip OAuth in `ResolveAuthKey()` (return static key), resolve auth once during server startup via NodeLifecycle.

## SHARED SERVER EVENT LOOP

`SharedServer.loop()` — single goroutine processes typed command structs:
- States: `sharedIdle` ↔ `sharedRunning`
- Generation counter (`gen`) prevents stale commands from affecting new runtimes
- `sendProducer()` avoids deadlock by checking context/done channel before sending
- First `Acquire()` starts tsnet; last `Release()` triggers 30s idle timer before shutdown

`ServicesServer` follows the same pattern with `servicesCmd` types.

## PORT ROUTING (shared mode)

| Protocol | Listener | Routing | Conflict rules |
|----------|----------|---------|----------------|
| HTTPS | `tsnet.Listen("tcp")` → `PortRouter(RouteSNI)` | TLS ClientHello SNI | Cannot mix SNI + HTTP Host on same port |
| HTTP | `tsnet.Listen("tcp")` → `PortRouter(RouteHTTPHost)` | Host header | Cannot mix with SNI on same port |
| TCP | `tsnet.Listen("tcp")` | Direct (no routing) | One domain per port exclusive |
| UDP | `tsnet.ListenPacket("udp")` | Direct (no routing) | One domain per port exclusive |

## SERVICES/VIP MODE CONSTRAINTS

- No custom domain support — FQDNs auto-assigned by Tailscale
- No UDP support
- Whois uses `X-Forwarded-For` with strict anti-spoofing: single XFF header, single IP, no loopback
- Port specs aggregated per service: all ports sent in one API call

## GOTCHAS

- Shared mode **only supports HTTPS ports for multi-domain routing**. TCP/HTTP ports rejected at `newSharedProxy()`. TCP gets direct listener (one domain per port).
- `SharedServer` event loop can deadlock if a command handler calls back into `SharedServer`. All public methods must only send a command and wait.
- `VirtualListener` channel buffer is 64. Under extreme load, connections silently dropped (`Dispatch` returns false).
- `DeviceReconciler.Reconcile()` skips online devices (`ConnectedToControl`). Two tsdproxy instances with same shared hostname coexist until one goes offline.
- `certSem` shared across all proxies from same `Client`. Throttles concurrent TLS cert provisioning.
- `whoisFromLocalClient` returns zero-value `Whois` for tagged nodes. Changing this allows tagged containers to impersonate users.
- `CleanAuthState()` removes auth files but preserves TLS certificates (avoids Let's Encrypt rate limits).
- RetryPolicy detects non-recoverable errors (e.g., invalid tags) and stops retrying early.

## CONFIGURATION

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `authKey` | string | "" | Static Tailscale auth key |
| `authKeyFile` | string | "" | Path to auth key file |
| `clientId` | string | "" | OAuth client ID for API-backed features |
| `clientSecret` | string | "" | OAuth client secret |
| `clientSecretFile` | string | "" | Path to client secret file |
| `controlUrl` | string | `https://controlplane.tailscale.com` | Tailscale control server URL |
| `tags` | string | "" | Comma-separated ACL tags |
| `hostname` | string | "" | Override hostname for shared/services modes |
| `maxCertConcurrency` | int | 2 | Max concurrent TLS cert provisioning |
| `preventDuplicates` | bool | `false` | Delete stale Tailscale devices before creating new nodes (requires OAuth; warns if enabled without OAuth) |
| `shared` | bool | false | Enable shared SNI mode |
| `services` | bool | false | Enable VIP Services mode |
| `autoApproveDevices` | bool | false | Auto-approve device registration (requires OAuth) |
| `authRetry` | AuthRetryConfig | — | Retry policy for tsnet startup (see below) |
| `reconcileInterval` | duration | `"0"` | Periodic device reconciliation interval (0 = disabled) |

### AuthRetryConfig

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `enabled` | bool | true | Enable retry on tsnet startup failure |
| `maxAttempts` | int | 3 | Maximum retry attempts (1-10) |
| `initialBackoff` | duration | `"2s"` | Initial backoff duration |
| `maxBackoff` | duration | `"30s"` | Maximum backoff cap (exponential growth) |
