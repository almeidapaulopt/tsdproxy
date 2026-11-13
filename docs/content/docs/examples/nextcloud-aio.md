---
title: Nextcloud AIO
weight: 4
prev: /docs/examples/nextcloud
next: /docs/examples/grafana-stack
---

Expose a [Nextcloud AIO](https://github.com/nextcloud/all-in-one) instance on
your Tailscale network with automatic HTTPS. For the plain `nextcloud` image,
see [Nextcloud]({{< ref "/docs/examples/nextcloud" >}}) instead — AIO works
differently and the two examples are not interchangeable.

## How Nextcloud AIO differs

- **Port 8080 is the AIO interface**, served by the `nextcloud-aio-mastercontainer` container — not Nextcloud itself. Nextcloud upstream recommends opening it **by IP address** (e.g. `https://192.168.1.10:8080`) and never by domain, because HSTS will block domain access to it later. Keep it out of TSDProxy and do the initial setup directly against the server IP.
- **Nextcloud is served by a separate container.** Following the upstream [reverse proxy flow](https://github.com/nextcloud/all-in-one/blob/main/reverse-proxy.md), the mastercontainer is started with `APACHE_PORT` (11000 in this example) and Nextcloud is then served by the `nextcloud-aio-apache` container on that port. Proxying port 80 of the mastercontainer — or 8080 — leads to a 502 or the wrong page.
- **The Apache container cannot carry TSDProxy labels.** It is created and managed by the mastercontainer, and upstream explicitly warns that custom Docker labels or environment variables on it are not possible. The labels therefore go on the **mastercontainer**, and TSDProxy forwards traffic to the `APACHE_PORT` that AIO publishes on the host — no extra port publishing on your side.

## docker-compose.yml

```yaml {filename="docker-compose.yml"}
services:
  tsdproxy:
    image: almeidapaulopt/tsdproxy:2
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
      - tsdproxy-data:/data
      - ./config:/config
    ports:
      - "8080:8080"
    extra_hosts:
      - "host.docker.internal:host-gateway"
    restart: unless-stopped

  nextcloud-aio-mastercontainer:
    image: ghcr.io/nextcloud-releases/all-in-one:latest
    init: true
    restart: always
    container_name: nextcloud-aio-mastercontainer # required by AIO, do not change
    volumes:
      - nextcloud_aio_mastercontainer:/mnt/docker-aio-config # required by AIO, do not change
      - /var/run/docker.sock:/var/run/docker.sock:ro
    ports:
      # AIO interface (self-signed certificate). Only the HOST port may be
      # changed — 8080 is already taken by the tsdproxy dashboard; the
      # container side must stay 8080.
      - "8081:8080"
    environment:
      # Port on which the AIO Apache container publishes Nextcloud on the
      # host (the upstream sample value).
      - APACHE_PORT=11000
      # Keep 0.0.0.0 so the tsdproxy container can reach the published
      # port via host.docker.internal.
      - APACHE_IP_BINDING=0.0.0.0
      # A <tailnet-name>.ts.net name cannot pass AIO's public domain
      # validation — see the Notes below before enabling.
      - SKIP_DOMAIN_VALIDATION=true
    labels:
      tsdproxy.enable: "true"
      tsdproxy.name: "cloud"
      tsdproxy.dash.label: "Nextcloud AIO"
      tsdproxy.dash.icon: "si/nextcloud"
      # Tailscale HTTPS on 443 -> AIO Apache on the host-published
      # APACHE_PORT (HTTP)
      tsdproxy.port.1: "443/https:11000/http"
    restart: always

volumes:
  tsdproxy-data:
  nextcloud_aio_mastercontainer:
    name: nextcloud_aio_mastercontainer # required by AIO backups, do not change
```

## Labels Explained

| Label | Value | Purpose |
|-------|-------|---------|
| `tsdproxy.enable` | `"true"` | Enable proxying for the mastercontainer |
| `tsdproxy.name` | `"cloud"` | Tailscale hostname. Your instance will be at `cloud.<tailnet>.ts.net` |
| `tsdproxy.dash.label` | `"Nextcloud AIO"` | Display name in the TSDProxy dashboard |
| `tsdproxy.dash.icon` | `"si/nextcloud"` | Dashboard icon (from Simple Icons). See [icons]({{< ref "/docs/advanced/icons" >}}) for available options |
| `tsdproxy.port.1` | `"443/https:11000/http"` | HTTPS on 443, forwarding to the host-published `APACHE_PORT` (HTTP), where `nextcloud-aio-apache` answers |

## Access

After authenticating through the dashboard, Nextcloud is available at:

```
https://cloud.<tailnet-name>.ts.net
```

The AIO interface (initial setup, container management, updates) is **not**
proxied by TSDProxy. Open it directly, by IP address:

```
https://<server-ip>:8081
```

## Alternative: manual installation

Nextcloud AIO also offers a
[manual installation](https://github.com/nextcloud/all-in-one/tree/main/manual-install)
where every container is defined in your own `compose.yaml`. There you own the
container definitions and can label `nextcloud-aio-apache` directly, which
lets TSDProxy reach it over the Docker network — the Apache port does not need
to be published on the host at all:

```yaml {filename="compose.yaml (excerpt)"}
services:
  tsdproxy:
    # run tsdproxy in the same compose project / network as the AIO containers
    # ...

  nextcloud-aio-apache:
    # ... existing service definition from the manual-install latest.yml ...
    labels:
      tsdproxy.enable: "true"
      tsdproxy.name: "cloud"
      tsdproxy.port.1: "443/https:11000/http"
```

The trade-offs are AIO's own: you lose the AIO interface, automatic updates
and the built-in backup solution, so this only makes sense if you specifically
need to manage the containers yourself.

## Notes

- **APACHE_IP_BINDING.** Keep it at `0.0.0.0`. Upstream suggests `127.0.0.1` when the reverse proxy runs on the host network — but the tsdproxy container reaches the published port through `host.docker.internal`, which a loopback binding blocks, and the proxy would 502.
- **Domain validation.** During setup the AIO interface asks for your domain and validates that it points to a publicly reachable server. A `.<tailnet-name>.ts.net` name lives inside your tailnet and cannot pass that check, which is why the example sets `SKIP_DOMAIN_VALIDATION=true`. Upstream documents this option as "only if you are completely sure that you've configured everything correctly" — make sure the domain resolves and the proxy works before skipping.
- **No `OVERWRITEPROTOCOL` or trusted domains.** Unlike the plain `nextcloud` image, AIO configures Nextcloud's reverse-proxy settings itself from the domain you enter in the AIO interface. Do not copy those settings from the [Nextcloud]({{< ref "/docs/examples/nextcloud" >}}) example.
- **HTTPS redirect.** If you want HTTP requests to redirect to HTTPS, add a second port:
  ```yaml
  tsdproxy.port.2: "80/http->https://cloud.<tailnet-name>.ts.net"
  ```
- **Other AIO ports.** Optional AIO services publish additional host ports when enabled (for example Talk on 3478 TCP and UDP). If you need one through Tailscale, add a matching `tsdproxy.port.N` label to the mastercontainer.
