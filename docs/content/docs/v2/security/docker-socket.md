---
title: Docker Socket Security
prev: /docs/v2/security
weight: 1
---

TSDProxy requires Docker socket access. Understanding the security implications is critical.

## Why Socket Access Is Needed

- List/discover containers with `tsdproxy.*` labels
- Read port mappings, network settings, and labels
- Watch container start/stop events in real time

## Security Risk

Mounting `/var/run/docker.sock` gives full Docker API access. A compromised TSDProxy could start, stop, or delete any container.

## Mitigations

### Docker Socket Proxy

Use [tecnativa/docker-socket-proxy](https://github.com/Tecnativa/docker-socket-proxy):

```yaml
services:
  docker-proxy:
    image: tecnativa/docker-socket-proxy
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
    environment:
      CONTAINERS: 1
      EVENTS: 1
    ports:
      - "2375:2375"

  tsdproxy:
    image: almeidapaulopt/tsdproxy:2
    environment:
      DOCKER_HOST: tcp://docker-proxy:2375
```

### Firewall

```bash
sudo ufw allow from 100.64.0.0/10 to any port 8080
```

> [!CAUTION]
> Never expose unauthenticated Docker TCP to any network.
