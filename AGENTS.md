# PROJECT KNOWLEDGE BASE

**Generated:** 2026-04-27
**Commit:** f4cfbb9
**Branch:** main

## OVERVIEW

TSDProxy — Go reverse proxy that auto-exposes Docker containers via Tailscale. Labels Docker containers with `tsdproxy.*` to create per-container Tailscale machines with automatic HTTPS. Stack: Go 1.26, templ (UI), Vite/Bun (frontend), Hugo (docs), zerolog (logging).

## STRUCTURE

```
tsdproxy/
├── cmd/
│   ├── server/main.go          # Main server binary
│   └── healthcheck/main.go     # Docker HEALTHCHECK binary (GET /health/ready/)
├── internal/
│   ├── config/                 # Config loading, validation, file watching
│   ├── consts/                 # Shared constants
│   ├── core/                   # HTTP server, logging, health, sessions, version
│   ├── dashboard/              # SSE dashboard routes + streaming
│   ├── model/                  # Shared types: Config, PortConfig, ProxyStatus, events
│   ├── proxymanager/           # Central orchestrator: wires target→proxy providers
│   ├── proxyproviders/         # ProxyProvider interface + Tailscale implementation
│   ├── targetproviders/        # TargetProvider interface + Docker/List implementations
│   └── ui/                     # templ-generated UI components (proxy cards)
├── web/                        # Frontend: Vite/Bun, embedded via go:embed dist
├── docs/                       # Hugo docs site (separate go.mod)
├── dev/                        # Dev docker-compose configs + sample tsdproxy.yaml
├── e2e/                        # End-to-end tests (//go:build e2e, requires TS_AUTHKEY)
└── .goreleaser.yaml            # Multi-arch release (DockerHub + GHCR, cosign)
```

## WHERE TO LOOK

| Task | Location | Notes |
|------|----------|-------|
| Add a new target provider | `internal/targetproviders/` | Implement `TargetProvider` interface |
| Add a new proxy provider | `internal/proxyproviders/` | Implement `Provider` + `ProxyInterface` |
| Change Docker label parsing | `internal/targetproviders/docker/consts.go` | All label constants in one file |
| Change port mapping logic | `internal/targetproviders/docker/container.go` | `getPorts()`, `getTargetURL()` |
| Modify dashboard UI | `internal/ui/pages/proxylist.templ` | templ template for proxy cards |
| Add frontend assets | `web/` | Build with `bun run build`, embedded in binary |
| Change config format | `internal/config/config.go` | Struct definitions; `internal/config/configfile.go` for I/O |
| Add HTTP routes | `internal/dashboard/dash.go` | `AddRoutes()` method |
| Change logging | `internal/core/log.go` | zerolog setup + HTTP middleware |
| Change release process | `.goreleaser.yaml` | Multi-arch Docker, version embedding |
| Tailscale auth flow | `internal/proxyproviders/tailscale/provider.go` | OAuth vs AuthKey resolution |
| Add E2E tests | `e2e/` | `//go:build e2e`, uses real tsdproxy binary + Tailscale + Docker containers |

## CODE MAP

| Symbol | Type | Location | Role |
|--------|------|----------|------|
| `WebApp` | Struct | `cmd/server/main.go` | Root app container, owns all subsystems |
| `InitializeApp` | Func | `cmd/server/main.go` | Bootstrap: config→logger→HTTP→health→proxy→dashboard |
| `TargetProvider` | Interface | `internal/targetproviders/targetproviders.go` | Contract for Docker/List providers |
| `Provider` / `ProxyInterface` | Interfaces | `internal/proxyproviders/proxyproviders.go` | Contract for Tailscale proxy provider |
| `ProxyManager` | Struct | `internal/proxymanager/proxymanager.go` | Orchestrator: watches events, manages proxy lifecycle |
| `Proxy` | Struct | `internal/proxymanager/proxy.go` | Per-container proxy: start/stop/status/ports |
| `HTTPServer` | Struct | `internal/core/http.go` | HTTP mux + middleware chain |
| `Config` (global) | Var | `internal/config/config.go` | Singleton config accessed globally |
| `Config` (per-proxy) | Struct | `internal/model/proxyconfig.go` | Per-proxy config: hostname, ports, tailscale, dashboard |
| `PortConfig` | Struct | `internal/model/port.go` | Port mapping: target, proxy port, TLS, redirect |
| `Dashboard` | Struct | `internal/dashboard/dash.go` | SSE streaming dashboard |
| `ConfigFile` | Struct | `internal/config/configfile.go` | YAML I/O with fsnotify live-reload |

## ARCHITECTURE

```
Docker containers ──labels──► TargetProvider (Docker/List)
                                    │
                                    ▼
                              ProxyManager ◄── config
                                    │
                                    ▼
                              ProxyProvider (Tailscale)
                                    │
                                    ▼
                              tsnet.Server (per-container Tailscale node)
                                    │
                                    ▼
                              HTTP reverse proxy → container port
```

Data flow: TargetProvider watches containers → emits TargetEvent → ProxyManager creates Proxy → Proxy spins up Tailscale tsnet.Server → reverse-proxies traffic to container.

### Target URL Resolution

`getTargetURL()` in `internal/targetproviders/docker/container.go` resolves the backend address for a container port. The resolution chain is **protocol-agnostic** — HTTP, TCP, and UDP all follow the same priority order:

1. **resolveSelfHost** — container IS the tsdproxy process → `127.0.0.1:internalPort`
2. **resolveByProbing** — probe connectivity by dialing container IPs and gateways (retries 5×, 5s sleep)
3. **resolvePublished** — `defaultTargetHostname:publishedPort` (or `internalPort` if no published port)
4. **resolveViaGateway** — Docker network gateway + published port (bridge-mode only)
5. **resolveContainerIP** — direct container IP + internal port, last resort (bridge-mode only)

Steps 4–5 are skipped for host-network containers. The chain is intentionally the same for all protocols to avoid resolution discrepancies between e.g. HTTPS and TCP ports on the same container.

## CONVENTIONS

- **SPDX headers required**: Every `.go` file must have `SPDX-FileCopyrightText` + `SPDX-License-Identifier: MIT` (enforced by golangci-lint `goheader`)
- **Global config singleton**: `config.Config` accessed directly (not injected)
- **Provider pattern**: Target providers (source of containers) and proxy providers (Tailscale) are pluggable via interfaces
- **Zero-value defaults**: `github.com/creasty/defaults` for struct defaults; `model/default.go` for constants
- **Error wrapping**: Use `fmt.Errorf("context: %w", err)` consistently
- **Logging**: zerolog with `log.With().Str("key", val).Logger()` for context
- **Unit tests**: `internal/*/` — co-located `*_test.go` files, run with `go test ./...`
- **E2E tests**: `e2e/` — full integration tests using real tsdproxy binary + Tailscale + Docker. Build tag `//go:build e2e`, run with `go test -tags=e2e ./e2e/`. Env vars: `TS_AUTHKEY` or `TS_AUTHKEY_FILE` (auth key), `TS_CLIENT_ID` + `TS_CLIENT_SECRET` (OAuth), `TS_TAGS` (default: `tag:tsdproxy-e2e`).
- **Frontend build**: `web/` uses Bun + Vite; output goes to `web/dist/` which is `//go:embed`-ed into the binary via `statigz` + brotli
- **UI framework**: `templ` for server-rendered components; Vanilla JS + EventSource for SSE real-time updates
- **Authentication**: Uses Tailscale user ID as auth — since access requires a Tailscale connection, the Tailscale identity (user ID) is used to authenticate and identify users. No separate auth system.
- **Identity propagation via localhost headers**: User identity flows through two paths: (1) direct tsnet connections store the Tailscale `Whois` in the request context, (2) the in-process reverse proxy forwards requests to the main HTTP server on localhost and sets `x-tsdproxy-*` headers with the authenticated identity. `StripProxyIdentityHeaders` strips these headers from non-localhost requests, so only the internal proxy can set them. Trusting `x-tsdproxy-*` headers from localhost is safe by design — they are always set by the internal proxy after Tailscale authentication; there is no scenario where a non-Tailscale user causes these headers to be set. The `AdminAllowLocalhost` config controls only whether *unauthenticated* localhost requests (with no identity at all) are permitted on admin endpoints — it does NOT gate header trust.

## ANTI-PATTERNS (THIS PROJECT)

- **TODOs in validator**: `internal/config/validator.go` has `TODO: add validation for each provider` and `TODO: add default proxy provider`
- **nolint directives**: `web/web.go` lines 40-41, `internal/proxymanager/port.go` line 47 (TLS InsecureSkipVerify), `internal/model/port.go` multiple `//nolint:mnd` for magic number suppression
- **Config case sensitivity**: YAML config keys are case-sensitive (documented as WARNING in docs)
- **Global mutable state**: `config.Config` is a package-level var set during init, accessed everywhere

## COMMANDS

```bash
# Development
make dev                    # Start docker containers + assets + server with hot reload
make build                  # Build binary to ./tmp/tsdproxy
make run                    # Build + run

# Testing
make test                   # Run all unit tests
make test/cover             # Tests with coverage
go test -tags=e2e ./e2e/    # Run E2E tests (requires TS_AUTHKEY or TS_AUTHKEY_FILE)

# Quality
make audit                  # Full audit: golangci-lint, staticcheck, go vet, deadcode, govulncheck, gosec

# Frontend
cd web && bun run dev       # Vite dev server for frontend
cd web && bun run build     # Build frontend to web/dist/

# Docker
make docker_image           # Build local Docker image
make dev_docker             # Run dev container

# Docs
make docs                   # Hugo docs server (localhost:1313)

# Release (CI)
# Tags v1.* → .goreleaser.yaml (stable)
# Tags v2.* → .goreleaser-nolatest.yaml (beta)
# Push to main → .goreleaser-dev.yaml (dev snapshot)
```

## NOTES

- **Version embedding**: Build ldflags inject `AppVersion`, `BuildDate`, `GitCommit`, `GitTreeState`, `GoVersion` into `internal/core` vars
- **Config live-reload**: `internal/config/configfile.go` uses fsnotify to watch config file changes
- **Health check**: Separate `healthcheck` binary pings `http://127.0.0.1:8080/health/ready/` — used as Docker HEALTHCHECK
- **Tailscale version injection**: GoReleaser fetches tailscale.com version via `go list -m tailscale.com` and embeds it in binaries
- **Docker labels**: All container labels start with `tsdproxy.` (see `internal/targetproviders/docker/consts.go`)
- **docs/ has separate go.mod**: Hugo docs site is independent Go module (`github.com/imfing/hextra-starter-template`)
- **Makefile `run` target**: Depends on `build/static` which may not exist — use `make dev` instead
