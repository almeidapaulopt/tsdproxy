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
| `eventloop.go` | `EventLoop[Cmd]` generic abstraction: typed cmds channel + done channel + `atomic.Bool` closed. Send methods: `SendPublic` (public callers), `SendProducer` (bridge/cert goroutines, ctx-aware), `SendCmd` (tests only). `ScheduleIdleTimer` captures the generation counter in its closure so stale timers no-op. Package-level `SendAndWait[Cmd, T]` helper (Go has no generic methods on generic types). Also defines `EventSub`, a `sync.Once`-protected subscriber channel. |

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
| `acl_manager.go` | `ACLManager` — nil-safe (all methods early-return on `m == nil`). `EnsureTags` adds missing `tagOwners` entries, `EnsureFunnelAttribute` adds the `funnel` nodeAttr grant. ETag-aware optimistic concurrency via the Tailscale v2 API (`PolicyFile().Get/Validate/Set`). `NewACLManager` returns nil when no API client is configured, which silently disables auto-provisioning. |

### Test files (30 test files, heavy `t.Parallel()` usage)

| File | Tests |
|------|-------|
| `status_watcher_test.go` | `mockStatusSource` with thread-safe call tracking, BackendState→ProxyStatus classification |
| `services_server_reconcile_test.go` (+ `_test.go`, `_approve_test.go`, `_extra_test.go`) | `mockVIPAPI`, `mockListenerFactory`, event-loop state machine testing via command injection, device approval, edge cases |
| `shared_server_test.go` | SharedServer event loop: acquire/release/close/idle timeout via direct command structs. Defines `mockTSNetServer` (function-field based, implements `TSNetServer`) reused across the package. |
| `device_reconciler_test.go` | `mockDeviceLister` with deleted-ID tracking, online/offline duplicate handling |
| `auth_manager_test.go` (+ `auth_manager_isoauth_test.go`) | `httptest.NewServer` for Tailscale API mock, 5-level auth key resolution chain, OAuth-specific paths |
| `state_manager_test.go` | Filesystem-based: `newStateManager`, `touchStateFile`, `writeMetaFile` |
| `whois_cache_test.go` (+ `whois_cache_evict_test.go`, `whois_test.go`) | TTL cache + singleflight dedup, eviction paths, identity resolution |
| `virtual_listener_test.go` (+ `virtual_listener_dropped_test.go`) | Concurrent dispatch + close safety (goroutine swarm with `atomic` stop flag), drop-on-full behavior |
| `port_router_test.go` (+ `port_router_management_test.go`) | SNI/HTTP Host routing, zero-copy peek, byte replay, listener lifecycle |
| `eventloop_test.go` | Generic `EventLoop` plumbing: `SendPublic`/`SendProducer`/`SendAndWait`, `ScheduleIdleTimer` |
| `exposure_test.go` | All three exposure types: `PerProxyExposure`, `SharedSNIExposure`, `ServicesVIPExposure` |
| `node_lifecycle_test.go` (+ `node_runtime_test.go`) | Lifecycle state machine, runtime handle, context independence |
| `shared_proxy_test.go` (+ `service_proxy_test.go`, `proxy_test.go`, `provider_test.go`) | Facade behaviors across all three modes and provider factory branching |
| `acl_manager_test.go` | `ACLManager` tag/funnel auto-provisioning, nil-safe paths |
| `certs_test.go` (+ `retry_policy_test.go`, `helpers_test.go`, `api_client_test.go`) | Cert conversion, retry policy decisions, tag parsing, API client factory |
| `goleak_test.go` | `TestMain` calling `goleak.VerifyTestMain` to fail on goroutine leaks. Ignores known `net/http.(*http2ClientConn).readLoop` background goroutine via `IgnoreAnyFunction`. |

### Supporting

| File | Role |
|------|------|
| `whois.go` | Resolves Tailscale identity from `local.Client`. Rejects tagged nodes (prevents spoofing). |
| `whois_cache.go` | `WhoisCache` — TTL (30s) + singleflight dedup. |
| `helpers.go` | `cleanTags` utility: comma-separated tag string parsing. |

## TRAFFIC EXPOSURE ABSTRACTION

`exposure.go` centralizes how each mode turns port configs into net listeners. The `Proxy`/`SharedProxy`/`ServiceProxy` facades no longer hold protocol logic directly; they delegate to an exposure instance.

- **`TrafficExposure`** — base interface: `Start(ctx, *NodeRuntime, *model.Config)` + `Close(ctx)`.
- **`ListenerExposure`** — optional: `GetListener(portName)` for HTTP/HTTPS/TCP listeners consumed by the reverse proxy.
- **`RawTCPExposure`** — optional: `GetRawTCPListener(portName)` for direct TCP passthrough.
- **`PacketExposure`** — optional: `GetPacketConn(portName)` for UDP.

Concrete implementations (compile-time assertions live near the bottom of `exposure.go`):

| Type | Interfaces satisfied | Notes |
|------|----------------------|-------|
| `PerProxyExposure` | `TrafficExposure` + all three optional interfaces | One tsnet.Server per container. UDP waits for `TailscaleIPs()` to report a valid v4 before binding (30s timeout, 500ms poll). |
| `SharedSNIExposure` | `TrafficExposure` + `ListenerExposure` only | Backed by `SharedServer.Acquire`. HTTPS wraps `VirtualListener` with TLS termination; cert comes from `CertPairToTLSCertificate` via the shared local client. 5min cert cache (`certCacheTTL`) guarded by a `singleflight.Group` keyed on domain to dedupe concurrent misses. No UDP, no RawTCP passthrough. |
| `ServicesVIPExposure` | `TrafficExposure` + `ListenerExposure` only | Backed by `ServicesServer.Acquire` returning `*tsnet.ServiceListener`. No UDP, no custom domains (FQDN auto-assigned). |

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

## GENERATION COUNTER INVARIANT

Both shared servers (and the per-proxy lifecycle through idle timers) carry a `gen int` on every async result and scheduled command. The loop handler discards any payload whose `gen` does not equal the current runtime's `gen`. This prevents stale `tsnet.Server` results (a watcher tick, a cert-provisioning reply, an idle-timer fire) from corrupting a runtime that was torn down and rebuilt while the payload was in flight.

- Every runtime gets a fresh `gen` on start (monotonic, loop-scoped counter).
- Idle timers capture `gen` in their closure via `EventLoop.ScheduleIdleTimer`; the fired command carries that `gen` so a timer that fires after teardown is dropped silently.
- Bridge goroutines (status watcher, cert provisioning) tag their commands the same way.

## CONTEXT LIFECYCLE

`NodeLifecycle.Start` juggles two distinct contexts:

- **Startup context** (caller-supplied): drives retry loops in `startWithRetry`, the reconcile API call, and any short-lived setup I/O. Cancellation aborts startup.
- **Runtime context** (derived from `context.Background()`): drives the StatusWatcher goroutine and any bridge goroutine that must outlive `Start()` returning. Cancelled only by `NodeLifecycle.Close()` via `rt.Cancel()`.

This split is deliberate: the caller's request context (often an HTTP request or proxy startup) may be cancelled after the node is already running, but the watcher must keep emitting events. Cert provisioning in `certs.go` uses `context.Background()` for the same reason, so Let's Encrypt registration is not interrupted mid-flight when a proxy tears down.

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
- `SendProducer` is MANDATORY for any goroutine that feeds the event loop (bridge, cert provisioning). Raw `el.cmds <- cmd` will deadlock the moment the loop is blocked in teardown waiting on `done`.
- Lifecycle events use a non-blocking send (`select { case ch <- evt: default: warn }` in `NodeLifecycle.sendEvent`). Under burst load with a full 64-deep buffer, events are silently dropped after a warning log line — do not treat the event channel as a reliable audit stream.
- `ACLManager` methods are nil-safe and `NewACLManager` returns nil when no API client is configured. A misconfigured OAuth scope (missing `Policy:Read`/`Policy:Write`) silently disables auto-provisioning with no error at startup; the failure surfaces only when `EnsureTags`/`EnsureFunnelAttribute` first runs.
- `SharedSNIExposure` caches the parsed TLS cert for 5min (`certCacheTTL`). Cert rotation by Tailscale may take up to that window to propagate to new TLS handshakes.
- `ServicesVIPExposure` has no UDP and no custom domains — FQDNs come from Tailscale, and attempting to configure either is rejected at `Start()`.

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
| `autoRemoveConflicts` | bool | `false` | Auto-delete conflicting **online** devices before creating new nodes (requires OAuth). More aggressive than `preventDuplicates`, which only handles offline devices. |
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
