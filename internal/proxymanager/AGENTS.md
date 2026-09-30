# internal/proxymanager

Central orchestrator. Wires target, proxy, DNS, and TLS providers per container event. Owns proxy lifecycle, port handlers, health probes, domain setup, and SSE status broadcast.

## STRUCTURE

| File | Lines | Purpose |
|------|-------|---------|
| `proxymanager.go` | 477 | `ProxyManager` struct. Three-lock split (RF-2). Primitives: `withCurrentProxy`, `removeAndTeardown`, `teardownProxy`, `closeProxyIfStillCurrent`. `registerProxy` does `setupWg.Add(1)` before map insert (Add/Wait race prevention). Provider registration, `StopAllProxies` two-tier shutdown. |
| `proxy.go` | 638 | `Proxy` struct per container. Two-lock hierarchy (`opMu` then `mtx`). Two-phase `Start`, `closeOnce`-guarded `Close`, `Pause`/`Resume`. `setStatus` ring buffer capped at last 5 transitions. |
| `events.go` | 218 | `WatchEvents` with exponential backoff (`maxWatchBackoff` 5m, `backoffMultiplier` 2). `dispatchProxyEvent` spawns `eventHandlerWg`-tracked goroutine. `HandleProxyEvent` does per-ID serialized entry via `targetLocks`. |
| `providers.go` | 288 | `addProxyProviders`/`addTargetProviders`/`addDNSProviders`/`addTLSProviders` registration. `resolveAndSetProviders` cascade. Per-proxy ACME is intentional design, not a bug. |
| `domain.go` | 349 | `configureProxyDomain`/`prepareDomainSetup`/`setupDomainForProxy` chain. Cert expiry tracker goroutine with `certTrackerStop`/`certTrackerDone` channels. DNS rollback on TLS provision failure. |
| `health.go` | 553 | `healthChecker`. HTTP/TCP probes with cooldown backoff and re-detect callback. `atomic.Value`/`atomic.Pointer[HealthResult]` for lock-free target and result reads. `clampDuration` overflow guard. |
| `port_http.go` | 440 | HTTP/HTTPS reverse proxy and redirect handler. Rate-limit and whois middleware, access log buffer. Header stripping for GHSA-pqg7-v6wh-3pfp. |
| `port_tcp.go` | 251 | TCP forwarder. Uses `portStartLock` for start-vs-close atomicity. |
| `port_udp.go` | 369 | UDP forwarder. Backend dial outside the client-map lock. |
| `proxyports.go` | 167 | `initPorts` factory dispatching on `IsRedirect`/`ProxyProtocol`. `startProvider` + `startListeners`. |
| `proxytls.go` | 78 | Custom-domain TLS listener via `RawTCPListener`. Cert lookup with Tailscale fallback when custom cert unavailable. |
| `locks.go` | 94 | `keyedLocks`: ref-counted per-key mutex with auto-cleanup, pointer-identity unlock check, `sync.Once`-wrapped unlock closure. Replaces `sync.Map` of `*sync.Mutex`. |
| `ratelimit.go` | 120 | Per-IP LRU-bounded rate limiter. |
| `logbuffer.go` | 152 | Ring buffer for access logs. SSE fan-out to dashboard subscribers. |
| `broadcast.go` | 58 | `SubscribeStatusEvents`/`broadcastStatusEvents`. Buffered chan (64), drop-on-full policy. Also dispatches webhook events. |
| `port.go` | 38 | `portHandler` interface. `portStartLock` helper for ctx-check-then-field-assign atomicity. |

Test files (~7,300 LOC across 14 files). Notable: `concurrency_test.go` (RF-2 race regressions), `domain_bug_test.go` + `lifecycle_bug_test.go` (regression guards), `goleak_test.go` (goroutine leak verification). Remaining cover per-file unit tests; `testhelpers_test.go` provides shared factories.

## LOCK HIERARCHY

`ProxyManager` three-lock split (RF-2 redesign): `proxyMu` guards `Proxies` and `targetIndex`; `providerMu` guards the four provider maps; `subMu` guards `statusSubscribers`. Rule: no method acquires more than one of these locks simultaneously, eliminating lock-ordering deadlocks between struct locks.

Keyed locks first, then struct locks. `targetLocks` and `hostLocks` (both `*keyedLocks`) are always acquired BEFORE `proxyMu`/`providerMu`/`subMu` when both are needed. Consistent across all call sites and never reversed, so no deadlock is possible between keyed and struct locks.

`Proxy` two-lock hierarchy: `opMu` (coarse lifecycle serialization, held for all of Start/Close/Pause/Resume) BEFORE `mtx` (fine-grained state RWMutex). When both are needed, `opMu` is acquired first. Documented in the `Proxy` doc comment.

`keyedLocks` invariant: `Lock` returns a `sync.Once`-wrapped unlock closure, eliminating double-unlock. On unlock, pointer identity is checked against the current map entry. If the entry was deleted and recreated (stale unlock from a previous generation, only reachable if a caller bypasses the Once guard), the call logs a stack trace to stderr and returns without corrupting the new holder.

## EVENT LIFECYCLE

```
TargetProvider.WatchEvents → eventsChan → dispatchProxyEvent
  → spawns eventHandlerWg-tracked goroutine → HandleProxyEvent
  → targetLocks.Lock(event.ID) → switch Action → eventStart / eventStop
```

`Start()` runs OUTSIDE the target lock. After releasing it, `HandleProxyEvent` re-checks pointer identity via `GetProxy` before calling `Start`, closing the window where a concurrent stop could have removed and closed the proxy.

`eventStop` acquires `hostLocks` for the hostname BEFORE `removeAndTeardown`, preventing a concurrent `restartProxyLocked` from inserting a new proxy whose DNS/TLS resources get destroyed by this cleanup.

`newProxy` (called by `eventStart` and `restartProxyLocked`) holds `hostLocks` for the hostname across the whole build. It calls `closeAndRemoveProxy` to tear down any existing proxy at the same hostname, then `buildProxy` then `registerProxy` then `configureProxyDomain`. `Start` is the caller's responsibility, run after the target lock releases.

Restart uses exponential backoff capped at `maxWatchBackoff` (5 minutes, `backoffMultiplier` 2). On stream close or error, `reconnectBackoff` sleeps then reconnects the provider.

## PROVIDER WIRING

Registration order in `Start()`:

```
addProxyProviders():  cfg.Tailscale.Providers → tsproxy.New (per-proxy, shared, services)
addTargetProviders(): cfg.Docker → docker.New, cfg.Lists → list.New
addDNSProviders():    switch cfg.Provider: "cloudflare" | "magicdns"
addTLSProviders():    Tailscale and ACME both warn and skip global registration
```

Both Tailscale and ACME TLS providers are auto-created per proxy, never globally registered.

Resolution cascade (from `resolveAndSetProviders`, called by `prepareDomainSetup`):
- DNS: `proxyCfg.DNSProvider` then `cfg.DefaultDNSProvider` then `ErrNoDNSProvider`.
- TLS: `proxyCfg.TLSProvider` then `cfg.DefaultTLSProvider` then `ErrNoTLSProvider`.
- Special `"tailscale"`: `tailscaletls.New(nil, 0)` inline, no map lookup.
- Special ACME: per-proxy instance bound to the proxy's own resolved DNS provider, ensuring DNS-01 challenges target the correct zone. Intentional design in `resolveTLSProviderLocked`, not a bug.

Proxy provider resolution (in `eventStart` via `getProxyProvider`): `proxyCfg.ProxyProvider` then target provider default then `cfg.DefaultProxyProvider` then `ErrProxyProviderNotFound`.

Tailscale TLS provider is NOT globally registered. Created inline per proxy.

## PROXY LIFECYCLE

`Start()` is two-phase. Phase 1 under `opMu`: `startProvider` (tsnet server + watch-status goroutine), `startHealthChecker`, event watcher goroutine, then `opMu` released. Phase 2: `startListeners` may block on interactive Tailscale login (tsnet `Up`). Releasing `opMu` between phases lets `Close()` proceed while login is pending.

`Close()` is `closeOnce`-guarded. It does NOT clean DNS/TLS or stop the cert tracker. That is `teardownProxy`'s job. All production removal paths go through `teardownProxy` or `removeAndTeardown`.

`teardownProxy` sequence (order matters): `cancelCtx` then `setupWg.Wait` then `stopCertTracker` then `cleanupDomainForProxy` then `p.Close` then `closeTLSProvider` then `cleanupProxyMetrics`. Each step gates the next: setupWg.Wait guarantees the cert-tracker channels are initialized before stopCertTracker reads them; stopCertTracker gates cleanupDomainForProxy so no concurrent `GetCertificate` races TLS teardown.

`Pause()` stops port listeners and health checker, keeps the provider proxy (tsnet) alive, sets status Paused. `Resume()` rebuilds port handlers and restarts listeners. If all listeners error on resume, the proxy re-pauses to avoid a zombie state (paused false + no listeners + no health).

`setStatus()` appends to a ring buffer capped at 5 transitions. While paused, it blocks status updates from provider events except the internal Paused/Running transitions.

## CONCURRENCY PRIMITIVES

- `setupWg` Add/Wait race prevention: `registerProxy` calls `setupWg.Add(1)` BEFORE inserting into the map, so `teardownProxy`'s `setupWg.Wait()` cannot return before `configureProxyDomain`'s paired `Done()`.
- `portStartLock`: ctx-check then field-assign then `wg.Add` then `started.Store`, all under `mtx`. Shared by TCP and UDP start paths.
- Cert tracker: `certTrackerStop` is closed BEFORE TLS cleanup in `teardownProxy`, preventing concurrent `GetCertificate` calls during teardown. `setupWg.Wait()` precedes it so the channels are guaranteed initialized.
- `eventsWg` + `eventHandlerWg` two-tier shutdown: `StopAllProxies` waits on `eventsWg` (WatchEvents goroutines) then `eventHandlerWg` (per-event handlers).
- `stopping` `atomic.Bool`: belt-and-suspenders guard checked in `dispatchProxyEvent`, `newProxy`, and `registerProxy`.
- Non-blocking broadcast: `broadcastStatusEvents` uses `select` with `default`, dropping events when a subscriber channel is full (slow consumer).
- `urlReady` channel + `urlOnce`: `waitForProxyURL` polls or waits for the single-shot signal that the proxy URL is populated.
- Async domain setup: `configureProxyDomain` launches a goroutine for DNS/TLS provisioning after synchronous provider resolution in `prepareDomainSetup`.

## GOTCHAS

- NEVER call `proxy.Close()` in production paths. Use `teardownProxy` or `removeAndTeardown`. `Close` alone leaks DNS records, TLS certs, and the cert tracker.
- Keyed-before-struct lock order is mandatory. Acquiring a struct lock then a keyed lock deadlocks.
- `opMu` before `mtx` on `Proxy`. Acquiring `mtx` without `opMu` when both are needed is forbidden.
- Per-proxy ACME is intentional design. Tests previously framed it as a BUG; that framing is obsolete.
- `tailscaletls.New(nil, 0)`: created with nil local client. `configureTailscaleTLS` calls `SetLocalClient` after the proxy starts, and `UpdateLocalClient` semantics apply per the tailscale TLS provider.
- `hostLocks` acquired BEFORE the identity check in `closeProxyIfStillCurrent`, closing the TOCTOU window between check and teardown.
- `clampDuration` prevents `time.NewTicker` panic from negative durations caused by int64 overflow in health check intervals.
- `removeAndTeardown` callers that already hold `hostLocks` (`eventStop`, `closeProxyIfStillCurrent`) must NOT reacquire it.
