---
title: Proxy List
next: /docs/advanced
weight: 4
---

TSDProxy can be configured to proxy using a YAML configuration file.
Can be used multiple files a they are called as target providers.
Each target provider could be used to group the way you decide better to help
you manage your proxies. Or can use a single file to proxy all your targets.

{{< callout type="info" >}}
TSDProxy will reload the proxy list when it is updated.
You only need to restart TSDProxy if your changes are in /config/tsdproxy.yaml
{{< /callout >}}

{{% steps %}}

### How to enable?

In yout /config/tsdproxy.yaml, define the files you want to use, just
like this example where is defined the `critical` and `media` providers.

```yaml  {filename="/config/tsdproxy.yaml"}
Files:
  critical:
    Filename: /config/critical.yaml
    DefaultProxyProvider: tailscale1
    DefaultProxyAccessLog: true
  media:
    Filename: /config/media.yaml
    DefaultProxyProvider: default
    DefaultProxyAccessLog: false
```

```yaml  {filename="/config/critical.yaml"}
nas1:
  url: https://192.168.1.2:5001
  TLSValidate: false
nas2:
  url: https://192.168.1.3:5001
  TLSValidate: false
```

```yaml  {filename="/config/media.yaml"}
music:
  url: http://192.168.1.10:3789
video:
  url: http://192.168.1.10:3800
photos:
  url: http://192.168.1.10:3801
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
Files:
  critical: # Name the target provider
    Filename: /config/critical.yaml # file with the proxy list
    DefaultProxyProvider: tailscale1 # (optional) default proxy provider
    DefaultProxyAccessLog: true # (optional) Enable access logs
```

### Proxy list file options

```yaml  {filename="/config/filename.yaml"}
music: # Name of the proxy
  URL: http://192.168.1.10:3789 # url of service to proxy
  ProxyProvider: default # (optional) name of the proxy provider
  TLSValidate: false # (optional, default true) disable TLS validation
  Tailscale:  # (optional) Tailscale configuration for this proxy
    AuthKey: asdasdas # (optional) Tailscale authkey
    Ephemeral: true # (optional) Enable ephemeral mode
    RunWebClient: false # (optional) Run web client
    Verbose: false # (optional) Run in verbose mode
    Funnel: false # (optional) Run in funnel mode
  Dashboard:
    Visible: false # (optional) doesn't show proxy in dashboard
```

{{% /steps %}}
