---
title: Docker secrets
---

If you want to use Docker secrets to store your Tailscale authkey, you can use
the following example:

{{% steps %}}

### Create a `.env` file with your Tailscale AuthKey

Create a new file in the same directory as your `docker-compose.yaml` file named `.env`.
Set your Tailscale AuthKey to a new variable (e.g. `TS_AUTHKEY`).

```env
TS_AUTHKEY="Your Tailscale AuthKey"
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
    environment: TS_AUTHKEY
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
