---
title: Server configuration
weight: 2
---


TSDProxy use the configuration file `/config/tsdproxy.yaml`

{{< callout type="warning" >}}
The environment variables configuration used until v0.6.0 is deprecated and
will be removed in the future.
{{< /callout >}}

{{% steps %}}

### Sample configuration File

```yaml  {filename="/config/tsdproxy.yaml"}
defaultproxyprovider: default
docker:
  local: # name of the docker provider
    host: unix:///var/run/docker.sock # host of the docker socket or daemon
    targethostname: 172.31.0.1 # hostname or IP of docker server
    defaultproxyprovider: default # name of which proxy provider to use
files: {}
tailscale:
  providers:
    default: # name of the provider
      authkey: your-authkey # define authkey here
      authkeyfile: "" # use this to load authkey from file. If this is defined, Authkey is ignored
      controlurl: https://controlplane.tailscale.com # use this to override the default control URL
  datadir: /data/
http:
  hostname: 0.0.0.0
  port: 8080
log:
  level: info # set logging level info, error or trace
  json: false # set to true to enable json logging
proxyaccesslog: true # set to true to enable container access log
```

### Log section

#### level

Define the logging level. The default is info.

#### json

Set to true if what logging in json format.

### Tailscale section

You can use the following options to configure Tailscale:

#### datadir

Define the data directory used by Tailscale. The default is `/data/`.

#### providers

Here you can define multiple Tailscale providers. Each provider is configured
with the following options:

```yaml  {filename="/config/tsdproxy.yaml"}
   default: # name of the provider
      authkey: your-authkey # define authkey here
      authkeyfile: "" # use this to load authkey from file.
      controlurl: https://controlplane.tailscale.com 
```

Look at next example with multiple providers.

```yaml  {filename="/config/tsdproxy.yaml"}
tailscale:
  providers:
    default:
      authkey: your-authkey
      authkeyfile: ""
      controlurl: https://controlplane.tailscale.com
 
    server1:
      authkey: authkey-server1
      authkeyfile: ""
      controlurl: http://server1
 
    differentkey:
      authkey: authkey-with-diferent-tags
      authkeyfile: ""
      controlurl: https://controlplane.tailscale.com
```

TSDProxy is configured with 3 tailscale providers. Provider 'default' with tailscale
servers, Provider 'server1' with a different tailscale server and provider 'differentkey'
using the default tailscale server with a different authkey where you can add any
tags.

### Docker section

TSDProxy can use multiple docker servers. Each docker server can be configured
like this:

```yaml  {filename="/config/tsdproxy.yaml"}
  local: # name of the docker provider
    host: unix:///var/run/docker.sock # host of the docker socket or daemon
    targethostname: 172.31.0.1 # hostname or IP of docker server
    defaultproxyprovider: default # name of which proxy provider to use
```

Look at next example of using a multiple docker servers configuration.

```yaml  {filename="/config/tsdproxy.yaml"}
docker:
  local: 
    host: unix:///var/run/docker.sock 
    defaultproxyprovider: default 
  srv1: 
    host: tcp://174.17.0.1:2376
    targethostname: 174.17.0.1
    defaultproxyprovider: server1
```

TSDProxy is configured with a local server and a server remote 'srv1'

#### host

host is the address of the docker socket or daemon. The default is `unix:///var/run/docker.sock`

#### targethostname

Is the ip address or dns name of docker server. TSDProxy has a autodetect system
to connect with containers, but there's some cases where it's necessary to use
the other interfaces besides the docker internals.

#### defaultproxyprovider

Defaultproxyprovider is the name of the proxy provider to use. (defined in tailscale
providers section). Any container defined to be proxied will use this provider
unless it has a specific provider defined label.

{{% /steps %}}
