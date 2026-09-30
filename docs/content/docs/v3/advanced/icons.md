---
title: Dashboard icons
---

TSDProxy supports three comprehensive icon libraries and custom user-added icons.

## Icon libraries

| Prefix | Library | Website |
|--------|---------|---------|
| `mdi/` | Material Design Icons | [pictogrammers.com/library/mdi](https://pictogrammers.com/library/mdi/) |
| `si/`  | Simple Icons | [simpleicons.org](https://simpleicons.org) |
| `sh/`  | Selfh.st Icons | [selfh.st/icons](https://selfh.st/icons/) |

> [!NOTE]
> `mdi/` and `si/` icons automatically invert in dark mode for readability.
> `sh/` icons are designed for both themes and render as-is.

## How icons work

Icons are no longer bundled in the TSDProxy binary. Instead, they are
downloaded on demand from the upstream GitHub repositories the first time
a browser requests them, then cached to disk at `{dataDir}/icons/`.

### First request flow

1. Browser requests `<img src="/icons/sh/nginx.svg">`
2. TSDProxy checks the cache directory (`{dataDir}/icons/sh/nginx.svg`)
3. If cached, serves immediately with a 24-hour cache header
4. If missing and `.svg`, downloads from the upstream GitHub repository
5. If download fails or the icon is unknown, serves the default TSDProxy icon

Non-SVG icons (`.png`, `.webp`) are never downloaded — they must be placed
on disk manually (see [Custom icons](#custom-icons) below).

## Usage

### Docker labels

Set `tsdproxy.dash.icon` with the library prefix and icon name:

```yaml
labels:
  tsdproxy.dash.icon: "si/tailscale"
  tsdproxy.dash.icon: "mdi/music-box"
  tsdproxy.dash.icon: "sh/adguard-home"
```

See the [Docker provider]({{< ref "/docs/v3/providers/docker.md" >}}#tsdproxydashicon)
for details.

### Lists provider

Use the `icon` field in the `dashboard` section:

```yaml
nas:
  dashboard:
    icon: "si/synology"
```

See the [Lists provider]({{< ref "/docs/v3/providers/lists.md" >}}#proxy-list-file-options)
for details.

### Auto-detection

If no icon is specified, TSDProxy extracts the image name from the container
(e.g. `nginx:latest` → `nginx`) and prefixes it with the default icon set
(`sh` by default). The result is `sh/nginx`.

You can change the default set in the [server configuration](#server-configuration).

## Custom icons

You can add your own icons by dropping files directly into the icons directory.
This works with SVG, PNG, and WebP formats.

### With a set prefix

Place the file at `{dataDir}/icons/<set>/<name>.<ext>` and reference it as
`<set>/<name>.<ext>`:

```text
{dataDir}/icons/
  icom/
    my-icon.svg
    my-icon.png
```

Label:

```yaml
tsdproxy.dash.icon: "icom/my-icon.png"
```

### Without a set prefix (root-level)

Place the file directly in the icons directory and reference it by name only:

```text
{dataDir}/icons/
  my-icon.png
```

Label:

```yaml
tsdproxy.dash.icon: "my-icon.png"
```

Icon files placed in the icons directory are served immediately — no restart
required.

### Custom icon formats

| Format | Extension | On-demand download | Dark mode invert |
|--------|-----------|-------------------|-----------------|
| SVG    | `.svg`    | ❌ (must be on disk) | ❌ |
| PNG    | `.png`    | ❌ (must be on disk) | ❌ |
| WebP   | `.webp`   | ❌ (must be on disk) | ❌ |

Custom icons are never inverted in dark mode.

## Server configuration

You can configure icon behavior in `tsdproxy.yaml`:

```yaml
icons:
  dir: /data/icons          # custom icons directory (default: {dataDir}/icons)
  download: true             # enable on-demand download (default: true)
  defaultSet: sh             # default icon set for auto-detection (default: sh)
```

### Options

| Field | Default | Description |
|-------|---------|-------------|
| `dir` | `{dataDir}/icons` | Path to the icons directory. Useful for sharing a volume with icon files. |
| `download` | `true` | Enable on-demand icon download from GitHub. Set to `false` for airgapped (offline) deployments. |
| `defaultSet` | `sh` | Default icon set used when auto-detecting from a container image name. |

### Airgapped / offline deployments

When `download: false`, icons are never fetched from GitHub. To pre-seed the
icon cache, use the `icons download` CLI command:

```bash
tsdproxy icons download --icons-dir /data/icons
```

This downloads all three icon sets, verifies SHA256 checksums, and extracts
them to the specified directory. The command is idempotent — it skips sets
that already have cached icons.

You can also manually copy icon files into the icons directory. Any SVG, PNG,
or WebP file placed there is immediately available.

### Examples

**Custom icons directory with a shared volume:**

```yaml
icons:
  dir: /mnt/icons
  download: false
```

**Keep defaults but use Simple Icons for auto-detection:**

```yaml
icons:
  defaultSet: si
```

## Dark mode behaviour

| Icon source | Dark mode |
|-------------|-----------|
| `mdi/*`     | Inverted (`dark:invert`) |
| `si/*`      | Inverted (`dark:invert`) |
| `sh/*`      | As-is |
| Custom icons | As-is |

> [!NOTE]
> Inline chrome glyphs (copy, star, info buttons) are always embedded in the
> binary and render correctly even without an internet connection or icon cache.
