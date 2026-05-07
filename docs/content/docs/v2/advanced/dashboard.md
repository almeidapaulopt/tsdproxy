---
title: Dashboard
prev: /docs/v2/advanced
---

TSDProxy includes a built-in web dashboard that displays all your proxies in
real time. Each proxy is shown as a card with its status, URL, icon, and port
information. The dashboard updates automatically via Server-Sent Events (SSE)
as proxies start, stop, or change status.

## Accessing the dashboard

The dashboard is served on the HTTP port configured in your `tsdproxy.yaml`
(default `8080`). To access it locally:

```text
http://localhost:8080
```

To access the dashboard from your Tailscale network, you need to expose it as a
proxy — just like any other service.

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
| `tsdproxy.dash.icon` | auto-detected | Icon for the proxy card (see [icons]({{< ref "/docs/v2/advanced/icons" >}})) |

Example:

```yaml
labels:
  tsdproxy.enable: "true"
  tsdproxy.name: "nas"
  tsdproxy.dash.label: "File Server"
  tsdproxy.dash.icon: "si/synology"
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
> [Dashboard icons]({{< ref "/docs/v2/advanced/icons" >}}) for the full list of
> available icon libraries.
