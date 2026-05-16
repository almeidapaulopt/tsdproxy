---
title: Docker
prev: /docs/providers
next: /docs/providers/lists
weight: 3
---

To add a service to your TSDProxy instance, you need to add a label to your
service container.

## How to enable

Just add the label `tsdproxy.enable` to true and restart your service. The
container will be started and TSDProxy will be enabled.

```yaml
labels:
  tsdproxy.enable: "true"
```

TSDProxy will use container name as Tailscale server, and will use the first docker
exposed port to proxy traffic. If TSDProxy doesn't detect the port you want to
proxy, you can use `tsdproxy.port` label, more details in [Port configuration](#port-configuration).

## Container Labels

{{% details title="tsdproxy.name" %}}

If you define a name different from the container name, you can define it with
the label `tsdproxy.name` and it will be used as the Tailscale server name.

```yaml
labels:
  tsdproxy.enable: "true"
  tsdproxy.name: "myserver"
```

{{% /details %}}
{{% details title="tsdproxy.proxyprovider" %}}

If you want to use a proxy provider other than the default one, you can define
it with the label `tsdproxy.proxyprovider`.

```yaml
labels:
  tsdproxy.enable: "true"
  tsdproxy.proxyprovider: "providername"
```

{{% /details %}}
{{% details title="tsdproxy.autodetect" %}}

Defaults to true. If you are having problems with the internal network interfaces
autodetection, set to false. You can also use the `no_autodetect` port option
(see [Port options](#port-options)).

```yaml
labels:
  tsdproxy.enable: "true"
  tsdproxy.autodetect: "false"
```

{{% /details %}}
{{% details title="tsdproxy.containeraccesslog" %}}

Enable or disable access logging for this proxy. Defaults to true (enabled).

```yaml
labels:
  tsdproxy.enable: "true"
  tsdproxy.containeraccesslog: "false"
```

{{% /details %}}

## Port configuration

To have better control over the ports you want to proxy, you can use the
`tsdproxy.port` labels.
TSDProxy v2 enables the possibility to define multiple ports to proxy. You can
also define http redirects.

### How to use it

You can use multiple ports to proxy, just define the `tsdproxy.port` label with a different index.

***Proxy***

```yaml
tsdproxy.port.<index>: "<proxy port>/<proxy Protocol>:<container port>/<container protocol>[, <options>]"
```

- **\<index\>** is the index of the port, starting from 1.
- **\<proxy port\>** is the port that will be exposed on the Tailscale network. (Examples: 443, 80, 8080)
- **\<proxy protocol\>** is the protocol that will be used on the proxy. (Examples: http, https)
- **\<container port\>** is the port that will be proxied to the container. (Examples: 80, 8080)
- **\<container protocol\>** is the protocol that will be used on the container. (Examples: http, https)
- **\<options\>** is a comma separated list of options. See [Port options](#port-options).

***Redirect***

```yaml
tsdproxy.port.<index>: "<proxy port>/<proxy Protocol> -> <url>"
```

- **\<index\>** is the index of the port, starting from 1.
- **\<proxy port\>** is the port that will be exposed on the Tailscale network. (Examples: 443, 80, 8080)
- **\<proxy protocol\>** is the protocol that will be used on the proxy. (Examples: http, https)
- **\<url\>** is the url that will be redirected to.

### Examples

```yaml
labels:
  tsdproxy.enable: "true"
  tsdproxy.name: "test"

  # add a https proxy to container target port 80
  tsdproxy.port.1: "443/https:80/http"

  # add a http proxy to container target port 8080, disable TLS validation
  tsdproxy.port.2: "80/http:8080/http, no_tlsvalidate"

  # short format: proxy only (auto-detects port)
  tsdproxy.port.3: "443/https"

  # redirect to https://test.funny-name.ts.net
  tsdproxy.port.4: "81/http->https://test.funny-name.ts.net"

  # redirect to https://othersite.com
  tsdproxy.port.5: "82/http->https://othersite.com"

  # TCP proxy for SSH (see TCP Proxy & SSH docs for details)
  tsdproxy.port.6: "22/tcp:22/tcp"
```

### Port options

| Option | Description |
|--------|-------------|
| `no_tlsvalidate` | Disable TLS validation on the target certificate (TLS validation is enabled by default) |
| `tailscale_funnel` | Activate Tailscale Funnel on the port |
| `no_autodetect` | Disable auto-detection of the target URL for this port |

## Port ranges

You can forward a range of ports using `start-end` syntax. This is useful for
applications like WebRTC or game servers that need many consecutive UDP/TCP ports.

```yaml
tsdproxy.port.<index>: "<start>-<end>/<protocol>:<start>-<end>/<protocol>[, <options>]"
```

- Both the proxy side and the target side can be a range.
- If both sides are ranges, they must have the **same number of ports**.
- One side can be a single port while the other is a range (the single port is
  reused for every port in the range).
- Maximum 1000 ports per range.
- Port ranges do **not** support redirect syntax (`->`).

### Examples

```yaml
labels:
  tsdproxy.enable: "true"
  tsdproxy.name: "neko"

  # Forward 3 UDP ports (56000-56002) — both sides match
  tsdproxy.port.1: "56000-56002/udp:56000-56002/udp"

  # Forward 100 TCP ports, all targeting the same port 8080
  tsdproxy.port.2: "50000-50099/tcp:8080/tcp"
```

## Tailscale Labels

{{% details title="tsdproxy.ephemeral" %}}

If you want to use an ephemeral container, you can define it with the label `tsdproxy.ephemeral`.

```yaml
labels:
  tsdproxy.enable: "true"
  tsdproxy.name: "myserver"
  tsdproxy.ephemeral: "true"
```

{{% /details %}}
{{% details title="tsdproxy.runwebclient" %}}

If you want to enable the Tailscale web client (port 5252), you can define it
with the label `tsdproxy.runwebclient`.

```yaml
labels:
  tsdproxy.enable: "true"
  tsdproxy.name: "myserver"
  tsdproxy.runwebclient: "true"
```

{{% /details %}}
{{% details title="tsdproxy.tsnet_verbose" %}}

If you want to enable Tailscale's verbose mode, you can define it with the label
`tsdproxy.tsnet_verbose`.

```yaml
labels:
  tsdproxy.enable: "true"
  tsdproxy.name: "myserver"
  tsdproxy.tsnet_verbose: "true"
```

{{% /details %}}
{{% details title="tsdproxy.authkey" %}}

Enable TSDProxy authentication with a different AuthKey.
This gives the possibility to add tags on your containers if they were defined when
created the AuthKey.

```yaml
labels:
  tsdproxy.enable: "true"
  tsdproxy.authkey: "YOUR_AUTHKEY_HERE"
```

{{% /details %}}
{{% details title="tsdproxy.authkeyfile" %}}

Path to a file containing the AuthKey. This is useful if you want to use
Docker secrets.

```yaml
labels:
  tsdproxy.enable: "true"
  tsdproxy.authkeyfile: "/run/secrets/authkey"
```

{{% /details %}}
{{% details title="tsdproxy.tags" %}}

Use it to apply tags to your proxy. `tsdproxy.tags` is a comma separated list
of tags. Tags only work with OAuth authentication.

```yaml
labels:
  tsdproxy.enable: "true"
  tsdproxy.tags: "tag:example,tag:server,tag:web"
```

{{% /details %}}

## Health Check Labels

These labels configure backend health monitoring and automatic target re-resolution
when a container restarts and gets a new IP. See [Health Check]({{< ref "/docs/operations/health-check#backend-health-monitoring" >}}) for details.

{{% details title="tsdproxy.auto_restart" %}}

Enable or disable automatic target re-resolution when the backend becomes
unhealthy. Defaults to `true`.

```yaml
labels:
  tsdproxy.enable: "true"
  tsdproxy.auto_restart: "false"
```

{{% /details %}}
{{% details title="tsdproxy.health_check_interval" %}}

Seconds between health probes. Must be at least 1. Defaults to `30`.

```yaml
labels:
  tsdproxy.enable: "true"
  tsdproxy.health_check_interval: "60"
```

{{% /details %}}
{{% details title="tsdproxy.health_check_failures" %}}

Number of consecutive health check failures before triggering re-resolution.
Must be at least 1. Defaults to `3`.

```yaml
labels:
  tsdproxy.enable: "true"
  tsdproxy.health_check_failures: "5"
```

{{% /details %}}
{{% details title="tsdproxy.health_check_cooldown" %}}

Fixed cooldown in seconds between re-resolution attempts while the target
remains unhealthy. Set to `0` (default) to use exponential backoff instead.

```yaml
labels:
  tsdproxy.enable: "true"
  tsdproxy.health_check_cooldown: "120"
```

{{% /details %}}

## Dashboard Labels

{{% details title="tsdproxy.dash.visible" %}}

Defaults to true, set to false to hide on Dashboard.

```yaml
labels:
  tsdproxy.enable: "true"
  tsdproxy.dash.visible: "false"
```

{{% /details %}}
{{% details title="tsdproxy.dash.label" %}}

Sets the proxy label on dashboard. Defaults to tsdproxy.name.

```yaml
labels:
  tsdproxy.enable: "true"
  tsdproxy.name: "nas"
  tsdproxy.dash.label: "Files"
```

{{% /details %}}
{{% details title="tsdproxy.dash.icon" %}}

Sets the proxy icon on dashboard. If not defined, TSDProxy will try to find an
icon based on the image name. See available icons in [icons]({{< ref "/docs/advanced/icons" >}}).

```yaml
labels:
  tsdproxy.enable: "true"
  tsdproxy.dash.icon: "si/portainer"
```

{{% /details %}}

## Legacy Labels (v1)

> [!WARNING]
> The following labels are **deprecated** in v2. They still work for backward
> compatibility but will be removed in a future version (planned for v2.1+).
> Migrate to the new [port configuration](#port-configuration) labels as soon as possible.

| Deprecated Label | Replacement |
|-----------------|-------------|
| `tsdproxy.container_port` | Use `tsdproxy.port.*` labels instead |
| `tsdproxy.scheme` | Use the protocol in `tsdproxy.port.*` labels |
| `tsdproxy.tlsvalidate` | Use the `no_tlsvalidate` option in `tsdproxy.port.*` labels |
| `tsdproxy.funnel` | Use the `tailscale_funnel` option in `tsdproxy.port.*` labels |
