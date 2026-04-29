---
title: Tailscale
next: /docs/troubleshooting
---

## OAuth

{{% steps %}}

### Disable AuthKey

OAuth authentication mode is enable if no AuthKey is set in the configuration
for Tailscale provider.

Like:

```yaml {filename="/config/tsdproxy.yaml"}
tailscale:
  providers:
    default: 
      authKey: ""
      authKeyFile: ""
```

When the proxy starts, it will wait to be authenticated with the Tailscale.

### Go Dashboard

Go to TSDProxy Dashboard (example: <http://192.168.1.1:8080>).

### Authenticate

Click on the Proxy that should show "Authentication" status.

>[!TIP]
> Set "Ephemeral" to false in the Tailscale provider to avoid the need of
authentication next time. See [docker Ephemeral label](../../docker/#tsdproxyephemeral)
or [Proxy List configuration](../../list/#proxy-list-file-options)

{{% /steps %}}

## AuthKey

{{% steps %}}

### Generate Authkey

1. Go to [https://login.tailscale.com/admin/settings/keys](https://login.tailscale.com/admin/settings/keys)
2. Click in "Generate auth key"
3. Add a Description
4. Enable Reusable
5. Enable Ephemeral
6. Add Tags if you need
7. Click in "Generate key"

>[!WARNING]
> If tags were added to the key, all proxies initialized with the same authkey
> will get the same tags.
> Add a new Tailscale provider to the configuration if
> you need to use different)

### Add to configuration

Add you key to the configuration as follow:

```yaml {filename="/config/tsdproxy.yaml"}
tailscale:
  providers:
    default: 
      authKey: "GENERATED KEY HERE"
      authKeyFile: ""
```

### Restart

Restart TSDProxy

{{% /steps %}}

## Funnel

Beside adding the TSDProxy configuration to activate Funnel to a proxy, you also
should give permissions on Tailscale ACL. See [here](.././troubleshooting/#funnel-doesnt-work) to more detail.
