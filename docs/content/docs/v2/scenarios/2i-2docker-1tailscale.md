---
title: Two TSDProxy instances, two Docker servers, one provider
---

Each server runs its own TSDProxy instance, both using the same auth key.

## SRV1 Config

```yaml {filename="/config/tsdproxy.yaml"}
defaultProxyProvider: default
docker:
  srv1:
    host: unix:///var/run/docker.sock
    defaultProxyProvider: default
tailscale:
  providers:
    default:
      authKey: "SAMEKEY"
```

## SRV2 Config

```yaml {filename="/config/tsdproxy.yaml"}
defaultProxyProvider: default
docker:
  srv2:
    host: unix:///var/run/docker.sock
    defaultProxyProvider: default
tailscale:
  providers:
    default:
      authKey: "SAMEKEY"
```
