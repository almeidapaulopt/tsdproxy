# internal/core

Foundational infrastructure: HTTP server, middleware chain, logging, health probes, auth/RBAC, sessions, CSRF, version, telemetry, metrics, webhooks.

## STRUCTURE

| File | Role |
|------|------|
| `http.go` | `HTTPServer` — mux wrapper with middleware chain. `Use()` adds middleware, `Handle/Get/Post/Put` register routes, `StartServer` binds and serves. JSON/error response helpers. |
| `log.go` | zerolog setup: `NewLog()` configures level (from config), format (JSON vs console), caller info. `LoggerMiddleware` for HTTP access logging. |
| `healthcheck.go` | `HealthHandler` — atomic ready/not-ready flag. Registers `GET /health/ready/` and `GET /health/live/`. Readiness set after HTTP server starts. |
| `version.go` | `GetVersion()` via `sync.Once`. GoReleaser ldflags inject `version` var; local builds get `"dev"` + dirty detection from `debug.ReadBuildInfo()`. |
| `admin.go` | `InitProxyAuth()` generates 32-byte random token. `AdminMiddleware`/`ViewerMiddleware` for RBAC. `AdminAllowList` for IP-based access. `StripProxyIdentityHeaders` validates/removes `x-tsdproxy-*` headers. |
| `sessions.go` | `SessionMiddleware` — UUID session cookie via `gorilla/sessions`. Cookie-based, no server-side store. |
| `csrf.go` | `CSRFMiddleware` — Go 1.25+ `http.CrossOriginProtection`. |
| `telemetry.go` | `InitTracer()` — optional OpenTelemetry tracer (config-driven). |
| `metrics/` | Prometheus-style metrics: proxy count, port count, status gauges. |
| `webhook/` | Webhook dispatch on proxy events: ntfy, Discord, Slack, Gotify, generic HTTP. Async with retry. |
| `secretstring/` | `SecretString` type — prevents logging/serialization of secret values. |

## MIDDLEWARE CHAIN (applied globally on HTTPServer)

```
StripProxyIdentityHeaders  →  SessionMiddleware  →  CSRFMiddleware
```

Applied via `HTTPServer.Use()` in `InitializeApp()`. Route-level auth (`AdminMiddleware`, `ViewerMiddleware`) applied per-handler in `AddRoutes()`.

## HTTP SERVER LIFECYCLE

```
NewHTTPServer(log)         → creates mux
HTTPServer.Use(mw...)      → adds middleware to chain
HTTPServer.Handle(pattern, handler) → registers with wrapped handler
HTTPServer.StartServer(ln) → http.Server.Serve(ln)
                            → health.SetReady() on success
Shutdown(ctx, 10s timeout) → health.SetNotReady() → server.Shutdown()
```

## AUTH FLOW

- `InitProxyAuth()` generates `proxyAuthToken` (package-level var, set once during init)
- `AdminMiddleware` checks session for admin role
- `ViewerMiddleware` checks session for viewer role
- `AdminAllowList` restricts admin access by IP (config: `adminAllowLocalhost`, `adminAllowList`)
- `StripProxyIdentityHeaders` removes `x-tsdproxy-*` identity headers from incoming requests (prevents spoofing)

## GOTCHAS

- **`proxyAuthToken` is package-level mutable**: set in `InitProxyAuth()`, accessed by middleware. No sync primitive beyond single-goroutine init sequence.
- **`fmt.Fprintf(os.Stderr, ...)` in `cmd/server/main.go`**: uses fmt instead of zerolog because logger doesn't exist yet. Acceptable for pre-logger messages.
