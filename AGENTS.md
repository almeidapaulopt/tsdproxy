# PROJECT KNOWLEDGE BASE

## OVERVIEW

TSDProxy — Go reverse proxy that auto-exposes Docker containers via Tailscale. Labels Docker containers with `tsdproxy.*` to create per-container Tailscale machines with automatic HTTPS. Stack: Go 1.26, templ (UI), Vite/Bun (frontend), Hugo (docs), zerolog (logging).

## STRUCTURE

```
tsdproxy/
├── cmd/
│   ├── server/main.go          # Main server binary (WebApp, InitializeApp)
│   └── healthcheck/main.go     # Docker HEALTHCHECK binary (GET /health/ready/)
├── internal/
│   ├── api/                    # REST API routes (JSON endpoints)
│   ├── config/                 # Config loading, validation, fsnotify file watching
│   ├── consts/                 # Shared constants (headers, proxy manager keys)
│   ├── core/                   # HTTP server, logging, health, sessions, CSRF, version, telemetry
│   │   ├── metrics/            # Prometheus-style metrics
│   │   └── webhook/            # Webhook dispatch on proxy events
│   ├── dashboard/              # SSE dashboard routes + streaming + preferences + API
│   ├── dnsproviders/           # DNS Provider interface + Cloudflare/MagicDNS implementations
│   ├── dom/                    # ID generation utility
│   ├── model/                  # Shared types: Config, PortConfig, ProxyStatus, events
│   ├── proxymanager/           # Central orchestrator: wires target→proxy→DNS→TLS providers
│   ├── proxyproviders/         # ProxyProvider interface + Tailscale (per-proxy & shared)
│   │   └── tailscale/          # Tailscale provider: Proxy, SharedProxy, SharedServer, SNIRouter
│   ├── targetproviders/        # TargetProvider interface + Docker/List implementations
│   │   ├── docker/             # Docker label parsing, container resolution, port mapping
│   │   └── list/               # Static YAML file-based target provider
│   ├── tlsproviders/           # TLS Provider interface + ACME/Tailscale implementations
│   └── ui/                     # templ server-rendered components (proxy cards, pages, layouts)
├── web/                        # Frontend: Vite/Bun + htmx, go:embed dist via statigz+brotli
├── docs/                       # Hugo docs site (separate go.mod: github.com/imfing/hextra-starter-template)
├── dev/                        # Dev docker-compose configs + sample tsdproxy.yaml + data
├── e2e/                        # E2E tests (//go:build e2e, testcontainers + real Tailscale)
└── contrib/                    # Community templates (Unraid)
```

## WHERE TO LOOK

| Task | Location | Notes |
|------|----------|-------|
| Add a new target provider | `internal/targetproviders/` | Implement `TargetProvider` (6 methods) |
| Add a new proxy provider | `internal/proxyproviders/` | Implement `Provider` + `ProxyInterface` (+ optional `RawTCPListener`, `DomainRequiredProvider`) |
| Add a new DNS provider | `internal/dnsproviders/` | Implement `Provider` (4 methods); for ACME also implement `certmagic.DNSProvider` |
| Add a new TLS provider | `internal/tlsproviders/` | Implement `Provider` (4 methods) |
| Change Docker label parsing | `internal/targetproviders/docker/consts.go` | All label constants (`tsdproxy.*`) |
| Change port mapping logic | `internal/targetproviders/docker/container.go` | `getPorts()`, `getTargetURL()` |
| Modify dashboard UI | `internal/ui/pages/proxylist.templ` | templ template for proxy cards |
| Add frontend assets | `web/` | Build with `bun run build`, embedded via go:embed |
| Change config format | `internal/config/config.go` | Struct definitions; `configfile.go` for I/O |
| Add HTTP routes | `internal/dashboard/dash.go` | `AddRoutes()` method |
| Change logging | `internal/core/log.go` | zerolog setup + HTTP middleware |
| Change release process | `.goreleaser.yaml` | Multi-arch Docker, version embedding |
| Tailscale auth flow | `internal/proxyproviders/tailscale/provider.go` | OAuth vs AuthKey resolution |
| Shared Tailscale mode | `internal/proxyproviders/tailscale/shared_server.go` | Ref-counted tsnet.Server, SNI routing |
| DNS record management | `internal/dnsproviders/` | LifecycleManager wraps create/delete/validate with retry |
| TLS certificate provisioning | `internal/tlsproviders/` | LifecycleManager wraps provision/cleanup |
| Add E2E tests | `e2e/` | `//go:build e2e`, real tsdproxy binary + Tailscale + testcontainers |
| Wire new provider into orchestrator | `internal/proxymanager/proxymanager.go` | Add case in `add*Providers()` switch |

## CODE MAP

| Symbol | Type | Location | Role |
|--------|------|----------|------|
| `WebApp` | Struct | `cmd/server/main.go` | Root app container, owns all subsystems |
| `InitializeApp` | Func | `cmd/server/main.go` | Bootstrap: config→logger→HTTP→health→proxy→dashboard |
| `TargetProvider` | Interface | `internal/targetproviders/targetproviders.go` | 6-method contract: WatchEvents, AddTarget, DeleteProxy, ReResolve, Close |
| `Provider` | Interface | `internal/proxyproviders/proxyproviders.go` | Factory: ResolveAuthKey + NewProxy |
| `ProxyInterface` | Interface | `internal/proxyproviders/proxyproviders.go` | Per-proxy: Start, Close, GetListener, GetURL, WatchEvents, Whois |
| `RawTCPListener` | Interface | `internal/proxyproviders/proxyproviders.go` | Optional: GetRawTCPListener for custom TLS termination |
| `DomainRequiredProvider` | Interface | `internal/proxyproviders/proxyproviders.go` | Optional: IsDomainRequired (shared Tailscale needs domains) |
| `dnsproviders.Provider` | Interface | `internal/dnsproviders/dnsproviders.go` | CreateRecord, DeleteRecord, ValidateRecord |
| `tlsproviders.Provider` | Interface | `internal/tlsproviders/tlsproviders.go` | Provision, GetCertificate, Cleanup |
| `ProxyManager` | Struct | `internal/proxymanager/proxymanager.go` | Orchestrator: watches events, manages proxy lifecycle, wires all 4 provider types |
| `Proxy` | Struct | `internal/proxymanager/proxy.go` | Per-container proxy: start/stop/status/ports |
| `SharedServer` | Struct | `internal/proxyproviders/tailscale/shared_server.go` | Ref-counted shared tsnet.Server with event-loop state machine |
| `ServicesServer` | Struct | `internal/proxyproviders/tailscale/services_server.go` | VIP Service-based shared tsnet.Server with event-loop state machine |
| `ServiceProxy` | Struct | `internal/proxyproviders/tailscale/service_proxy.go` | Services mode facade: acquires/releases VIP ServiceListeners |
| `AuthManager` | Struct | `internal/proxyproviders/tailscale/auth_manager.go` | 5-level auth key resolution chain + OAuth key generation |
| `NodeLifecycle` | Struct | `internal/proxyproviders/tailscale/node_lifecycle.go` | Full node lifecycle: startup, state cleanup, device reconciliation, retry |
| `StatusWatcher` | Struct | `internal/proxyproviders/tailscale/status_watcher.go` | Polls tsnet backend state, classifies into ProxyStatus events |
| `DeviceReconciler` | Struct | `internal/proxyproviders/tailscale/device_reconciler.go` | Prevents Tailscale "-1" hostname suffix duplication |
| `StateManager` | Struct | `internal/proxyproviders/tailscale/state_manager.go` | Stale state detection/cleanup via persisted meta comparison |
| `PortRouter` | Struct | `internal/proxyproviders/tailscale/port_router.go` | SNI/HTTP Host routing: TLS ClientHello peeking, domain dispatch |
| `WhoisCache` | Struct | `internal/proxyproviders/tailscale/whois_cache.go` | TTL-based cache + singleflight dedup for Tailscale identity |
| `HTTPServer` | Struct | `internal/core/http.go` | HTTP mux + middleware chain |
| `Config` (global) | Var | `internal/config/config.go` | Singleton config accessed globally (no DI) |
| `Config` (per-proxy) | Struct | `internal/model/proxyconfig.go` | Per-proxy config: hostname, ports, tailscale, dashboard, providers |
| `PortConfig` | Struct | `internal/model/port.go` | Port mapping: target, proxy port, TLS, redirect |
| `Dashboard` | Struct | `internal/dashboard/dash.go` | SSE streaming dashboard |
| `ConfigFile` | Struct | `internal/config/configfile.go` | YAML I/O with fsnotify live-reload |
| `LifecycleManager` (DNS) | Struct | `internal/dnsproviders/lifecycle.go` | SetupDNS/CleanupDNS with retry + status tracking |
| `LifecycleManager` (TLS) | Struct | `internal/tlsproviders/lifecycle.go` | Provision/Cleanup with status tracking |

## ARCHITECTURE

```
Docker containers ──labels──► TargetProvider (Docker/List)
                                    │
                                    ▼
                              ProxyManager ◄── config
                                    │
                          ┌─────────┼─────────┐
                          ▼         ▼         ▼
                   ProxyProvider  DNSProvider  TLSProvider
                   (Tailscale)   (CF/MagicDNS) (ACME/Tailscale)
                          │
                          ▼
                   tsnet.Server (per-proxy or shared)
                          │
                          ▼
                   HTTP/TCP reverse proxy → container port
```

Data flow: TargetProvider watches containers → emits TargetEvent → ProxyManager creates Proxy → resolves ProxyProvider + DNSProvider + TLSProvider → Proxy spins up tsnet.Server → reverse-proxies traffic to container.

Provider resolution per-proxy: `cfg.ProxyProvider` → target provider default → global default. Same cascade for DNS and TLS providers.

### Supported Exposure Modes

1. **Tailscale-only per proxy** — each proxy gets its own Tailscale connection, DNS via MagicDNS, TLS via Tailscale certs.
2. **Per-proxy Tailscale + external DNS/ACME** — own Tailscale connection, hostname via external DNS (Cloudflare), TLS via ACME/Let's Encrypt.
3. **Shared Tailscale + external DNS/ACME** — multiple proxies share one tsnet.Server, each hostname via external DNS + ACME cert. Only HTTPS ports supported (SNI routing requires TLS ClientHello; TCP/HTTP ports rejected at startup).
4. **Services/VIP mode** — multiple proxies share one tsnet.Server using Tailscale VIP Services. Each service gets auto-assigned FQDN from Tailscale. No custom domain support. No UDP support.

Keep all four modes working when changing proxy startup, DNS provisioning, TLS provider selection, or shared Tailscale lifecycle.

### Provider Extensibility

Four parallel provider hierarchies, each with interface at top level and implementations in subdirectories:

- `internal/targetproviders/` → `docker/`, `list/`
- `internal/proxyproviders/` → `tailscale/`
- `internal/dnsproviders/` → `cloudflare/`, `magicdns/`
- `internal/tlsproviders/` → `acme/`, `tailscale/`

Registration is config-driven via switch statements in `ProxyManager.add*Providers()`. Compile-time interface checks: `var _ Interface = (*Impl)(nil)`.

### Target URL Resolution

`getTargetURL()` in `internal/targetproviders/docker/container.go` — protocol-agnostic chain (same for HTTP/TCP/UDP):

1. **resolveSelfHost** — container IS tsdproxy → `127.0.0.1:internalPort`
2. **resolveByProbing** — dial container IPs and gateways (5 retries, 5s sleep)
3. **resolvePublished** — `defaultTargetHostname:publishedPort`
4. **resolveViaGateway** — Docker network gateway + published port (bridge-mode only)
5. **resolveContainerIP** — direct container IP + internal port, last resort (bridge-mode only)

Steps 4–5 skipped for host-network containers.

## CONVENTIONS

- **SPDX headers required**: Every `.go` file must start with `SPDX-FileCopyrightText` + `SPDX-License-Identifier: MIT` (enforced by `goheader` linter)
- **Global config singleton**: `config.Config` accessed directly (not injected)
- **Provider pattern**: Four provider types pluggable via interfaces; register via config-driven switch in `proxymanager.go`
- **Zero-value defaults**: `github.com/creasty/defaults` for struct defaults; `model/default.go` for constants
- **Error handling**: Three-tier: `fmt.Errorf("context: %w", err)` wrapping → sentinel `ErrFoo` vars → custom `XxxError` types
- **Logging**: zerolog with `log.With().Str("key", val).Logger()` for context. `"module"` or `"component"` key. Trace for function boundaries, Debug for lifecycle, Info for state changes, Error with `.Err(err)`.
- **Unit tests**: Co-located `*_test.go` files, run with `go test ./...` (or `make test` using gotestsum)
- **E2E tests**: `e2e/` — `//go:build e2e`, testcontainers + real Tailscale. Env vars: `TSDPROXY_E2E_AUTHKEY`/`TSDPROXY_E2E_AUTHKEY_FILE`, `TSDPROXY_E2E_CLIENTID`/`TSDPROXY_E2E_CLIENTSECRET`, `TS_TAGS`
- **Frontend build**: `web/` uses Bun + Vite; `web/dist/` embedded via `go:embed` + statigz + brotli
- **UI framework**: `templ` for server-rendered HTML; htmx 4 + `hx-sse` for live updates
- **Import aliases**: Descriptive when packages collide: `cloudflaredns`, `magicdns`, `acmetls`, `tailscaletls`, `tsproxy`
- **Import ordering**: Three groups via goimports: stdlib → third-party → project-internal (`github.com/almeidapaulopt/tsdproxy`)
- **nolint convention**: Use specific linter name (`//nolint:gosec`, `//nolint:mnd`) not bare `//nolint`
- **Magic numbers**: Suppressed with `//nolint:mnd` inline rather than named constants (prolific in `model/port.go`, `tailscale/sni_router.go`)

## DASHBOARD STACK

- **Frontend framework:** htmx 4 with `hx-sse` extension. `<hx-partial>` for SSE DOM updates.
- **SSE pattern:** Server sends pre-rendered HTML fragments. `<hx-partial hx-target="..." hx-swap="...">` targets DOM elements server-side.
- **Modals:** Loaded into `#modal-root` outside `#proxy-list` via `hx-get`, decoupled from live list updates.
- **Sorting/filtering/grouping:** Server-side via `hx-get`; returns ready-to-swap HTML.
- **User preferences:** Persisted per Tailscale user as JSON at `{DataDir}/dashboard/preferences/{userID}.json`. Identity key: `ResolveWhois(r).ID`, fallback `__localhost__`. Schema: `dark`, `view`, `sort`, `grouped`, `filterStatus`, `filterHealth`, `pinned`. Search is transient.
- **Proxy actions:** `hx-post` with `hx-swap="none"` — SSE drives state updates.

## ANTI-PATTERNS (THIS PROJECT)

- **Global mutable state**: `config.Config` (package-level var) and `core.proxyAuthToken` — set during init, accessed everywhere without synchronization guarantees
- **nolint directives**: Prolific across codebase; ~half are `//nolint:mnd` magic number suppression (concentrated in `model/port.go`, `tailscale/sni_router.go`); bare `//nolint` without specific linter also present (should specify which)
- **InsecureSkipVerify**: `proxymanager/port.go:74`, `proxymanager/health.go:105` — config-driven TLS validation toggle, uses bare `//nolint`
- **println/fmt.Print in prod**: `cmd/server/main.go`, `internal/core/log.go`, `internal/config/validator.go` — 6 instances using println instead of zerolog
- **Swallowed errors**: `config/generateproviders.go` prints errors but continues; `dashboard/dash.go` discards render errors with `_ =`
- **TODOs in validator**: `internal/config/validator.go` — incomplete per-provider validation
- **Config case sensitivity**: YAML keys are case-sensitive (documented WARNING, no runtime validation)
- **Makefile ldflags mismatch**: Makefile targets `AppVersion`/`BuildDate`/`GitCommit` vars that don't exist in `version.go` (only GoReleaser's `version` var works)
- **`prod` build tag**: Set in GoReleaser but no `//go:build prod` constraints in source — inert

## COMMANDS

```bash
# Development
make dev                    # Start docker containers + assets + server with hot reload (air)
make build                  # Build binary to ./tmp/tsdproxy (ldflags version injection)
make run                    # Build + run (needs make bootstrap first)

# Testing
make test                   # Run all unit tests (gotestsum -race -buildvcs)
make test/cover             # Tests with coverage report
make test/e2e               # E2E tests with gotestsum -tags=e2e (needs TSDPROXY_E2E_AUTHKEY)

# Quality
make audit                  # Full audit: golangci-lint, staticcheck, go vet, deadcode, govulncheck, gosec
make ci                     # Destructive clean rebuild + test (CI equivalent)

# Frontend
cd web && bun run dev       # Vite dev server (proxies to Go backend on :8080)
cd web && bun run build     # Build frontend to web/dist/

# Docker
make docker_image           # Build local Docker image (Dockerfile)
make dev_docker             # Run dev container

# Docs
make docs                   # Hugo docs server (localhost:1313)

# Release (CI)
# Tags v1.* → .goreleaser.yaml (stable, DockerHub + GHCR + Homebrew + AUR + cosign)
# Push to main → .goreleaser-dev.yaml (dev snapshot, draft, Docker images only)
```

## NOTES

- **Version embedding**: GoReleaser ldflags inject `internal/core.version`. Makefile ldflags target wrong vars — local builds always show "dev"
- **Tailscale version injection**: GoReleaser overwrites `tailscale.com/version.*` vars via ldflags to stamp Tailscale with TSDProxy context
- **Config live-reload**: `internal/config/configfile.go` uses fsnotify to watch config file changes
- **Health check**: Separate `healthcheck` binary pings `http://127.0.0.1:8080/health/ready/` — Docker HEALTHCHECK
- **Docker labels**: All container labels start with `tsdproxy.` (see `internal/targetproviders/docker/consts.go`)
- **docs/ is separate Go module**: `github.com/imfing/hextra-starter-template` — `go test ./...` at root ignores it
- **Three Dockerfiles**: `Dockerfile` (local build), `Dockerfile.goreleaser` (CI pre-built binaries), `dev/Dockerfile.dev` (hot-reload dev)
- **Icon pipeline**: `web/scripts/download-icons.js` downloads SVGs from GitHub with SHA256 verification, cached by content-hash
- **Per-target serialization**: ProxyManager uses per-ID mutex (`sync.Map` of `*sync.Mutex`) so start/stop for same container can't interleave
