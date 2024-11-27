---
title: Docker secrets
---

If you want to use Docker secrets to store your Tailscale authkey, you can use the following example:

{{% steps %}}

### Requirements ###

Make sure you have Docker Swarm enabled on your server.

<https://docs.docker.com/engine/swarm/secrets/>

"Docker secrets are only available to swarm services, not to standalone containers. To use this feature, consider adapting your container to run as a service."

### Add a docker secret

We need to create a docker secret, which we can name `authkey` and store the Tailscale authkey in it. We can do that using the following command:

```bash
printf "Your Tailscale AuthKey" | docker secret create authkey -
```

### TsDProxy Docker compose

```yaml docker-compose.yml
services:
  tailscale-docker-proxy:
    image: almeidapaulopt/tsdproxy:latest
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
      - datadir:/data
    restart: unless-stopped
    environment:
      # Get AuthKey from your Tailscale account
      - TSDPROXY_AUTHKEYFILE=/run/secrets/authkey 
      # Address of docker server (access to example.com ports)
      - TSDPROXY_HOSTNAME=192.168.1.1 
      - DOCKER_HOST=unix:///var/run/docker.sock 
    secrets:
      - authkey

volumes:
  datadir:

secrets:
  authkey:
    external: true
```

### Restart tsdproxy

``` bash
docker compose restart
```

{{% /steps %}}
