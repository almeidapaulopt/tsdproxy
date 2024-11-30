---
title: Docker secrets
---

If you want to use Docker secrets to store your Tailscale authkey, you can use
the following example:

{{% steps %}}

### Requirements

Make sure you have Docker Swarm enabled on your server.

<https://docs.docker.com/engine/swarm/secrets/>

"Docker secrets are only available to swarm services, not to standalone
containers. To use this feature, consider adapting your container to run as a service."

### Add a docker secret

We need to create a docker secret, which we can name `authkey` and store the Tailscale
authkey in it. We can do that using the following command:

```bash
printf "Your Tailscale AuthKey" | docker secret create authkey -
```

### TsDProxy Docker compose

```yaml docker-compose.yml
services:
  tsdproxy:
    image: almeidapaulopt/tsdproxy:latest
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
      - datadir:/data
      - <PATH TO CONFIG>:/config
    secrets:
      - authkey

volumes:
  datadir:

secrets:
  authkey:
    external: true
```

### TsDProxy configuration

```yaml /config/tsdproxy.yaml
tailscale:
  providers:
     default: # name of the provider
      authkeyfile: "/run/secrets/authkey" 
```

### Restart tsdproxy

``` bash
docker compose restart
```

{{% /steps %}}
