# internal/targetproviders/proxmox

Proxmox VE target provider: watches a Proxmox cluster (QEMU VMs + LXC containers) for guests whose notes/description field carries a `tsdproxy:` YAML block, resolves target URLs from guest-agent/LXC interface addresses, generates per-proxy config. No event API exists, so lifecycle is discovered by polling `/cluster/resources` and diffing snapshots.

## STRUCTURE

| File | Role |
|------|------|
| `proxmox.go` | `Client` (TargetProvider impl). `WatchEvents` polls every `pollIntervalSeconds`; first poll doubles as initial scan, its failure goes to errChan (ProxyManager backoff reconnect), later transient failures keep the watcher + snapshot alive. `pollOnce` → `handleRunningGuest` (diff state machine: Start/Stop/Restart) + `handleStoppedGuest` + `emitDeleted`. `AddTarget`/`ReResolve` via `buildProxyConfig`. |
| `rest.go` | `APIClient` interface + hand-rolled `restClient` (no official PVE Go SDK). API-token auth (`Authorization: PVEAPIToken=<id>=<secret>`), PVE JSON envelope (`{"data":...}`) decoding, optional CA cert (`tlsCaCertFile`) / insecure TLS, optional `Doer` injection. `GetVersion` doubles as connect-time credential check. Node filter applied client-side in `ListGuests`. |
| `notes.go` | Notes/description → `notesConfig` parsing (ALWAYS non-nil; `Enable=false` means no tsdproxy data). Accepts BOTH styles: Docker/Incus flat grammar (`tsdproxy.port.443: 8443/https:8080/http`) and list-provider schema (`ports: {"443/https": {targets: [...]}}` + proxyProvider/dashboard/tailscale/... keys). List-style scalars are translated into flat settings keys, filling only absent keys (flat wins). Whole-string base64 notes tolerated (raw YAML tried first). |
| `guest.go` | `guest` struct. `setInterfaces` (eth0-first, IPv4-first, drops loopback/link-local/multicast), `getPorts` merges flat-grammar ports (`model.Ports` + `generateTargetFromFirstTarget`) with list-style entries (`processListPort`: explicit targets verbatim, else target generated from guest address — long-label placeholder carries scheme/port, short labels use proxy protocol/port; funnel/tlsValidate gated by operator settings), `newProxyConfig` builds `model.Config`. |
| `consts.go` | Flat settings key constants (mirror Docker label names), guest type/status strings, timing constants. |
| `errors.go` | Sentinel errors: `ErrGuestNotEnabled`, `ErrGuestNotRunning`, `ErrGuestNotFound`, `ErrNoAddressFound`, `ErrInvalidAPIToken`, `ErrInvalidGuestID`. |
| `*_test.go` | `mockAPIClient` (provider tests), `httptest` server (REST tests, `newPVE` factory serves /version automatically), notes parsing table tests, goleak `TestMain`. |

## CONFIG KEY SCHEMA

Notes are the only free-form field on a Proxmox guest (QEMU `notes`, LXC `description`). A guest is enabled when its notes contain tsdproxy data, unless `tsdproxy.enable: false` (presence = opt-in, list-provider semantics). Two accepted shapes:

```yaml
# Docker/Incus style — port value is the shared label grammar
tsdproxy:
  port:
    443: 8443/https:8080/http     # proxy 8443/https -> guest:8080/http
  dash.icon: jellyfin

# list provider style — same keys as a list file entry
tsdproxy:
  hostname: myapp                # -> tsdproxy.name
  ports:
    "443/https":
      targets:
        - "http://10.0.0.5:8080" # explicit; omit to generate from guest address
      tlsValidate: false          # gated by allowTlsValidateDisable
      tailscale:
        funnel: true              # gated by allowGuestFunnel
```

Rest of the notes must be valid YAML (comment out prose with `#`). Flat keys are canonical; when both styles set the same field, the flat key wins. Typed lookups use the shared `internal/targetproviders/settings` package; flat per-guest overrides (health_*, ratelimit.*, authkey/authkeyfile) work exactly like Docker/Incus.

## KEY DESIGN DECISIONS

- **Polling, not events**: `/cluster/resources?type=vm` every N seconds; per-state diff emits Start (new/restarted/re-enabled), Stop (stopped/disabled/deleted — only when tracked, i.e. a proxy exists), Restart (notes DeepEqual change while tracked; untracked change retries Start).
- **Snapshot survives transient errors**: config-fetch or notes-parse failures keep the previous snapshot (`continue`), never looking like a change or deletion. Only the FIRST poll failure surfaces via errChan → ProxyManager backoff.
- **Tracked map = proxy existence**: `guests` map populated by `AddTarget` gates all Stop emissions — ProxyManager never receives stops for nonexistent proxies.
- **Guest ID is `<type>/<vmid>`** (e.g. `qemu/100`): stable across node migration, unique cluster-wide, matches the resources endpoint `id` field.
- **Target resolution**: provider `targetHostname` override → else first usable guest address (QEMU needs guest agent `network-get-interfaces`; LXC uses `/interfaces`; agent missing/failed → no addresses, fallback decides). Same preference order as Incus: eth0 first, IPv4 before IPv6.
- **Templates and non-vm resource types skipped** in pollOnce.
- **`findGuestResource` consults the snapshot first**, falls back to a fresh listing (AddTarget can race the poll loop).

## GOTCHAS

- **PVE 9 field naming**: QEMU VMs use `description` for notes (same as LXC); the `notes` property no longer exists on the config PUT schema. `parseGuestNotes` prefers `description` and falls back to `notes` for PVE ≤8 qemu.
- **LXC `/interfaces` returns `inet`/`inet6` as single strings** ("10.0.0.5/24"), not arrays.
- **`"port-label":` (null YAML entry) wipes struct defaults** during unmarshal — `mergeListPorts` decodes into `map[string]*notesPort` and rebuilds null entries with fresh defaults.
- QEMU guests need the guest agent running inside the guest OS for addresses; without it (and without targetHostname) ports are dropped gracefully and the proxy fails with "no ports configured".
- yaml.v3 yields `map[any]any` for nested maps with non-string keys (numeric port keys!) — `normalizeTree` in notes.go converts them; every downstream lookup assumes `map[string]any`.
- `ConfigPrefix` ends with a dot; `flattenSettings` prefixes always carry the trailing dot — don't add another.
- Long port labels (`443/https:8080/http`) parse into a `0.0.0.0` placeholder target (`placeholderHost` const); explicit targets REPLACE the placeholder (first one), generated targets rewrite its host keeping scheme/port.
- Map values in `PortConfigList` are not addressable — copy to a local var before calling pointer methods in tests.
- List-style `ports` requires the nested block form; flat dotted `tsdproxy.ports.*` keys are NOT supported (list style is inherently nested).
- PVE error bodies: `{"errors": {...}}` — `pveErrorMessage` formats them; status check happens before envelope decode.
- `newRestClient` does a live `/version` call — unit tests must serve it (see `newPVE` in rest_test.go).
- `DeleteProxy` wraps `targetproviders.ErrTargetNotFound`; enablement errors use package sentinels.
