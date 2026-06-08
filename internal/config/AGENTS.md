# internal/config

Config loading, validation, secrets, key normalization, fsnotify live-reload, legacy env-to-YAML generation.

## STRUCTURE

| File | Role |
|------|------|
| `config.go` | `config` struct (all YAML config types), `Config` singleton (package-level `*config`), `InitializeConfig()` entry point. Secrets loading from files, env overrides for Tailscale auth. |
| `configfile.go` | `File` — YAML I/O with fsnotify. `NewFile()` loads+decodes, `Watch()` debounces reload (100ms). `onChange` callback receives `fsnotify.Event`. |
| `generateproviders.go` | Legacy env-based config generation (`generateDefaultProviders`). Creates Docker + Tailscale provider configs from `DOCKER_HOST`, auth key env vars. |
| `validator.go` | `ValidateConfig()` — `go-playground/validator` + custom validators. Checks: default proxy provider exists, DNS/TLS provider references valid, domain+FQDN rules, Tailscale config consistency. Custom error types: `DefaultProxyProviderNotFoundError`, `DomainProviderError`. |
| `keynormalizer.go` | YAML key case-insensitivity: `normalizeNodeKeys()` walks YAML AST, rewrites keys to canonical camelCase via reflect-based lookup. Levenshtein distance suggestions for unknown keys. Handles nested structs, maps, slices. |
| `testconfig.go` | `SetTestConfig()` — sets up minimal `Config` singleton for tests (single Tailscale provider, empty Docker/Lists maps). |
| `secrets_test.go` | Secret loading tests: file-based auth keys, client secrets, API tokens. `saveEnv`/`writeFile` helpers. |
| `config_test.go` | Config loading integration tests. |
| `generateproviders_test.go` | Legacy env generation tests. |
| `keynormalizer_test.go` | Key normalization: case folding, underscore/hyphen stripping, Levenshtein suggestions, nested struct/map/slice traversal. |
| `validator_test.go` | Validation rule tests. |

## CONFIG SINGLETON

```go
var Config *config  // package-level, set by InitializeConfig()
```

Accessed globally as `config.Config`. No DI, no synchronization beyond single-goroutine init sequence.

## INITIALIZATION FLOW

```
InitializeConfig(configPath, dataDir)
  → config{} with defaults (creasty/defaults)
  → generateDefaultProviders() — legacy env fallback
  → loadConfigFile() — YAML decode + key normalization + validate
  → loadSecretsFromFiles() — authKeyFile → AuthKey, clientSecretFile → ClientSecret, etc.
  → LoadTailscaleEnvOverrides() — TAILSCALE_AUTHKEY, TS_AUTHKEY overrides
  → ValidateConfig() — full structural validation
```

## KEY NORMALIZATION

YAML keys are case-insensitive with auto-correction:
- `normalizeNodeKeys()` rewrites keys in-place before `yaml.Node.Decode()`
- `buildKeyLookup()` indexes all yaml-tagged fields via reflect
- Unknown keys → `keyIssue` with Levenshtein suggestions (distance ≤ 3 or prefix ≥ 3)
- Handles: nested structs, pointer structs, map values, slices

## VALIDATION RULES

Custom validators in `validator.go`:
- `defaultproxyprovider` — referenced provider must exist in Tailscale map
- `dnsprovider` — DNS provider name must exist in DNSProviders map
- `tlsprovider` — TLS provider name must exist in TLSProviders map
- `domain` / `fqdn` — FQDN format for domain fields
- Cross-field: Tailscale shared mode requires hostname; services mode constraints

## SECRETS LOADING

File-based secrets replace their inline counterparts:
- `authKeyFile` → overwrites `AuthKey`
- `clientSecretFile` → overwrites `ClientSecret`
- `apiKeyFile` → overwrites `APIKey`
- `apiTokenFile` → overwrites `APIToken`
- All read via `os.ReadFile`, wrapped in `secretstring.SecretString`

## GOTCHAS

- **`generateproviders.go` swallows `defaults.Set()` errors**: prints to stderr but continues with possibly uninitialized structs. Lines 36, 60.
- **Case-sensitive YAML keys** — mitigated by keynormalizer, but raw `yaml.Unmarshal` without normalization will fail on casing mismatches.
- **`fmt.Fprintf(os.Stderr, ...)` instead of zerolog**: used in `generateDefaultProviders()` because logger may not exist yet.
- **`File.Watch()` debounces at 100ms** (`//nolint:mnd`) — rapid successive writes may coalesce.
- **`SetTestConfig()` directly mutates `Config` singleton** — not goroutine-safe, tests must not run in parallel.
- **`ClearSecrets()` zeroes all SecretString fields** — called after loading to prevent accidental logging, but the raw values persist in `config` struct fields until GC.
