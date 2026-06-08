# internal/tlsproviders

TLS certificate provider interface + implementations + lifecycle management.

## STRUCTURE

| File | Role |
|------|------|
| `tlsproviders.go` | `Provider` interface: `Name()`, `Provision()`, `GetCertificate()`, `Cleanup()`. `TLSStatus` type alias for `lifecycle.Status`. Status constants: None/Pending/Active/Error. |
| `lifecycle.go` | `TLSLifecycleManager` — wraps provision/cleanup with status tracking via `lifecycle.StateTracker`. Optional cleanup skip (`cleanup` bool). |
| `acme/acme.go` | ACME (Let's Encrypt) TLS provider via `certmagic`. DNS-01 challenge support (requires DNS provider implementing `certmagic.DNSProvider`). Configurable CA, email, cert storage path. |
| `acme/acme_test.go` | ACME provider tests. |
| `tailscale/tailscale.go` | Tailscale TLS provider — wraps `tsnet.Server.GetCertificate()`. No-op `Provision()` (Tailscale handles cert provisioning internally). `nil` local client accepted initially (replaced after proxy starts). |
| `tailscale/tailscale_test.go` | Tailscale provider tests. Uses `context.TODO()` (should be `context.Background()`). |
| `tlsproviders_test.go` | Interface compliance tests. |

## INTERFACE

```go
type Provider interface {
    Name() string
    Provision(ctx context.Context, domain string) error
    GetCertificate(ctx context.Context, domain string) (tls.Certificate, error)
    Cleanup(ctx context.Context, domain string) error
}
```

## LIFECYCLE PATTERN

Parallels `dnsproviders.LifecycleManager`:

```
Provision(ctx, provider, domain)
  → tracker.Set(domain, TLSStatusPending)
  → provider.Provision(ctx, domain)
  → tracker.Set(domain, TLSStatusActive)

Cleanup(ctx, provider, domain)
  → if !lm.cleanup { return }  — cleanup flag gates deletion
  → provider.Cleanup(ctx, domain)
  → tracker.Delete(domain)
```

`TLSLifecycleManager` wraps `lifecycle.StateTracker` (thread-safe map with `sync.RWMutex`).

## PROVIDER DETAILS

### Tailscale (`tailscale/`)

- Certs provisioned by Tailscale's built-in ACME (automatic, no config needed)
- `Provision()` is a no-op — Tailscale handles everything
- `GetCertificate()` delegates to `tsnet.Server.GetCertificate()` via `local.Client`
- Created inline per-proxy with `tailscaletls.New(nil)`, then `UpdateLocalClient()` called after proxy starts
- Used as default when no external TLS provider configured

### ACME (`acme/`)

- Uses `certmagic` library for ACME protocol
- **Requires DNS provider** for DNS-01 challenges (Cloudflare implements `certmagic.DNSProvider`)
- Configurable: `email` (ACME account), `ca` (certificate authority URL), `certStorage` (path)
- Created per-proxy in `ProxyManager.resolveTLSProviderLocked()` when ACME detected from config
- Per-proxy ACME instance gets the proxy's own DNS provider (bypasses global registration gap)

## REGISTRATION

Config-driven via `ProxyManager.addTLSProviders()`:
```go
switch cfg.Provider {
case "acme":       // → acme.New(dnsProvider, cfg)
case "tailscale":  // warns: auto-created per proxy, skip global
}
```

Tailscale TLS provider is NOT registered globally — created inline per proxy in `resolveTLSProviderLocked()`.

Per-proxy cascade: `proxyCfg.TLSProvider` → `config.DefaultTLSProvider` → `ErrNoTLSProvider`.

Special: `"tailscale"` → `tailscaletls.New(nil)` inline (no map lookup).

## GOTCHAS

- **Per-proxy ACME bypass**: If `addTLSProviders()` skips ACME (no global DNS default), `resolveAndSetProviders()` creates a per-proxy ACME instance using the proxy's own DNS provider. Tests assert this as a known BUG.
- **`tailscaletls.New(nil)`**: TLS provider created with nil local client. Must call `UpdateLocalClient()` after proxy starts or `GetCertificate()` will fail.
- **`CleanupDNS` config flag**: When `false`, TLS `Cleanup()` is a no-op — certs persist across restarts (avoids Let's Encrypt rate limits).
- **ACME tests use `context.TODO()`**: Should be `context.Background()` per project conventions.
