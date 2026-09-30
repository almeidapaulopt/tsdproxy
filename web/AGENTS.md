# web

Frontend: Vite/Bun build to `dist/`, embedded into the Go binary via `go:embed` + statigz + brotli. htmx 4 beta + hx-sse extension + Tailwind v4 + daisyUI v5.

## STRUCTURE

| File | Role |
|------|------|
| `web.go` | Go side. `//go:embed dist` + `statigz.FileServer` with brotli precompressed variants. `NewAssets()` now takes `(iconsDir, dataDir, defaultSet, download)`. `IconsHandler()` serves `/icons/` — on-demand download + cache + default fallback. `GuessIcon` prefixes basename with default set (no index). `IconSVG` reads from cache dir with `fill="currentColor"`, falls back to embedded default. |
| `icon_download.go` | `IconDownloader` — per-file on-demand download from `raw.githubusercontent.com`, atomic write, `singleflight` dedup, `failed` map for 404 skip, path-traversal validation, charset enforcement. |
| `default_icon.svg` | Embedded default icon (~1 KB) served at `/icons/tsdproxy.svg` and as fallback for unknown SVGs. |
| `package.json` | htmx.org 4.0.0-beta4, tailwindcss v4.3, daisyui v5.5, vite v8, vite-plugin-pwa v1.3, vite-plugin-compression2 (gzip + brotli). |
| `vite.config.js` | rollup config forces non-hashed `app.js` + `styles.css` output; custom `proxy-root` middleware forwards bare `/` to backend `:8080` during dev; `server.proxy` forwards `/dashboard`, `/stream`, `/api`; PWA with autoUpdate + workbox. |
| `app.js` | Keyboard nav (j/k/i/Enter/Esc/`/`/Ctrl-F) + SSE handlers exposed on `window`. |
| `styles.css` | DaisyUI plugin config, `@source` scans `../internal/**/*.templ` + `app.js` for classes, custom base/components/utilities layers. |
| `tsdproxy-dark.css` / `tsdproxy-light.css` | DaisyUI CSS-first theme tokens (oklch colors, radii, sizes). |
| `scripts/icons.json` | Manifest of 3 icon repos (`mdi`, `sh`, `si`) with version + SHA256. Embedded at compile time for the downloader + CLI. |
| `dist/` | Build output: `app.js`, `styles.css`, `sw.js`, `workbox-*.js`, `manifest.webmanifest`, PWA icons, brotli + gzip precompressed variants. No longer contains `icons/`. |

## BUILD PIPELINE

```
bun run build              ->  dist/  (vite + tailwind + PWA + brotli/gzip)
                |
go:embed dist              ->  web.go (statigz + brotli FileServer)
                |
binary                     ->  served at runtime with on-the-fly brotli decode
```

- `make dev` orchestrates docker-compose up + `cd web && bun run dev` (vite dev with proxy middleware) + air Go hot reload.
- `cd web && bun run build` rebuilds `dist/`. The `build` script runs `vite build` directly (no icon download step).
- `go:embed dist` is read at Go COMPILE time. Stale `dist/` means stale frontend in the binary.

## NON-HASHED FILENAME CONSTRAINT

- `vite.config.js` rollup config: `entryFileNames` returns `'app.js'` for the app chunk, `assetFileNames` returns `'styles.css'` for the styles asset. All other chunks/assets keep `[name]-[hash]`.
- WHY: templ templates reference `<script src="/app.js">` and `<link href="/styles.css">` as static strings. Hashing breaks them.
- Implication: aggressive cache headers are unsafe for `app.js`/`styles.css`. Service worker handles invalidation via the PWA update flow.

## ICON PIPELINE (On-Demand)

Icons are no longer embedded in the binary. The embedded icon footprint is ~1 KB (default icon) + ~500 B (manifest).

**Default icon:** `default_icon.svg` — embedded, served at `/icons/tsdproxy.svg`, used as fallback for any unknown/missing SVG.

**Manifest:** `scripts/icons.json` — embedded at compile time. Declares 3 repos: `si` (simple-icons/simple-icons @ tag), `mdi` (Templarian/MaterialDesign-SVG @ tag), `sh` (selfhst/icons @ commit). Each entry carries `version`, `refType`, `svgDir`, `sha256` of the tarball.

**On-demand download (`IconDownloader`):** Per-file download from `raw.githubusercontent.com/{repo}/{version}/{svgDir}/{name}.svg`:
- Atomic write to `<DataDir>/icons/<set>/<name>.svg`
- `singleflight` dedup for concurrent requests
- `failed` map skips retry for known 404s
- Path-traversal validation (name charset `^[a-z0-9][a-z0-9._-]*$`, containment check)
- Disabled when `icons.download: false` (airgapped deployments)

**Resolution flow:**
1. Browser `<img src="/icons/sh/nginx.svg">` hits `IconsHandler`
2. Handler checks `<icons_dir>/sh/nginx.svg` on disk → serve
3. If missing + `.svg` + `download=true` → fetch from GitHub, cache, serve
4. If still missing → serve embedded default icon (200, not 404)
5. Non-SVG (`.png`/`.webp`) → 404 if not on disk (no download)

**Auto-detection (`GuessIcon`):** Strips registry path + `:tag` + `@digest` from container image, prefixes with `defaultSet` (configurable, default `sh`). Returns e.g. `sh/nginx`. No index lookup — the handler resolves on demand.

**Inline SVG glyphs (`IconSVG`):** Used only for chrome glyphs (copy/star/info) that need `fill="currentColor"`. Reads from cache dir, falls back to embedded default. Does NOT trigger downloads (synchronous templ render path).

**Offline pre-seed:** `tsdproxy icons download` downloads all tarballs, verifies SHA256, extracts to `<icons_dir>/<set>/`. Idempotent.

**Add a new icon set:** edit `scripts/icons.json`, then run `tsdproxy icons download` on the target server.

## HTMX + SSE PATTERN

- htmx 4 beta is pulled via npm and aliased in vite config: `htmx.org/dist/ext/hx-sse.js`.
- `app.js` exposes handlers on `window` because htmx's `hx-sse` extension calls them BY NAME via string lookup. ES module imports don't work here.
- Handlers all receive the SSE event and parse `evt.detail`:
  - `showProxyNotification(evt)` splits `evt.detail` on `\x00` into name + status, fires a browser `Notification`.
  - `handleConnId(evt)` sets the hidden `#sseConnId` input.
  - `scrollLogs(evt)` sets `scrollTop` on the selector in `evt.detail`.
  - `trimLogs(evt)` splits `evt.detail` on `\n` into selector + max lines, trims children.
  - `requestNotifications()` requests `Notification` permission.
- Keyboard shortcuts: `j`/`k` move focus across visible proxy cards, `i` opens the info modal, `Enter` clicks the open link, `Esc` closes modal then clears search then clears focus, `/` or `Ctrl-F` focuses the search input.

## PWA

- vite-plugin-pwa with `registerType: 'autoUpdate'`. Service worker updates silently on next load.
- `manifest.webmanifest` generated at build time from the vite config manifest block.
- Workbox precaches `**/*.{js,css,html,ico}` via `globPatterns`. Icons excluded from compression but served as static assets.

## GOTCHAS

- **`go:embed dist` is compile-time**: stale `dist/` means stale frontend. Run `bun run build` before `go build` if the frontend changed. `make dev` handles this automatically.
- **Non-hashed filenames are intentional**: do not "fix" by adding content hashes. templ templates depend on the stable `app.js`/`styles.css` names.
- **`window` handler pattern is required**: hx-sse calls handlers by string name. ES module exports won't work.
- **Dev proxy middleware**: `proxy-root` plugin forwards bare `/` (HTML routes) to Go backend `:8080` during `bun run dev`. `server.proxy` forwards `/dashboard`, `/stream`, `/api`. Vite serves assets.
- **DaisyUI v5 uses CSS-first config**: theme tokens live in `tsdproxy-dark.css` / `tsdproxy-light.css` via `@plugin "daisyui/theme"`, not in a JS config.
- **Beta htmx**: `htmx.org 4.0.0-beta4` is pinned. API may change between betas.
- **`@source` scanning**: `styles.css` scans `../internal/**/*.templ` and `app.js` so Tailwind picks up classes used in templ templates. New template directories may need adding to `@source`.
- **`IconSVG` does not trigger downloads**: it reads from cache or returns the embedded default. Downloads are only triggered by the HTTP `/icons/` handler from browser `<img>` tags.
- **Path-traversal is enforced in two places**: `IconDownloader.validateName` (charset check) and `IconDownloader.safePath` (containment check). Both must pass before any disk write.
- **`icons.download` defaults to true**: airgapped deployments must set `icons.download: false` and pre-seed with `tsdproxy icons download` or manual file drops.
- **`GuessIcon` now always uses `defaultSet`**: it no longer searches across sets. A name that exists only in `mdi` or `si` will resolve as a miss (showing the default icon) unless the user specifies an explicit `tsdproxy.dash.icon=mdi/foo` label.

