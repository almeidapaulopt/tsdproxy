---
title: Lists
prev: /docs/providers/docker-reference
next: /docs/advanced
weight: 5
---

TSDProxy can be configured to proxy using a YAML configuration file.
Multiple lists can be used, and they are referred to as target providers.
Organize your proxies into groups or use a single file for all targets —
whatever works best for your setup.

> [!CAUTION]
> Configuration files are case sensitive

{{% steps %}}

### How to enable?

In your /config/tsdproxy.yaml, specify the lists you want to use, just
like this example where the `critical` and `media` providers are defined.

```yaml  {filename="/config/tsdproxy.yaml"}
lists:
  critical:
    filename: /config/critical.yaml
    defaultProxyProvider: tailscale1
    defaultProxyAccessLog: true
  media:
    filename: /config/media.yaml
    defaultProxyProvider: default
    defaultProxyAccessLog: false
```

```yaml  {filename="/config/critical.yaml"}
nas1:
  ports:
    443/https:
      targets:
        - http://nas1.local:5001
    80/http:
      targets:
        - nas1.funny-name.ts.net
      isRedirect: true
nas2:
  ports:
    443/https:
      targets:
        - https://nas2.local:5001
```

```yaml  {filename="/config/media.yaml"}
music:
  ports:
    443/https:
      targets:
        - http://192.168.1.10:3789
video:
  ports:
    443/https:
      targets:
        - http://192.168.1.10:8080
photos:
  ports:
    443/https:
      targets:
        - http://192.168.1.10:8181
host-ssh:
  ports:
    22/tcp:
      targets:
        - tcp://192.168.1.10:22
```

This configuration will create two groups of proxies:

- nas1.funny-name.ts.net and nas2.funny-name.ts.net
  - Self-signed tls certificates
  - Both use 'tailscale1' Tailscale provider
  - All access logs are enabled
- music.ts.net, video.ts.net and photos.ts.net.
  - On the same host with different ports
  - Use 'default' Tailscale provider
  - Don't enable access logs

### Provider Configuration options

```yaml  {filename="/config/tsdproxy.yaml"}
lists:
  critical: # Name the target provider
    filename: /config/critical.yaml # file with the proxy list
    defaultProxyProvider: tailscale1 # (optional) default proxy provider
    defaultProxyAccessLog: true # (optional) Enable access logs
```

### Proxy list file options

```yaml  {filename="/config/filename.yaml"}
proxyname: # Name of the proxy
 proxyProvider: default # (optional) name of the proxy provider

  tailscale:  # (optional) Tailscale configuration for this proxy
    authKey: asdasdas # (optional) Tailscale authkey
    ephemeral: false # (optional) (defaults to false) Enable ephemeral mode
    runWebClient: false # (optional) (defaults to false)  Run web client
    verbose: false # (optional) (defaults to false) Run in verbose mode
    tags: "tag:example,tag:server" # (optional) tags to apply
                                   # (will override the default provider tags)

  ports:
    port/protocol: #example 443/https, 80/http
    targets: # list of targets (in this version only the first will be used)
      - http://sub.domain.com:8111 # change to your target
    tailscale: # (optional)
      funnel: true # (optional) (defaults to false), enable funnel mode
    isRedirect: true # (optional) (defaults to false), redirect to the target 
    tlsValidate: false # (optional) /defaults to true), disable targets TLS validation

  dashboard:
    visible: false # (optional) (defaults to true) doesn't show proxy in dashboard
    label: "" # (optional), label to be shown in dashboard
    icon: "" # (optional), icon to be shown in dashboard
```

> [!TIP]
> TSDProxy will reload the proxy list when it is updated.
> You only need to restart TSDProxy if your changes are in /config/tsdproxy.yaml

> [!NOTE]
> See available icons in [icons](../../advanced/icons).

{{% /steps %}}
