---
title: TCP Proxy & SSH
weight: 6
---

TSDProxy supports proxying raw TCP connections through your Tailscale network.
This enables you to expose SSH servers, databases, gRPC services, and any other
TCP-based protocol without HTTP overhead.

## How it works

When you configure a port with the `tcp` protocol, TSDProxy creates a raw TCP
listener on the Tailscale node and forwards all traffic bidirectionally to the
target. No HTTP parsing or TLS termination is performed — the bytes flow
through as-is.

```
Client ──TCP──► Tailscale node ──TCP──► Target (SSH, database, etc.)
```

## SSH examples

### Docker containers

Expose an SSH server running inside a Docker container:

```yaml {filename="docker-compose.yml"}
services:
  myserver:
    image: linuxserver/openssh-server
    environment:
      - PUID=1000
      - PGID=1000
    labels:
      tsdproxy.enable: "true"
      tsdproxy.name: "ssh-server"
      # Proxy port 22/tcp → container port 22/tcp
      tsdproxy.port.1: "22/tcp:22/tcp"
```

After authenticating the proxy in the dashboard, connect with:

```bash
ssh user@ssh-server.your-tailnet.ts.net
```

### Proxy to the Docker host SSH

To reach the Docker host's SSH server through Tailscale:

```yaml {filename="docker-compose.yml"}
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
    labels:
      tsdproxy.enable: "true"
      tsdproxy.name: "host-ssh"
      tsdproxy.autodetect: "false"
      # Proxy port 22/tcp → host.docker.internal:22/tcp
      tsdproxy.port.1: "22/tcp:22/tcp"
```

> [!NOTE]
> `autodetect` is set to `false` because the SSH port on the host is not
> published through Docker's port mapping. TSDProxy resolves the target via
> `host.docker.internal` instead.

### Lists configuration

Expose an SSH server using a [proxy list](../providers/lists):

```yaml {filename="/config/servers.yaml"}
host-ssh:
  ports:
    22/tcp:
      targets:
        - tcp://192.168.1.10:22
```

### Custom proxy port

If you don't want to use port 22 on the Tailscale side, pick a different port:

```yaml
labels:
  tsdproxy.enable: "true"
  tsdproxy.name: "my-ssh"
  # Tailscale clients connect on port 2222
  tsdproxy.port.1: "2222/tcp:22/tcp"
```

Connect with:

```bash
ssh -p 2222 user@my-ssh.your-tailnet.ts.net
```

## Database examples

### PostgreSQL

```yaml
labels:
  tsdproxy.enable: "true"
  tsdproxy.name: "postgres"
  tsdproxy.port.1: "5432/tcp:5432/tcp"
```

Connect with:

```bash
psql -h postgres.your-tailnet.ts.net -p 5432 -U myuser mydb
```

### MySQL / MariaDB

```yaml
labels:
  tsdproxy.enable: "true"
  tsdproxy.name: "mysql"
  tsdproxy.port.1: "3306/tcp:3306/tcp"
```

### Redis

```yaml
labels:
  tsdproxy.enable: "true"
  tsdproxy.name: "redis"
  tsdproxy.port.1: "6379/tcp:6379/tcp"
```

## Port configuration reference

| Format | Description | Example |
|--------|-------------|---------|
| `<port>/tcp` | Short format — auto-detects target port | `22/tcp` |
| `<port>/tcp:<port>/tcp` | Full format — explicit proxy and target ports | `22/tcp:2222/tcp` |
| `<port>/tcp:<port>` | Target port without protocol (defaults to tcp) | `2222/tcp:22` |

### Lists configuration

```yaml
proxyname:
  ports:
    <port>/tcp:
      targets:
        - tcp://<hostname>:<port>
```

## UDP proxying

TSDProxy also supports proxying UDP traffic. Use the `udp` protocol to forward
UDP ports. This is useful for applications like WebRTC, game servers, or VoIP
that rely on UDP.

### Single UDP port

```yaml
labels:
  tsdproxy.enable: "true"
  tsdproxy.name: "myapp"
  tsdproxy.port.1: "5000/udp:5000/udp"
```

### UDP port range

For applications that need many consecutive UDP ports, use range syntax:

```yaml
labels:
  tsdproxy.enable: "true"
  tsdproxy.name: "neko"
  # Forward 3 UDP ports
  tsdproxy.port.1: "56000-56002/udp:56000-56002/udp"
```

See [Port ranges](../providers/docker#port-ranges) for full range syntax details.

### Lists configuration

```yaml {filename="/config/servers.yaml"}
myapp:
  ports:
    5000/udp:
      targets:
        - udp://192.168.1.10:5000
neko:
  ports:
    56000-56002/udp:
      targets:
        - udp://192.168.1.10:56000
```

## Notes

- **No TLS termination**: TCP proxying forwards raw bytes. The target service
  is responsible for any encryption (e.g., SSH handles its own key exchange).
- **No Funnel**: Tailscale Funnel does not support raw TCP listeners. TCP ports
  are only accessible within your tailnet.
- **Docker networking**: For non-HTTP protocols inside Docker containers,
  TSDProxy connects directly to the container IP rather than through Docker's
  published port mapping. This requires the proxy and the target container to
  share a Docker network.
- **Multiple ports**: You can mix HTTP/HTTPS and TCP ports on the same proxy:

```yaml
labels:
  tsdproxy.enable: "true"
  tsdproxy.name: "myapp"
  tsdproxy.port.1: "443/https:80/http"
  tsdproxy.port.2: "22/tcp:22/tcp"
```
