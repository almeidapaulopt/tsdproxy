# internal/proxyproviders/tailscale

Tailscale proxy provider: creates `tsnet.Server` instances (per-proxy or shared) that reverse-proxy traffic to Docker containers over a Tailscale network.

## STRUCTURE

| File | Lines | Role |
|------|-------|------|
| `provider.go` | 393 | `Client` factory. `New()` from config, `ResolveAuthKey()` (OAuth token exchange or static key), `NewProxy()` branches to per-proxy or shared mode. Stale state/device cleanup on config mismatch. |
| `proxy.go` | 411 | `Proxy` — per-proxy `tsnet.Server`. `Start()` brings up tsnet, `WatchEvents()` monitors auth/cert status, `GetListener()`/`GetRawTCPListener()` for HTTP/TCP. Funnel support via `tsnet.Funnel`. |
| `shared_proxy.go` | 187 | `SharedProxy` — thin facade over `SharedServer`. `Start()` acquires virtual listeners per HTTPS port, `Close()` releases them. Forwards server events to per-proxy channel. |
| `shared_server.go` | 702 | `SharedServer` — ref-counted tsnet.Server with event-loop state machine. Commands: `acquireCmd`, `releaseCmd`, `closeCmd`. One `SNIRouter` per listened port. Auto-stops tsnet when route count hits zero. |
| `sni_router.go` | 274 | `SNIRouter` — peeks TLS ClientHello bytes to extract SNI hostname, dispatches connection to matching `VirtualListener`. Pure TLS parsing, no stdlib TLS dependency. |
| `virtual_listener.go` | 85 | `VirtualListener` — `net.Listener` backed by a buffered channel. `Dispatch()` is non-blocking; drops connections on full buffer or close. |
| `whois.go` | 44 | Shared helper: resolves Tailscale identity from `local.Client`. Rejects tagged nodes (prevents spoofing by tagged containers). |

## KEY PATTERNS

**Auth resolution chain** (`getAuthkey`): `config.Tailscale.ResolvedAuthKey` → per-proxy `AuthKey` → OAuth token exchange → provider-level `AuthKey` → empty (interactive login). OAuth creates a short-lived key via Tailscale API with tags.

**Shared server state machine**: Single goroutine event loop (`loop()`). Public methods are synchronous wrappers that send typed command structs and wait on reply channels. State lives in `sharedRuntime`, swapped on each start/stop cycle. `sendProducer()` detects dead loop to prevent goroutine leaks.

**SNI routing flow**: `SharedServer` opens one `tsnet.Listener` per port → wraps in `SNIRouter` → each `SharedProxy.Start()` registers its domain → incoming TLS connections peeked for SNI → dispatched to `VirtualListener` → `ProxyManager` reads from it as a standard `net.Listener`.

**Stale state cleanup** (`cleanStaleState`): Compares saved `stateMeta` (ephemeral flag) against current config. Mismatch → removes entire datadir to prevent tsnet getting stuck in `NeedsLogin`.

**Stale device cleanup** (`cleanupStaleDevice`): When `preventDuplicates` is set and OAuth is configured, queries the Tailscale API for offline devices with same hostname and deletes them. Prevents the "-1" suffix duplication problem.

## GOTCHAS

- Shared mode **only supports HTTPS ports**. TCP/HTTP ports are rejected at `newSharedProxy()`. SNI requires TLS ClientHello; there is no way to peek domain on plain TCP.
- `SharedServer` event loop can deadlock if a command handler calls back into `SharedServer`. All public methods must only send a command and wait.
- `VirtualListener` channel buffer is 64. Under extreme load, connections are silently dropped (`Dispatch` returns false). Not an error condition for the router.
- `cleanupStaleDevice` skips devices that are currently online (`ConnectedToControl`). Two tsdproxy instances with same shared hostname will coexist until one goes offline.
- `certSem` (semaphore) is shared across all proxies from the same `Client`. It throttles concurrent TLS cert provisioning to avoid hitting Let's Encrypt rate limits.
- `whoisFromLocalClient` returns zero-value `Whois` for tagged nodes. If you change this, any tagged container in the tailnet can impersonate users on dashboard/admin endpoints.
