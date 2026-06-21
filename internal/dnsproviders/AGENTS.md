# internal/dnsproviders

DNS provider interface + implementations + lifecycle management with retry.

## STRUCTURE

| File | Role |
|------|------|
| `dnsproviders.go` | `Provider` interface: `Name()`, `CreateRecord()`, `DeleteRecord()`, `ValidateRecord()`. `DNSStatus` enum (None/Pending/Active/Error). |
| `lifecycle.go` | `LifecycleManager` — wraps DNS operations with retry + status tracking. `SetupDNS()` creates record + validates, `CleanupDNS()` deletes record. Validation is inline (polls via `Retry()`). |
| `retry.go` | `Retry()` — generic retry with exponential backoff (max 30s) for DNS operations. Overflow guard on duration arithmetic. |
| `cloudflare/` | Cloudflare DNS implementation. Full CRUD via Cloudflare API. ACME DNS-01 challenge support via `certmagic.DNSProvider` interface. |
| `magicdns/` | MagicDNS (Tailscale built-in) — no-op for create/delete (MagicDNS is automatic). |

## LIFECYCLE PATTERN

`LifecycleManager` wraps every DNS operation:

```
SetupDNS(ctx, domain, recordType, value)
  → provider.CreateRecord(ctx, domain, recordType, value)
  → retryWithBackoff: provider.ValidateRecord(ctx, domain, recordType, value)
  → status: DNSStatusActive

CleanupDNS(ctx, domain, recordType)
  → provider.DeleteRecord(ctx, domain, recordType)
  → status: DNSStatusNone
```

Status tracked via `DNSStatus` enum. Errors set `DNSStatusError`.

## INTERFACE

```go
type Provider interface {
    Name() string
    CreateRecord(ctx context.Context, domain, recordType, value string) error
    DeleteRecord(ctx context.Context, domain, recordType string) error
    ValidateRecord(ctx context.Context, domain, recordType, expectedValue string) (bool, error)
}
```

## PROVIDER DETAILS

**Cloudflare**: Full CRUD via Cloudflare API. Requires `apiToken` in config. Also implements `certmagic.DNSProvider` for ACME DNS-01 challenges. Snake_case JSON fields use `//nolint:tagliatelle`.

**MagicDNS**: Tailscale's built-in DNS. `CreateRecord` and `DeleteRecord` are no-ops (MagicDNS handles `.ts.net` automatically). Used as default when no external DNS needed.

## REGISTRATION

Config-driven via `ProxyManager.addDNSProviders()`:
```go
switch cfg.Provider {
case "cloudflare": // → cloudflare.New()
case "magicdns":   // → magicdns.New()
}
```

Per-proxy cascade: `proxyCfg.DNSProvider` → `config.DefaultDNSProvider` → `ErrNoDNSProvider`.

## GOTCHAS

- ACME TLS provider requires a DNS provider for DNS-01 challenges. Cloudflare implements both `dnsproviders.Provider` and `certmagic.DNSProvider`.
- MagicDNS is a no-op provider — it exists so the cascade resolution doesn't fail when using Tailscale-only mode.
- `validateproviders.go` in config ensures `defaultDNSProvider` exists in the map before startup.
- DNS validation uses retry with backoff — DNS propagation can take seconds to minutes.

## TEST PATTERNS

- `lifecycle_test.go`: `mockProvider` with error injection fields (`createErr`, `validateErr`, `deleteErr`). Tests status tracking through all lifecycle states.
- `retry_test.go`: Tests exponential backoff, context cancellation, overflow guard.
- `cloudflare/cloudflare_test.go`: `httptest.NewServer` for Cloudflare API mock.
- All tests use `zerolog.Nop()` to suppress logging.
