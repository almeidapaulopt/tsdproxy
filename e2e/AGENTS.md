# E2E Tests

## OVERVIEW

Integration tests against real Tailscale infrastructure. 25 files, 59 test functions, all gated behind `//go:build e2e`. Tests spin up actual tsdproxy binaries, real Docker containers via testcontainers-go, and real tsnet connections.

## STRUCTURE

```
e2e/
├── e2e_test.go              # TestMain: build binary, clean containers, run suite
├── helpers.go               # Test infrastructure (847 lines): TSDProxyInstance, TSNetClient, config gen
├── basic_test.go            # Start/stop/restart, hostname, ephemeral, multi-container
├── labels_test.go           # Legacy + modern label parsing, dashboard, access log
├── ports_test.go            # Port formats, multi-port, redirect, TLS validate
├── tcp_test.go              # Basic TCP forwarding
├── tcp_advanced_test.go     # Large transfer, concurrent connections
├── funnel_test.go           # Tailscale Funnel exposure
├── health_test.go           # /health/ready/ lifecycle
├── health_liveness_test.go  # /health/live/ endpoint
├── persistence_test.go      # Identity survives restart
├── reload_test.go           # List provider live-reload
├── discovery_test.go        # Cold start discovery
├── network_test.go          # Bridge, host, auto-detect, custom hostname
├── custom_network_test.go   # Custom Docker network
├── ssh_test.go              # SSH TCP proxy
├── websocket_test.go        # WebSocket forwarding
├── providers_test.go        # Docker vs list, provider override, OAuth
├── tags_test.go             # Tags propagation
├── authkey_labels_test.go   # Per-container authKey/authKeyFile
├── duplicates_test.go       # Duplicate hostname recovery
├── multiprovider_test.go    # Multi-provider selection
├── methods_test.go          # HTTP method forwarding
├── negative_test.go         # Missing labels, no ports, invalid targets
└── runwebclient_test.go     # runWebClient label
```

## RUNNING

```bash
make test/e2e                      # gotestsum -tags=e2e -timeout=0
go test -tags=e2e -timeout=0 ./e2e/
TS_AUTHKEY=tskey-auth-xxx go test -tags=e2e -run TestBasicProxy ./e2e/
```

Required: `TS_AUTHKEY` or `TS_AUTHKEY_FILE`. Optional: `TS_CLIENT_ID` + `TS_CLIENT_SECRET` (OAuth tests), `TS_TAGS` (default: `tag:tsdproxy-e2e`).

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
- `requireOAuth` skips without `TS_CLIENT_ID`/`TS_CLIENT_SECRET`.
- Tailscale machine names must be unique. Timestamp-based hostnames for tsnet clients.
- `proxyStartupTimeout` 120s. First-run Tailscale auth + cert can be slow.
