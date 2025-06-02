---
title: Docker
weight: 3
---

To add a service to your TSDProxy instance, you need to add a label to your
service container.

## How to enable

Just add the label `tsdproxy.enable` to true and restart you service. The
container will be started and TSDProxy will be enabled.

```yaml
labels:
  tsdproxy.enable: "true"
```

TSProxy will use container name as Tailscale server, and will use the first docker
exposed port to proxy traffic. If TSDProcy doesn't detect the port you want to
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

Defaults to true, if your having problem with the internal network interfaces
autodetection, set to false.

```yaml
labels:
  tsdproxy.enable: "true"
  tsdproxy.autodetect: "false"
```

{{% /details %}}

### Port configuration

To have better control over the ports you want to proxy, you can use the
`tsdproxy.port` labels.
TSDProxy v2 enables the possibility to define multiple ports to proxy. You can
also define http redirects.

#### How to use it

You can use multiple ports to proxy, just define multiple the `tsdproxy.port` label with a different index.

***Proxy***

```yaml
tsdproxy.port.<index>: "<proxy port>/<proxy Protocol>:<container port>/<container protocol>[, <options>]"
```

- **\<index\>** is the index of the port, starting from 1.
- **\<proxy port\>** is the port that will be exposed on the Tailscale network. (Examples: 443,80,8080)
- **\<proxy protocol\>** is the protocol that will be used on the proxy. (Examples: http,https)
- **\<container port\>** is the port that will be proxied to the container. (Examples: 80,8080)|
- **\<container protocol\>** is the protocol that will be used on the container. (Examples: http,https)
- **\<options\>** is a comma separated list of options. (Examples: noautodetect, notlsverify)

***Redirect***

```yaml
tsdproxy.port.<index>: "<proxy port>/<proxy Protocol> -> <url>"
```

- **\<index\>** is the index of the port, starting from 1.
- **\<proxy port\>** is the port that will be exposed on the Tailscale network. (Examples: 443,80,8080)
- **\<proxy protocol\>** is the protocol that will be used on the proxy. (Examples: http,https)
- **\<url\>** is the url that will be redirected to.

#### Examples

```yaml
labels:
  tsdproxy.enable: "true"
  tsdproxy.name: "test"

  # add a https proxy to container target port 80
  tsdproxy.port.1: "443/https:80/http"

  # add a http proxy to container target port 8080, do not try to autodetect
  tsdproxy.port.2: "80/http:8080/http, no_autodetect"

  # on port 81 redirect to https://test.funny-name.ts.net
  tsdproxy.port.3: "81/http->https://test.funny-name.ts.net"

  # on port 81 redirect to https://othersite.com
  tsdproxy.port.4: "82/http->https://othersite.com"
```

#### Port options

| Option | Description |
|-----|---|
|no_tlsvalidate | disable the tls validation on target certification |
|tailscale_funnel| activate tailscale funnel in the port|

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
{{% details title="tsdproxy.webclient" %}}

If you want to enable the Tailscale webclient (port 5252), you can define it
with the label `tsdproxy.webclient`.

```yaml
labels:
  tsdproxy.enable: "true"
  tsdproxy.name: "myserver"
  tsdproxy.webclient: "true"
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

Enable TSDProxy authentication with a different Authkey.
This give the possibility to add tags on your containers if were defined when
created the Authkey.

```yaml
labels:
  tsdproxy.enable: "true"
  tsdproxy.authkey: "YOUR_AUTHKEY_HERE"
```

{{% /details %}}
{{% details title="tsdproxy.authkeyfile" %}}

Authkeyfile is the path to your Authkey. This is useful if you want to use
docker secrets.

```yaml
labels:
  tsdproxy.enable: "true"
  tsdproxy.authkey: "/run/secrets/authkey"
```

{{% /details %}}

{{% details title="tsdproxy.tags" %}}

Use it to apply tags to your proxy. tsdproxy.tags is as comma separated list
of tags.

```yaml
labels:
  tsdproxy.enable: "true"
  tsdproxy.tags: "tag:example,tag:server,tag:web"
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

Sets the proxy icon on dashboard. If not defined, TSDProxy will try to find a
icon based on the image name. See available icons in [icons](/docs/v2/advanced/icons/).

```yaml
labels:
  tsdproxy.enable: "true"
  tsdproxy.dash.icon: "si/portainer"
```

{{% /details %}}
