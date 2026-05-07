---
title: Docker Networking
prev: /docs/advanced/environment-variables
next: /docs/advanced/headscale
---

TSDProxy sits between your Tailscale network and your Docker containers. For
traffic to flow, TSDProxy needs a network path **from its own container to the
target container**. How that path is established depends entirely on Docker
networking.

This page walks through the networking modes, how auto-detection works, and
what to do when things don't connect.

## How TSDProxy Reaches Containers

When TSDProxy creates a proxy for a container, it builds a target URL like
`http://172.17.0.3:80` and reverse-proxies Tailscale traffic to that address.
The hostname and port in that URL come from either auto-detection or explicit
labels.

```
┌─────────────────────────────────────────────────┐
│ Docker Network (bridge)                         │
│                                                 │
│  TSDProxy ──HTTP──► Container (port 80)        │
│  (172.17.0.2)        (172.17.0.3)              │
└─────────────────────────────────────────────────┘
```

If TSDProxy can't reach that address, the proxy won't work. That's it. Almost
every networking issue comes down to this: the target URL points somewhere
unreachable.

## Auto-Detection

When auto-detection is enabled (the default), TSDProxy probes the container to
figure out the correct target URL. Here's what it does:

1. Looks up the container's published port info from Docker
2. Builds candidate URLs using the container's internal IP and/or the configured
   `targetHostname`
3. Tries to connect to each candidate, up to **5 attempts** with a **5-second
   pause** between tries
4. Uses a **2-second dial timeout** per attempt
5. Picks the first URL that connects successfully

Auto-detection works well when TSDProxy and the target share a Docker network.
When they don't, or when the container runs in host network mode, you'll need
to help it along.

> [!TIP]
> If auto-detection fails, check the logs at `trace` level to see which URLs
> were tried and why they failed. See [Enabling debug logging]({{< ref "/docs/troubleshooting#enabling-debug-logging" >}}).

When auto-detection isn't finding the right target, you have two options:

- Disable it per-container with `tsdproxy.autodetect: "false"` and specify the
  port explicitly
- Disable it per-port with the `no_autodetect` port option

Both approaches are described in the [Docker labels]({{< ref "/docs/providers/docker#port-options" >}})
documentation.

## Networking Modes

### Scenario 1: Shared Bridge Network (Default)

This is the simplest setup. TSDProxy and the target container are on the same
Docker network. TSDProxy can reach the container directly by its internal IP.

```
┌──────────────────────────────────────────────────┐
│ Docker Network "myapps"                           │
│                                                   │
│  TSDProxy ──HTTP──► nginx (port 80)              │
│  (172.20.0.2)        (172.20.0.3)                │
└──────────────────────────────────────────────────┘
```

Auto-detection works out of the box. No extra configuration needed.

{{% steps %}}

#### docker-compose.yml

```yaml
services:
  tsdproxy:
    image: almeidapaulopt/tsdproxy:2
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
      - datadir:/data
      - ./config:/config
    restart: unless-stopped
    networks:
      - myapps

  nginx:
    image: nginx:latest
    labels:
      tsdproxy.enable: "true"
    networks:
      - myapps

networks:
  myapps:
```

#### tsdproxy.yaml

```yaml {filename="/config/tsdproxy.yaml"}
docker:
  local:
    host: unix:///var/run/docker.sock
    targetHostname: host.docker.internal
    tryDockerInternalNetwork: true
    defaultProxyProvider: default
```

> [!TIP]
> Setting `tryDockerInternalNetwork: true` tells auto-detection to prefer the
> container's internal Docker IP over `targetHostname`. This is the key setting
> for shared-network setups.

{{% /steps %}}

### Scenario 2: Docker Socket from Host (host.docker.internal)

In the default docker-compose setup, TSDProxy connects to the Docker socket
but doesn't share a network with the target containers. It reaches them through
the Docker host's gateway IP instead.

```
┌────────────────┐        ┌────────────────────────┐
│ TSDProxy       │        │ Docker Host             │
│ container      │        │                        │
│                │  HTTP  │  ┌── nginx (:8111)     │
│  host.docker   ├───────►│  └── app     (:3000)  │
│  .internal     │        │                        │
└────────────────┘        └────────────────────────┘
```

This requires two things:

1. `extra_hosts` in the TSDProxy container to map `host.docker.internal` to the
   host gateway
2. `targetHostname: host.docker.internal` in the Docker provider config so
   auto-detection uses this address

This is the setup generated by default when you first run TSDProxy.

{{% steps %}}

#### docker-compose.yml

```yaml
services:
  tsdproxy:
    image: almeidapaulopt/tsdproxy:2
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
      - datadir:/data
      - ./config:/config
    restart: unless-stopped
    ports:
      - "8080:8080"
    extra_hosts:
      - "host.docker.internal:host-gateway"

  nginx:
    image: nginx:latest
    ports:
      - "8111:80"
    labels:
      tsdproxy.enable: "true"

volumes:
  datadir:
```

#### tsdproxy.yaml

```yaml {filename="/config/tsdproxy.yaml"}
docker:
  local:
    host: unix:///var/run/docker.sock
    targetHostname: host.docker.internal
    defaultProxyProvider: default
```

> [!IMPORTANT]
> Target containers must publish their ports (`-p` flag or `ports:` in compose)
> for TSDProxy to reach them through the host gateway. Unpublished ports aren't
> accessible from outside the container's network.

{{% /steps %}}

### Scenario 3: Host Network Mode

When a container runs with `network_mode: host`, it shares the host's network
stack. There's no Docker bridge, no port mapping, and no internal container IP.
Auto-detection can't probe anything because Docker reports no published ports
for host-mode containers.

{{% steps %}}

#### Disable auto-detection

Use the `no_autodetect` port option or set `tsdproxy.autodetect: "false"` at
the container level:

```yaml
services:
  myservice:
    image: myservice:latest
    network_mode: host
    labels:
      tsdproxy.enable: "true"
      tsdproxy.port.1: "443/https:8080/http, no_autodetect"
```

Or disable it for the whole container:

```yaml
    labels:
      tsdproxy.enable: "true"
      tsdproxy.autodetect: "false"
      tsdproxy.port.1: "443/https:8080/http"
```

#### Verify connectivity

The target port must be reachable from the TSDProxy container at the configured
`targetHostname`. If TSDProxy also runs in host network mode, use `127.0.0.1`.

{{% /steps %}}

For more details on host network mode, see
[Service with host network_mode]({{< ref "/docs/advanced/host-mode" >}}).

### Scenario 4: Docker Internal Network (tryDockerInternalNetwork)

The `tryDockerInternalNetwork` setting changes how auto-detection picks the
target URL. When enabled, auto-detection prefers the container's internal Docker
IP over the `targetHostname`.

This is useful when TSDProxy and target containers share a Docker network but
you still want `targetHostname` as a fallback for containers on other networks.

{{% steps %}}

#### Enable in tsdproxy.yaml

```yaml {filename="/config/tsdproxy.yaml"}
docker:
  local:
    host: unix:///var/run/docker.sock
    targetHostname: host.docker.internal
    tryDockerInternalNetwork: true
    defaultProxyProvider: default
```

#### Per-container override

Individual containers can override this behavior with the `tsdproxy.autodetect`
label:

```yaml
labels:
  tsdproxy.enable: "true"
  tsdproxy.autodetect: "false"
  tsdproxy.port.1: "443/https:8080/http"
```

{{% /steps %}}

> [!TIP]
> `tryDockerInternalNetwork` only affects auto-detection. If you've disabled
> auto-detection with `tsdproxy.autodetect: "false"` or `no_autodetect`, this
> setting has no effect.

## Configuration Options

| Option | Where | Purpose |
|--------|-------|---------|
| `targetHostname` | Docker provider config | Default hostname for reaching containers. Defaults to `172.31.0.1` |
| `tryDockerInternalNetwork` | Docker provider config | Prefer internal Docker IPs for auto-detection. Defaults to `false` |
| `host.docker.internal` | Docker `extra_hosts` | Maps to the Docker host gateway for cross-network access |
| `tsdproxy.autodetect` | Container label | Enable or disable auto-detection per container. Defaults to `true` |
| `no_autodetect` | Port option | Disable auto-detection for a specific port |

## Troubleshooting Network Issues

> [!CAUTION]
> Before digging into network issues, enable trace logging. It shows exactly
> which URLs auto-detection is trying and why they fail.
>
> ```yaml
> log:
>   level: trace
> ```

| Symptom | Likely Cause | Fix |
|---------|-------------|-----|
| Connection refused | Wrong port, or service not listening | Check the container's port labels. Verify the service is running inside the container |
| Connection timeout | Wrong hostname/IP, firewall, or different network | Check `targetHostname`. Verify firewall allows traffic from Docker networks. Ensure TSDProxy and the target share a network or the target publishes ports |
| Container appears in dashboard but can't reach it | TSDProxy and target are on different Docker networks | Add both to the same network, or set `targetHostname` to `host.docker.internal` with `extra_hosts` |
| Auto-detection keeps failing | Port probing can't connect within retries | Use `tsdproxy.autodetect: "false"` with an explicit port as a fallback |

> [!TIP]
> The most reliable fix for persistent network problems is to disable
> auto-detection and specify the target explicitly. This removes the guesswork
> from TSDProxy's side:
>
> ```yaml
> labels:
>   tsdproxy.enable: "true"
>   tsdproxy.autodetect: "false"
>   tsdproxy.port.1: "443/https:8080/http"
> ```

For general troubleshooting steps, see
[Troubleshooting]({{< ref "/docs/troubleshooting" >}}).
