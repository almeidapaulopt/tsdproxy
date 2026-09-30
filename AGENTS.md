# TSDProxy - AGENTS.md

## OVERVIEW

- **Go version**: `go 1.26` — require explicit version in `GOVERSION` build arg during releases
- **Frontend build**: Use `bun run build`, result embedded by Go via `go:embed dist`. Do NOT use npm/vite directly in binary builds.
- **E2E tests**: Requires `TS_AUTHKEY` env var; runs against real containers on Tailscale network
- **Config format**: YAML (`tsdproxy.yaml`). See `/internal/config/configfile.go` for I/O handling

## WORKSPACE STRUCTURE

```
tsdproxy/
├── cmd/          # Entry binaries: server, healthcheck
├── internal/     # Backend Go code (main implementation)
│   ├── config/       # Config loading/validation/file watching
│   ├── core/         # HTTP server, zerolog logging
│   ├── dashboard/    # SSE streaming UI endpoints
│   ├── model/        # Shared types
│   ├── proxymanager/ # Orchestrator wiring target→proxy providers
│   └── proxyproviders/# Tailscale proxy provider impls
├── web/            # Frontend assets, embedded in binary via go:embed dist
├── docs/           # Hugo docs (separate go.mod)
├── dev/            # Docker compose configs + sample YAML
├── e2e/            //go:build e2e tests
└── tools/          # Development utilities and scripts
```

## DEVELOPMENT WORKFLOW

**Standard sequence**: `gofmt` → `go vet` → `go test ./...` → `bun run build` (frontend) → typecheck docs

**Linter**: `golangci-lint run --timeout 5m`

**Codegen**: `go generate ./...` (if non-identity scripts exist)

## KEY ENTRYPOINTS

- Main binary: `cmd/server/main.go` (`InitializeApp()` → config→logger→HTTP)
- Config singleton: `internal/config/config.go` (`Config` global var accessed everywhere)
- Docker labels parsed in: `internal/targetproviders/docker/consts.go` (all label constants centralized here)

## ARCHITECTURE FLOW

```
Docker containers ──(tsdproxy.enable label)──► TargetProvider (Docker/List)
                                               │
                                               ▼
                                       ProxyManager ◄── config
                                               │
                                               ▼
                                       ProxyProvider (Tailscale)
                                               │
                                               ▼
                                          tsnet.Server
```

Data flow: Target watch → event emitters → Proxy lifecycle → Tailscale machine → reverse proxy to container.

## IMPORTANT QUirks

1. **Frontend embedding**: `web/` contents embedded from `go.mod` replace in `internal/core/http.go`. Build `dist` folder first via bun before Go embed works.

2. **Port resolution order**: In `getTargetURL()` logic (internal/targetproviders/docker/container.go): self-host → probe (5 retries) → published + gateway → container IP. Same for all protocols (HTTP/TCP/UDP).

3. **Live config reload**: fsnotify watches `/config/tsdproxy.yaml`. Changes trigger automatic Proxy re-initialization without restart.

4. **E2E setup**: `e2e/` tests need running Docker containers with TSDProxy binary + real Tailscale network access (TS_AUTHKEY auth or direct machine keys).
