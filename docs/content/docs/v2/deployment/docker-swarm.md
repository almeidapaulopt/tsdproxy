---
title: Docker Swarm Deployment
prev: /docs/v2/deployment
weight: 2
---

TSDProxy supports Docker Swarm for managing proxies across a cluster.

## Deploying with Swarm

```yaml docker-compose.yml
services:
  tsdproxy:
    image: almeidapaulopt/tsdproxy:2
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
      - datadir:/data
      - /path/to/config:/config
    ports:
      - "8080:8080"
    extra_hosts:
      - "host.docker.internal:host-gateway"
    deploy:
      mode: replicated
      replicas: 1
      placement:
        constraints:
          - node.role == manager
    secrets:
      - authkey

volumes:
  datadir:

secrets:
  authkey:
    external: true
```

Deploy: `docker stack deploy -c docker-compose.yml tsdproxy`

## Service Labels

Service labels work identically to container labels:

```yaml
services:
  nginx:
    image: nginx:latest
    deploy:
      labels:
        tsdproxy.enable: "true"
        tsdproxy.name: "my-nginx"
        tsdproxy.port.1: "443/https:80/http"
```

## Considerations

- Run TSDProxy on a manager node to access the Docker API
- Use `deploy.placement.constraints` to pin to managers
- For multi-node setups, ensure Docker API is reachable from the manager
