---
title: Changelog
prev: /docs/faq
weight: 500
---

{{% steps %}}

### Next

#### New features

- **Services/VIP mode** — new Tailscale exposure mode using VIP Services API. Each proxy gets an auto-assigned FQDN from Tailscale without needing external DNS or TLS providers. Configure with `services: true` on a Tailscale provider. Requires OAuth credentials and a `hostname`. No custom domain or UDP support. See [Services Mode]({{< ref "/docs/v3/advanced/tailscale#services-mode" >}}) for documentation.
- **Shared Tailscale with custom domains** — multiple proxies can now share a single Tailscale connection with SNI routing while using external DNS and ACME TLS providers for custom domains. Configure with `shared: true` on a Tailscale provider. See [Shared Tailscale]({{< ref "/docs/v3/advanced/tailscale#shared-tailscale" >}}) for documentation.
- **`allowContainerFunnel` config option** — operator-controlled gate for Tailscale Funnel. When `false` (default), containers cannot self-enable public internet exposure via the `tailscale_funnel` port option or legacy `tsdproxy.funnel` label. Requests are silently ignored with a warning. See [Server Configuration]({{< ref "/docs/v3/serverconfig#allowcontainerfunnel" >}}) for details.
- **`allowTlsValidateDisable` config option** — operator-controlled gate for TLS validation disable. When `false` (default), containers cannot disable TLS certificate validation via the `no_tlsvalidate` port option or legacy `tsdproxy.tlsvalidate=false` label. See [Server Configuration]({{< ref "/docs/v3/serverconfig#allowtlsvalidatedisable" >}}) for details.
- **`autoApproveDevices` config option** — automatically approve device registration in Tailscale. Useful in services mode where new nodes need approval before starting. Requires OAuth credentials. See [Server Configuration]({{< ref "/docs/v3/serverconfig#tailscale-section" >}}) for details.
- **`authRetry` config block** — configurable retry policy for tsnet startup failures. Controls max attempts, initial backoff, and backoff cap with exponential growth. Non-recoverable errors (invalid tags, expired keys) stop retrying early. See [Server Configuration]({{< ref "/docs/v3/serverconfig#authretry" >}}) for details.
- **`reconcileInterval` config option** — periodic device reconciliation interval. TSDProxy checks for stale devices matching the provider's hostname and tags and removes offline ones. Set to a duration string (e.g. `"5m"`, `"1h"`). Default `"0"` disables periodic reconciliation. See [Server Configuration]({{< ref "/docs/v3/serverconfig#reconcileinterval" >}}) for details.
- **`preventDuplicates` boolean** — the `preventDuplicates` option is now a boolean (`true`/`false`). When set to `true` without OAuth credentials, TSDProxy logs a warning and disables it. The `"auto"` string value is no longer accepted. See [Prevent Duplicate Machines]({{< ref "/docs/v3/advanced/tailscale#prevent-duplicate-machines" >}}) for details.
- **`clientSecretFile` config option** — load the OAuth client secret from a file, supporting Docker secrets and secret managers. Overrides `clientSecret` if both are set.
- **New proxy statuses** — three new proxy lifecycle statuses for improved observability:
  - **`AuthFailed`** — authentication permanently failed (invalid key, bad tags, expired credentials)
  - **`DeviceConflict`** — hostname collision with an existing online Tailscale device
  - **`Reconciling`** — device reconciler is cleaning up stale devices before starting
- **Dashboard filter buttons for new statuses** — filter bar updated with buttons for `AuthFailed`, `DeviceConflict`, and `Reconciling` statuses.
- **`AwaitingApproval` status in dashboard** — dashboard now handles the `AwaitingApproval` status with appropriate UI and API support.
- **Provider change warning** — TSDProxy now warns in logs when a proxy's provider changes during restart, helping detect configuration drift.
- **Rate limiting** — per-IP rate limiting for HTTP proxy ports to protect backends from abusive clients. Configurable per-provider (Docker/Lists) and overridable per-container with Docker labels. See [Rate Limiting]({{< ref "/docs/v3/providers/docker#rate-limiting" >}}) and [Server Configuration]({{< ref "/docs/v3/serverconfig#docker-section" >}}) for details.
  - `rateLimitEnabled` — toggle rate limiting on or off (default: `true`)
  - `rateLimitRps` — max requests per second per client IP (default: `100`, range: 1–10000)
  - `rateLimitBurst` — burst capacity per client IP (default: `200`, range: 1–100000)
  - Per-container Docker labels: `tsdproxy.ratelimit.enabled`, `tsdproxy.ratelimit.rps`, `tsdproxy.ratelimit.burst`
- **`shutdownDrainSeconds` config option** — configurable drain period after health endpoint goes not-ready before HTTP server shutdown. Gives load balancers time to converge. Range: 0–300 seconds. See [Server Configuration]({{< ref "/docs/v3/serverconfig#shutdowndrainseconds" >}}) for details.

#### Fixes

- Fix HTTP port listener fd leak when `close()` runs before `Serve()` registers the listener — now uses `portStartLock` guard consistent with TCP/UDP ports
- Fix TCP listener fd leak when `Accept()` exhausts retries — listener is now explicitly closed on permanent error
- Fix TCP proxy missing idle timeout — connections now have a 5-minute read/write deadline to prevent slowloris-style resource exhaustion
- Fix `keyedLocks` double-unlock silently corrupting an unrelated lock holder — Lock now returns an unlock closure guarded by `sync.Once` with pointer-identity validation
- Fix TCP `io.Copy` errors silently discarded — now logged at Debug for connection debugging
- Fix `extractHost` returning malformed URLs as DNS CNAME targets — now returns an error on parse failure
- Fix `setupDomainForProxy` bare-returning `configureTailscaleTLS` error without context wrapping
- Fix cert expiry metric silently going stale — consecutive parse failures now escalate to Warn after 3 cycles
- Fix `DeleteProxy` error logged at Debug instead of Warn — provider cleanup failures now visible to operators
- Fix inner `provider.WatchEvents` goroutine untracked — now tracked via WaitGroup to prevent goroutine accumulation on reconnect
- Fix `ResolvedAuthKey` stored as plaintext `string` — now uses `secretstring.SecretString` to prevent credential leakage in debug dumps
- Fix O(n) LRU eviction in IP rate limiter — replaced with O(1) `container/list` doubly-linked list
- Fix device reconciler deleting offline exact-match devices on restart even when local state exists
- Fix goroutine, semaphore, and shutdown leaks in Tailscale provider lifecycle
- Fix data races in services server tests
- Fix proxy provider leaks, races, and path traversal vulnerabilities
- Fix lifecycle leaks, context timeouts, and quality improvements in Tailscale provider

#### Changes

- Unified node lifecycle architecture — decomposed the Tailscale provider into lifecycle, auth, state, and status modules with a shared event loop state machine used by both shared and services modes
- TLS certificate caching and deduplication across exposure types
- **Merged healthcheck into server binary** — the separate `cmd/healthcheck` binary is removed. Docker HEALTHCHECK now runs `tsdproxyd healthcheck` (a subcommand of the main binary)
- **Removed `cmd/healthcheck/main.go`** — healthcheck logic is now part of the server binary, eliminating the separate 5.7 MB healthcheck binary from Docker images

### 2.3.0

#### New features

- **`healthCheckEnabled` config option** — per-provider and per-container toggle to completely disable health probes. Set `healthCheckEnabled: false` in Docker or Lists provider config, or use the `tsdproxy.health_check_enabled` Docker label to disable health monitoring for individual containers. Defaults to `true`. See [Health Check]({{< ref "/docs/v3/operations/health-check#configuration" >}}) for details.

### 2.2.0

#### Breaking changes

- **Default `http.hostname` changed from `0.0.0.0` to `127.0.0.1`** — the HTTP dashboard now binds to localhost only by default. If you expose the dashboard externally (e.g. via a reverse proxy or port mapping), set `http.hostname: 0.0.0.0` explicitly. See [GHSA-j8rq-87gr-gm9q](https://github.com/almeidapaulopt/tsdproxy/security/advisories/GHSA-j8rq-87gr-gm9q).
- **All dashboard and API endpoints now require authentication** — previously, an empty `admins` list left endpoints unprotected. Now every request must present a valid Tailscale identity, API key, or come from localhost with `adminAllowLocalhost` enabled. See [Admin Allowlist]({{< ref "/docs/v3/security/admin-allowlist" >}}) for migration instructions.
- **Dashboard migrated from Datastar to htmx 4 + templ** — the frontend framework has been replaced. Custom CSS and JS overrides targeting Datastar internals will need updating. See the [dashboard documentation]({{< ref "/docs/v3/advanced/dashboard" >}}) for the new architecture.

#### New features

- **Viewer/admin dashboard access tiers** — all tailnet users can now view proxy status and preferences (viewer role). Admin actions (restart, pause, resume, reauth) are restricted to users in the `admins` list or authenticated via API key. Non-admin users see a read-only dashboard without the Actions and Logs tabs. See [Admin Allowlist]({{< ref "/docs/v3/security/admin-allowlist#viewer-role" >}}).
- **API key authentication** — new `apiKey` and `apiKeyFile` config options allow non-Tailscale clients (scripts, CI, monitoring tools) to authenticate against the API using an `Authorization: Bearer <key>` header. See [Server Configuration]({{< ref "/docs/v3/serverconfig#api-key-authentication" >}}).
- **`tsdproxy.identity_headers` label** — per-container opt-out from identity header injection (`Remote-User`, `X-Forwarded-User`, `x-tsdproxy-*`). Set `tsdproxy.identity_headers: "false"` for backends that consume these headers in conflicting ways (e.g. wetty). Anti-spoofing header stripping still runs regardless.
- **`tsdproxy.dash.category` label** — group proxies in the dashboard by category. Set `tsdproxy.dash.category: "Production"` to assign a proxy to a category group. See [Dashboard Labels]({{< ref "/docs/v3/providers/docker-reference#dashboard-labels" >}}).
- **Dashboard preferences** — per-user preferences (dark mode, view mode, sort, grouping, filters, pinned proxies) are persisted server-side at `{DataDir}/dashboard/preferences/{userID}.json`. Preferences sync across browser sessions.
- **Dashboard list view** — new true list layout replaces the former compact view. Toggle between grid and list views in the dashboard.
- **Status timeline and uptime display** — each proxy card shows uptime duration and a status change timeline in the detail modal.
- **Browser notifications** — opt-in browser notifications for proxy status changes (Running, Stopped, Error).
- **Version in dashboard footer** — the running TSDProxy version is displayed in the dashboard footer.
- **Prometheus metrics endpoint** — `/metrics` endpoint exposing per-proxy request counters, latency histograms, and proxy status gauges. Protected by admin middleware.
- **Unraid Community Applications support** — official Unraid CA template (`contrib/unraid-template.xml`) for one-click deployment on Unraid servers.
- **htmx 4 migration** — the dashboard frontend has been fully migrated from Datastar to htmx 4 with `hx-sse` extension. Server-sent HTML fragments replace client-side DOM manipulation, reducing frontend complexity and improving maintainability.

#### Fixes

- **Security: GHSA-j8rq-87gr-gm9q** — close unauthenticated access to management endpoints. All API and dashboard routes now require authentication. Prevent `x-tsdproxy-*` header spoofing from localhost with per-process auth token validated via constant-time comparison.
- Fix dashboard errors leaking internal details — errors are now sanitized before being sent to the client
- Fix SSE connections not capped — concurrent SSE connections are now limited to prevent resource exhaustion
- Fix preferences directory traversal — preference file paths are restricted to the configured data directory
- Fix auth token not stripped immediately after reading — tokens are zeroed from memory after validation
- Fix session cookie hardening — improved `Secure` and `HttpOnly` flag handling
- Fix version `isDirty` data race — eliminate race condition in version reporting
- Fix Tailscale OAuth scopes — narrowed to minimum required permissions
- Fix `adminAllowLocalhost` not working with Docker port mapping — the localhost check now also trusts RFC 1918 private networks (Docker bridge IPs), not just loopback
- Fix Docker deployments requiring manual `hostname: 0.0.0.0` — the hostname is now automatically overridden to `0.0.0.0` when running inside a container
- Fix OAuth tag rejection error message — surfaces actionable guidance about OAuth client tag assignment when Tailscale returns a 400
- Fix WatchEvents CPU spin loop — add reconnection backoff when Docker event stream disconnects
- Fix proxy Start/Close race — add mutex for Start/Close exclusion, fix port double-close
- Fix proxy lifecycle ordering — guard metrics writes and fix proxy lifecycle ordering
- Fix SSE subscriber leak — deduplicate SSE refreshes and fix subscriber leak
- Fix TLS cert pre-warming for HTTP-only proxies — skip cert generation when no HTTPS port is configured, add timeout
- Fix health checker idle connections — close idle transport connections on health checker stop
- Fix health check reusing HTTP client — reuse `http.Client` across health checks to avoid connection leak
- Fix proxy status broadcast ordering — install proxy in map before broadcasting status to prevent stale data
- Fix Docker API call timeout — add timeout to Docker daemon API calls
- Fix Docker hostname validation — validate container hostnames before target resolution
- Fix Docker port determinism — fix legacy port selection to be deterministic
- Fix Docker context-aware probing — improve target URL probing with container context
- Fix Docker port option parser — extract and harden port option parsing
- Fix list provider event sends — use non-blocking channel sends to prevent stalled clients blocking the provider
- Fix healthcheck binary IPC — use configurable data directory for port file
- Fix Tailscale URL scheme — derive URL scheme from port proxy protocol (fixes TCP/UDP showing `https://`)
- Fix metrics: capture actual response status and prevent Prometheus series leak
- Fix metrics: add `Hijack()` to status recorder for WebSocket support
- Fix config: correct DNS check logic, improve file permissions and error handling
- Fix UI: handle non-HTTP port URLs in dashboard links
- Fix UI: make footer social icons visible in dark theme
- Fix UI: show pin button for all proxies in list view
- Fix UI: prevent XSS with `textContent` instead of `innerHTML` for toast messages
- Fix UI: remove duplicate inline onclick handlers
- Fix SSE streaming through reverse proxy

#### Fixes

- Fix startup race: DNS setup now waits for the Tailscale proxy URL before creating CNAME records
- Fix tailscale TLS provider unresolvable when configured under a custom name (e.g. `tlsProviders.my-ts-tls.provider: tailscale`)
- Fix ReloadProviders deadlock: provider resolution no longer re-entrant under write lock
- Fix cleanup using stale provider references after config reload — now uses stored providers per proxy
- Fix cleanup using cancelled context — now uses a fresh 30s timeout context
- Fix setupDomainForProxy reading providers without lock
- Fix Cloudflare HTTP client hanging indefinitely — added 30s timeout
- Fix TLS listener not setting MinVersion (now requires TLS 1.2+)
- Fix ValidateProxyConfig rejecting valid configs that use global default providers
- Fix Cloudflare zone detection for multi-part TLDs (e.g. `.co.uk`, `.com.br`)
- Fix Cloudflare query params not URL-encoded
- Fix concurrent map read/write panic on provider reload
- Add DNS/TLS status badge propagation (pending/error/active) in dashboard
- Add ValidateProxyConfig call before domain setup to catch misconfiguration early
- Implement validateProxyProviders — reject config with no Tailscale providers

#### Changes

- Migrated frontend from Datastar to htmx 4 with `hx-sse` extension
- Migrated dashboard server-side rendering to `templ` templates
- Removed Datastar Go and JavaScript dependencies
- Render dashboard icons as inline SVG with `currentColor` for dark theme compatibility
- Removed `pprof` profiling endpoints (was only enabled via `TSDPROXY_PPROF` env var)
- Upgraded tailscale.com from 1.98.0 to 1.98.3
- Upgraded daisyui from 5.5.19 to 5.5.20
- Upgraded golang.org/x/crypto from 0.51.0 to 0.52.0
- E2E tests: scope cleanup to e2e-owned containers, make `adminAllowLocalhost` opt-in
- Removed stale TODOs from config validator

### 2.1.0

#### New features

- **Backend health monitoring with automatic target re-resolution** — when a proxied container restarts and gets a new IP address, TSDProxy now detects the backend failure via configurable health probes and automatically re-resolves the target without restarting the proxy or tearing down the Tailscale connection. The hot-swap is transparent — running connections continue on the old target while new connections use the updated address. Configurable per-provider and per-container with `autoRestart`, `healthCheckInterval`, `healthCheckFailures`, and `healthCheckCooldown` settings. See [Health Check]({{< ref "/docs/v3/operations/health-check#backend-health-monitoring" >}}) for details.

#### Fixes

- Fix permanent 502 errors after container restart — health checker now triggers target re-resolution instead of only reporting status
- Fix UDP port handlers not reflecting target changes after re-resolution
- Fix potential race condition in list provider config reload — diffs are now computed under lock and events emitted after unlock

### 2.0.0

#### Breaking changes

- Files provider renamed to **Lists** (config key changed from `files:` to `lists:`)
- Lists use a new YAML format supporting multiple ports and redirects

#### Deprecated Docker labels

- `tsdproxy.autodetect` — use per-port `no_autodetect` option instead
- `tsdproxy.container_port` — use the new port configuration syntax
- `tsdproxy.funnel` — use `tailscale_funnel` port option instead
- `tsdproxy.scheme` — use the new port configuration syntax
- `tsdproxy.tlsvalidate` — use per-port TLS settings instead

#### New features

- **Multi-port support** — expose multiple ports per container with granular protocol control
- **TCP port forwarding** — proxy raw TCP connections (SSH, databases, gRPC) through Tailscale
- **Tailscale Funnel** — expose services to the public internet via `tailscale_funnel` port option
- **Real-time dashboard** — SSE-powered web UI with live proxy status, search, and alphabetical sorting
- **OAuth authentication** — headless auth without using the dashboard
- **Interactive login** — manual Tailscale authentication when no auth key is configured
- **Tags on Tailscale hosts** — assign Tailscale tags to proxied machines
- **Docker Swarm support** — full support for Docker Swarm stacks
- **HTTP and HTTPS proxy modes** — per-port protocol selection
- **Multiple redirects** — configure multiple HTTP→HTTPS redirects per proxy
- **Tailscale user profile** — displayed in top-right of dashboard
- **Identity headers** — pass Tailscale identity headers (`x-tsdproxy-username`, `x-tsdproxy-displayname`, `x-tsdproxy-profilepicurl`) and standard auth headers (`Remote-User`, `X-Forwarded-User`, `X-Auth-Request-User`) to backend services
- **`no_autodetect` per-port option** — disable autodetection at the port level
- **`preventDuplicates`** — opt-in config to auto-remove stale Tailscale devices before creating new nodes (OAuth only)
- **Auto-detect `host.docker.internal`** — automatically detected when generating default config
- **Docker internal networks** — support via `tryDockerInternalNetwork` config option
- **Live config reload** — configuration changes take effect without restart
- **Health check endpoint** — `/health/ready/` for Docker HEALTHCHECK and orchestrators
- **Backup and restore** — operations for TSDProxy state persistence
- **Standalone deployment** — run as a binary outside Docker

#### Fixes

- Fix identity header spoofing: strip all identity headers from incoming requests before setting TSDProxy headers
- Fix race condition in duplicate-hostname proxy replacement
- Fix proxy manager context leak on zero-ports path causing WatchEvents goroutine leak
- Fix memory leak: events channel not closed on proxy shutdown
- Fix config file watcher: replace `log.Fatal` with error returns, honor context in list provider, fix session cookie `Secure` flag, survive atomic file replacement
- Fix health endpoint returning wrong status codes; buffer template rendering
- Fix dashboard SSE: unique connection IDs, buffer channels, escape template data, prevent send-on-closed-channel races
- Fix config: trim whitespace from auth keys, restrict file permissions, fix list provider reload
- Fix Tailscale: prevent panic on closed channel during shutdown, nil-deref on shutdown
- Fix OAuth single-use auth keys cached and reused after restart causing "Invalid API Key" errors
- Fix OAuth cached key validated against current tags and ephemeral settings
- Fix stale tsnet state auto-recovery on restart and after changing ephemeral flag
- Fix ephemeral nodes leaving stale state on disk causing "node key expired" on reboot
- Fix TCP target scheme: default to matching proxy protocol (tcp→tcp instead of tcp→http)
- Fix TCP goroutine leak: port handler connections not cleaned up on shutdown
- Fix TCP proxy SSE streaming: add `FlushInterval` for immediate event delivery
- Fix legacy label proxy: use HTTP for legacy labels to avoid ACME TLS cert failures on Docker bridge
- Fix Docker networking: extract `getTargetURL` into resolve helpers with deterministic IP selection
- Fix Docker redirect ports silently dropped when configured via labels
- Fix Docker containers with no published ports returning error when internal port is known
- Fix Docker event watcher panic: guard channel sends against consumer exit
- Fix healthcheck binary: use configurable port from `TSDPROXY_HTTP_PORT` instead of hardcoded 8080
- Fix stuck proxy in NeedsLogin state without auth URL — now shows as error in dashboard
- Fix logging: downgrade `NeedsLogin` without auth URL from error to info, suppress expected `context.Canceled` errors
- TLS certificate prefetch for faster proxy startup
- Readiness ordering: HTTP server waits for proxy manager
- Race conditions in proxy lifecycle (start/stop ordering)
- Hardened auth-key file path validation (symlink and non-regular file rejection)
- Improve "invalid key" error messages for auth failures and hardware attestation
- Warn on unrecognized Docker label port options
- Remove redundant `X-Forwarded-For` header copy
- Fix cross-page documentation links (use Hugo `ref` shortcodes)

#### Changes

- Migrated to Tailscale v2 client library
- Migrated Docker client from `docker/docker` to `moby/moby` sub-modules for improved type safety
- Unified icon download pipeline into reproducible JS script
- Upgraded datastar from v0.21.4 to v1.0.1
- Upgraded tailscale.com from v1.84.0 to v1.98.0
- Dependency updates: OpenTelemetry v1.36.0
- Comprehensive documentation overhaul
- Added comprehensive E2E test suite: basic proxy, health endpoints, label parsing, port config, Docker networking, cold-start discovery, TCP, WebSocket, HTTP method forwarding, auth keys, multi-provider, tags, funnel, persistence, reload, and web client tests
- Replaced `go test` with `gotestsum` for all test targets
- CI: bump Hugo version to 0.161.1 for hextra v0.12.2 compatibility

{{% /steps %}}
