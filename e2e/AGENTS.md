# E2E Tests

## OVERVIEW

Integration tests against real Tailscale infrastructure. All gated behind `//go:build e2e`. Tests spin up actual tsdproxy binaries, real Docker containers via testcontainers-go, and real tsnet connections.

## STRUCTURE

```
e2e/
├── e2e_test.go     # TestMain: build binary, clean containers, run suite
├── helpers.go      # TSDProxyInstance, TSNetClient, config gen, testcontainers wrappers
└── *_test.go — one file per scenario, grouped by theme:
    Lifecycle:     basic, persistence, reload, discovery, duplicates
    Networking:    network, custom_network, websocket
    Ports & TCP:   ports, tcp, tcp_advanced, ssh
    Funnel:        funnel
    Health:        health, health_liveness
    Labels & auth: labels, tags, authkey_labels, providers, multiprovider, methods, runwebclient
    Shared mode:   shared
    Negative:      negative (missing labels, no ports, invalid targets)
```

## RUNNING

```bash
make test/e2e                      # gotestsum -tags=e2e -timeout=0
go test -tags=e2e -timeout=0 ./e2e/
TSDPROXY_E2E_AUTHKEY=tskey-auth-xxx go test -tags=e2e -run TestBasicProxy ./e2e/
```

Required: `TSDPROXY_E2E_AUTHKEY` or `TSDPROXY_E2E_AUTHKEY_FILE`. Optional: `TSDPROXY_E2E_CLIENTID` + `TSDPROXY_E2E_CLIENTSECRET` (OAuth tests), `TS_TAGS` (default: `tag:tsdproxy-e2e`).

## TEST INFRASTRUCTURE

**TestMain** (`e2e_test.go`): builds binary to `tmp/tsdproxy-e2e`, cleans leftover containers, runs suite, removes binary on exit.

**TSDProxyInstance** (`helpers.go`): subprocess tsdproxy with generated YAML config. Logs to `tmpDir/tsdproxy.log`. Waits for `/health/ready/` (120s). Cleanup: SIGINT, kill after 15s. Data dir: `/tmp/tsdproxy-e2e/<testname>/`.

**TSNetClient** (`helpers.go`): ephemeral tsnet.Server as test client. `GetNoFollowRedirect` (HTTPS) + `GetNoFollowRedirectHTTP` for assertions.

**ContainerConfig** (`helpers.go`): testcontainers-go wrapper for nginx containers with labels. Tagged `tsdproxy.e2e=true`.

**generateConfig** (`helpers.go`): builds `tsdproxy.yaml` from `configParams`. Handles authKey vs authKeyFile, OAuth, controlURL, Docker host.

**ListEntry types** (`helpers.go`): `ListEntry`, `ListPort`, `ListTailscale`, `ListDashboard` for list provider YAML via `GenerateListProviderFile`.

## GOTCHAS

- Tests run **serially** (shared Docker daemon). No `t.Parallel()`.
- `getFreePort()` has TOCTOU race. OK for serial runs.
- `TestMain` cleans `tsdproxy.e2e=true` containers before suite. Crashed runs may leave stale ones.
- ACME rate limits: rapid re-runs with same hostnames hit Let's Encrypt limits.
- `requireTailscaleAuth` calls `t.Skip()` without auth key. Tests silently skip in CI.
- `requireOAuth` skips without `TSDPROXY_E2E_CLIENTID`/`TSDPROXY_E2E_CLIENTSECRET`.
- Tailscale machine names must be unique. Timestamp-based hostnames for tsnet clients.
- `proxyStartupTimeout` 120s. First-run Tailscale auth + cert can be slow.

## TEST PATTERNS

- **No `t.Parallel()`** — serial only (shared Docker daemon).
- **Subprocess-based**: launches actual tsdproxy binaries, not library imports.
- **Assertion library**: `stretchr/testify/require` (fatal) + `assert` (non-fatal).
- **Skip patterns**: `requireTailscaleAuth(t)`, `requireOAuth(t)`, `requireCloudflare(t)` — graceful `t.Skip()`.
- **testcontainers-go**: nginx containers tagged `tsdproxy.e2e=true` for cleanup.
- **tsnet client**: ephemeral `TSNetClient` for HTTPS assertions (`GetNoFollowRedirect`).
- **Config generation**: `generateConfig()` builds YAML from `configParams` struct.
- **List provider**: `GenerateListProviderFile` / `RenderListProviderFile` for YAML-based list providers.
- **No table-driven tests**: each test case is a separate `func Test*`. No `t.Run()`.
- **TLS testing**: `StartSelfSignedHTTPSServer` / `StartSelfSignedHTTPSContainer` for TLS scenarios.
