# internal/proxymanager

Central orchestrator. Wires target, proxy, DNS, and TLS providers per container event.

## STRUCTURE

| File | Lines | Purpose |
|------|-------|---------|
| `proxymanager.go` | 938 | `ProxyManager` struct. Provider registration, event loop, per-ID mutex, broadcast, restart/pause/resume, `ReloadProviders()` (dead code — never called) |
| `proxy.go` | 783 | `Proxy` struct per container. Composes 3 providers, start/stop/status/ports, status history ring (last 5), log buffer subscription |
| `port.go` | 545 | Port handlers: `httpProxy`, `httpRedirect`, `tcpForward`, `udpForward`. Reverse proxy, TLS, rate limiting |
| `health.go` | ~120 | `healthChecker`. Periodic HTTP/TCP pings with configurable interval and thresholds |
| `logbuffer.go` | ~100 | Ring buffer for per-proxy log lines. Broadcasts to SSE dashboard subscribers |
| `providers_test.go` | ~200 | Mock provider tests (`mockDNSProvider`, `mockTLSProvider`, `domainRequiredStub`). Includes BUG assertion: per-proxy ACME bypass when no global DNS default. Compile-time interface checks on mocks. |
| `port_test.go` | ~507 | Port handler tests with `startEchoBackend`, `newTestTCPConfig`, header injection tests. Uses stdlib `t.Fatalf` (not testify). |
| `logbuffer_test.go` | ~100 | Ring buffer: concurrent writers, subscriber atomicity, slow consumer drop (goroutine swarm test). |
| `health_test.go` | ~120 | Health checker: probe simulation, `clampDuration` overflow guard. |

## PROVIDER WIRING

### Registration (`Start()`)

```
addTargetProviders()  — config.Docker → docker.New, config.Lists → list.New
addProxyProviders()   — config.Tailscale.Providers → tsproxy.New
addDNSProviders()     — switch cfg.Provider: "cloudflare" | "magicdns"
addTLSProviders()     — switch cfg.Provider: "acme" (needs DNS) | "tailscale" (warns: auto-created)
```

Tailscale TLS provider skips global registration. It gets created inline per proxy in `resolveTLSProviderLocked()`.

### Resolution cascade (per-proxy, `resolveAndSetProviders`)

- **DNS**: `proxyCfg.DNSProvider` → `config.DefaultDNSProvider` → `ErrNoDNSProvider`
- **TLS**: `proxyCfg.TLSProvider` → `config.DefaultTLSProvider` → `ErrNoTLSProvider`
  - Special: `"tailscale"` → `tailscaletls.New(nil)` inline (no map lookup)
  - Special: ACME detected from config → per-proxy ACME instance created with the proxy's own DNS provider (bypasses global registration gap)

### Proxy provider resolution (in `eventStart`)

`proxyCfg.ProxyProvider` → target provider default → `config.DefaultProxyProvider` → `ErrProxyProviderNotFound`

## EVENT LIFECYCLE

```
TargetProvider.WatchEvents() → eventsChan
  → HandleProxyEvent(event)
      getTargetLock(event.ID)     // per-ID mutex from sync.Map
      ActionStartProxy  → eventStart()  → newAndStartProxy()
      ActionStopProxy   → eventStop()   → closeAndRemoveProxy()
      ActionRestartProxy → stop then start
```

`newAndStartProxy`: resolve auth key → `proxyProvider.NewProxy()` → create `Proxy` → `resolveAndSetProviders()` → `setupDomainForProxy()` (DNS create + TLS provision + hostname assignment) → `proxy.Start()` → register in `pm.Proxies`.

`closeAndRemoveProxy`: `proxy.Close()` → DNS cleanup → TLS cleanup → delete from map.

Status changes broadcast via `broadcastStatusEvents()` to all SSE subscribers and webhook sender.

## GOTCHAS

- **Per-proxy ACME bypass**: If `addTLSProviders()` skips ACME (no global DNS default), `resolveAndSetProviders()` creates a per-proxy ACME instance using the proxy's own DNS provider. Tests assert this as a known BUG.
- **`tailscaletls.New(nil)`**: TLS provider created with nil local client first. Replaced with real client after proxy starts in `setupDomainForProxy()`.
- **`InsecureSkipVerify`**: `port.go` uses `//nolint` (bare) for config-driven TLS skip on proxy transport.
- **`waitForProxyURL`**: Polls until tsnet populates the proxy URL (async). Timeout blocks startup.
- **`hostMu`**: Second `sync.Map` of mutexes serializes hostname registration to prevent duplicate Tailscale machines.
- **`clampDuration`**: Prevents `time.NewTicker` panics from negative durations caused by int64 overflow in health check intervals.
- **`ReloadProviders()`**: Method exists but is dead code — not wired to any trigger (fsnotify or API). Main config requires restart.
