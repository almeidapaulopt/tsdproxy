---
title: Changelog
weight: 500
---

> [!CAUTION]
> This is the legacy v1 changelog. For the current changelog, see the [main changelog](/docs/changelog/).

{{% steps %}}

### 1.4.0

#### New features

- OAuth authentication using the Dashboard.
- Dashboard has now proxy status.
- Icons and Labels can be used to customize the Dashboard.

#### Fixes

- Error on port when autodetect is disabled.

### 1.3.0

#### Breaking changes

Configuration files are now validated and doesn't allow invalid configuration keys
[Verify valid configuration keys](../serverconfig/#sample-configuration-file).

#### New features

- Generate TLS certificates for containers when starting proxies.
- Configuration files are now validated.

### 1.2.0

#### New features

Dashboard finally arrived.

### 1.1.2

#### Fixes

Reload Proxy List Files when changes.

#### New features

- Quicker start with different approach to start proxies in docker
- Add support for targets with self-signed certificates.

### 1.1.1

#### New Docker container labels

##### tsdproxy.autodetect

If TSDProxy, for any reason, can't detect the container's network you can
disable it.

##### tsdproxy.scheme

If a container uses https, use tsdproxy.scheme=https label.

### 1.1.0

#### New File Provider

TSDProxy now supports a new file provider. It's useful if you want to proxy URL
without Docker.
Now you can use TSDProxy even without Docker.

### 1.0.0

#### New Autodetection function for containers network

TSDProxy now tries to connect to the container using docker internal
ip addresses and ports. It's more reliable and faster, even in container without
exposed ports.

#### New configuration method

TSDProxy still supports the Environment variable method. But there's much more
power with the new configuration yaml file.

#### Multiple Tailscale servers

TSDProxy now supports multiple Tailscale servers. This option is useful if you
have multiple Tailscale accounts, if you want to group containers with the same
AUTHKEY or if you want to use different servers for different containers.

#### Multiple Docker servers

TSDProxy now supports multiple Docker servers. This option is useful if you have
multiple Docker instances and don't want to deploy and manage TSDProxy on each one.

#### New installation scenarios documentation

Now there is a new  [scenarios]({{< ref "/docs/v1/scenarios/_index.md" >}}) section.

#### New logs

Now logs are more readable and easier to read and with context.

#### New Docker container labels

**tsdproxy.proxyprovider** is the label that defines the Tailscale proxy
provider. It's optional.

#### TSDProxy can now run standalone

With the new configuration file, TSDProxy can be run standalone.
Just run tsdproxyd --config ./config .

#### New flag --config

This new flag allows you to specify a configuration file. It's useful if you
want to use as a command line tool instead of a container.

```bash
tsdproxyd --config ./config/tsdproxy.yaml
```

{{% /steps %}}
