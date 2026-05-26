---
title: Dashboard
prev: /docs/advanced
---

TSDProxy includes a built-in web dashboard that displays all your proxies in
real time. Each proxy is shown as a card with its status, URL, icon, and port
information. The dashboard updates automatically via Server-Sent Events (SSE)
as proxies start, stop, or change status.

## Accessing the dashboard

The dashboard is served on the HTTP port configured in your `tsdproxy.yaml`
(default `8080`, bound to `127.0.0.1`). To access it locally:

```text
http://localhost:8080
```

To access the dashboard from your Tailscale network, you need to expose it as a
proxy — just like any other service.

> [!IMPORTANT]
> The default bind address changed from `0.0.0.0` to `127.0.0.1`. If you need
> the dashboard accessible on all interfaces (e.g. via Docker port mapping),
> set `http.hostname: 0.0.0.0` in your config. See
> [Server Configuration]({{< ref "/docs/serverconfig#hostname" >}}) for details.

> [!IMPORTANT]
> All dashboard endpoints require authentication. If you access the dashboard
> through Docker port mapping instead of a Tailscale proxy, there is no
> Tailscale identity to authenticate with. Enable `adminAllowLocalhost: true`
> in your config to allow local access. See
> [Admin Allowlist]({{< ref "/docs/security/admin-allowlist" >}}) and
> [Troubleshooting]({{< ref "/docs/troubleshooting#access-requires-a-tailscale-connection-on-dashboard" >}})
> for details.

## Exposing via Tailscale

{{% steps %}}

### Via Docker labels

Add labels to the TSDProxy container in your `docker-compose.yml`:

```yaml  {filename="docker-compose.yml"}
services:
  tsdproxy:
    image: almeidapaulopt/tsdproxy:2
    # ... other config ...
    labels:
      tsdproxy.enable: "true"
      tsdproxy.name: "dash"
      tsdproxy.port.1: "443/https:8080/http"
```

Restart TSDProxy:

```bash
docker compose restart
```

### Via Lists provider

Add a dashboard entry to your proxy list file:

```yaml  {filename="/config/proxies.yaml"}
dash:
  ports:
    443/https:
      targets:
        - http://127.0.0.1:8080
```

The list file reloads automatically — no restart needed.

### Test access

```bash
curl https://dash.FUNNY-NAME.ts.net
```

> [!NOTE]
> Replace `FUNNY-NAME` with your Tailscale network name.

{{% /steps %}}

## Dashboard configuration

Customize how each proxy appears on the dashboard using labels (Docker) or the
`dashboard` section (Lists).

### Docker labels

| Label | Default | Description |
|-------|---------|-------------|
| `tsdproxy.dash.visible` | `true` | Show or hide the proxy on the dashboard |
| `tsdproxy.dash.label` | proxy name | Display label for the proxy card |
| `tsdproxy.dash.icon` | auto-detected | Icon for the proxy card (see [icons]({{< ref "/docs/advanced/icons" >}})) |
| `tsdproxy.dash.category` | — | Category for grouping proxies in the dashboard |

Example:

```yaml
labels:
  tsdproxy.enable: "true"
  tsdproxy.name: "nas"
  tsdproxy.dash.label: "File Server"
  tsdproxy.dash.icon: "si/synology"
  tsdproxy.dash.category: "Storage"
```

### Lists provider

Use the `dashboard` section in your proxy list entry:

```yaml  {filename="/config/proxies.yaml"}
nas:
  ports:
    443/https:
      targets:
        - http://nas.local:5001
  dashboard:
    visible: true
    label: "File Server"
    icon: "si/synology"
```

> [!TIP]
> TSDProxy auto-detects icons based on the container image name. See
> [Dashboard icons]({{< ref "/docs/advanced/icons" >}}) for the full list of
> available icon libraries.

## Viewer and Admin Roles

The dashboard has two access tiers:

- **Viewers** — all tailnet users who can reach the dashboard. They can view
  proxy status, health, uptime, browse with search/filter/group, and manage
  their own preferences. The Actions and Logs tabs are hidden.
- **Admins** — users in the `admins` list or authenticated via API key. They
  see the full dashboard including the Actions tab (restart, pause, resume,
  reauth) and Logs tab (live access log streaming).

See [Admin Allowlist]({{< ref "/docs/security/admin-allowlist#viewer-role" >}}) for
configuration details.

## Dashboard Features

### Real-time updates

The dashboard uses Server-Sent Events (SSE) via htmx `hx-sse` to stream
updates. Proxy cards, status badges, and action panels update in real time
without page reloads.

### Views

Toggle between grid (card) and list views. The list view shows proxies in a
compact row layout for monitoring many proxies at once. Switch views using the
view toggle in the toolbar — your preference is persisted per-user.

### Sorting, filtering, and grouping

- **Sort** by name, status, or health
- **Filter** by status (Running, Stopped, Error, Paused) and health (Up, Down)
- **Group** by category using the `tsdproxy.dash.category` label

All sorting, filtering, and grouping is server-side. Click a toolbar button
to change the view — the server returns pre-rendered HTML fragments.

### Search

Type in the search bar to filter proxies by name or label. Search is transient
(per-session) and not persisted across browser sessions.

### Per-user preferences

Preferences are persisted per Tailscale user at
`{DataDir}/dashboard/preferences/{userID}.json` and sync across browser sessions:

| Preference | Options |
|---|---|
| Theme | Light / Dark |
| View | Grid / List |
| Sort | Name / Status / Health |
| Grouped | On / Off |
| Status filter | All / Running / Stopped / Error / Paused |
| Health filter | All / Up / Down |
| Pinned proxies | Pinned set |

### Proxy detail modal

Click a proxy card to open the detail modal with tabs:

- **Info** — proxy configuration, ports, Tailscale settings, target provider
- **Actions** — restart, pause, resume, reauth (admin only)
- **Logs** — live access log stream (admin only)

The modal persists across dashboard list refreshes — it is rendered outside
the proxy list DOM so SSE updates don't close an open modal.

### Status timeline and uptime

Each proxy card shows the current uptime duration. The detail modal displays
a status change timeline with timestamps.

### Browser notifications

Enable browser notifications to receive alerts when proxies change status
(Running, Stopped, Error). Click the notification bell icon in the toolbar to
enable — your browser will prompt for permission.

### Version display

The running TSDProxy version is shown in the dashboard footer.

## Keyboard Shortcuts

| Key | Action |
|-----|--------|
| `/` | Focus search bar |
| `Esc` | Close modal / clear search |

## Architecture

The dashboard frontend is built with:

- **htmx 4** with `hx-sse` extension for real-time updates
- **templ** for server-side rendered HTML components
- **Server-Sent Events** for streaming proxy status updates
- **DaisyUI** for styling with dark/light theme support

The previous Datastar-based frontend has been removed. If you had custom CSS
or JS overrides targeting Datastar internals, you will need to update them.
