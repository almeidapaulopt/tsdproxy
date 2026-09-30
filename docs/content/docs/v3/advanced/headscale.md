---
title: Headscale / Custom Control Server
prev: /docs/advanced/docker-networking
next: /docs/advanced/host-mode
---

TSDProxy works with any Tailscale-compatible control server, including
[Headscale](https://headscale.net/). You point TSDProxy at your control server
using the `controlUrl` field in the provider configuration.

The default `controlUrl` is `https://controlplane.tailscale.com` (the Tailscale
SaaS control plane). To use Headscale or any other compatible server, override
this value with your instance's URL.

> [!NOTE]
> OAuth authentication (`clientId` / `clientSecret`) is a Tailscale SaaS feature.
> With Headscale, use an **AuthKey** instead. See
> [Authentication Methods]({{< ref "/docs/v3/security/auth-methods" >}}) for the
> full comparison.

## Setup with Headscale

{{% steps %}}

### Install and configure Headscale

Set up a Headscale instance following the
[official documentation](https://headscale.net/). Make sure it's reachable from
the TSDProxy container over the network.

If you're running Headscale in Docker, note the internal address and port
(e.g. `http://headscale:8080` when sharing a Docker network).

### Generate an AuthKey

Use the Headscale CLI to create a pre-authenticated key:

```bash
headscale preauthkeys create --user myuser --reusable
```

You can also add tags at this stage:

```bash
headscale preauthkeys create --user myuser --reusable --tags "tag:tsdproxy"
```

Copy the generated key. You'll need it in the next step.

> [!CAUTION]
> Store the AuthKey securely. Avoid committing it to version control. Consider
> using `authKeyFile` with Docker secrets or a mounted file instead of
> `authKey` inline.

### Configure TSDProxy

Edit your `tsdproxy.yaml` and set `controlUrl` to your Headscale instance,
then provide the AuthKey:

```yaml {filename="/config/tsdproxy.yaml"}
tailscale:
  providers:
    default:
      controlUrl: http://headscale:8080
      authKey: "your_preauthkey_here"
      authKeyFile: ""
  dataDir: /data/
```

Replace `http://headscale:8080` with the actual URL of your Headscale server.

> [!WARNING]
> Configuration files are case-sensitive. The field is `controlUrl` (camelCase),
> not `controlurl` or `ControlUrl`.

### Restart TSDProxy

Restart to apply the new configuration:

```bash
docker compose restart tsdproxy
```

Check the logs to confirm the proxy connected to your Headscale instance:

```bash
docker compose logs tsdproxy -f
```

{{% /steps %}}

## Docker Compose Example

Here's a complete `docker-compose.yml` running Headscale and TSDProxy together
on a shared network:

```yaml {filename="docker-compose.yml"}
services:
  headscale:
    image: headscale/headscale:latest
    volumes:
      - ./headscale/config:/etc/headscale
      - ./headscale/data:/var/lib/headscale
    ports:
      - "8080:8080"
    networks:
      - tsdproxy-net

  tsdproxy:
    image: almeidapaulopt/tsdproxy:dev
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
      - ./tsdproxy-data:/data
      - ./tsdproxy-config:/config
    networks:
      - tsdproxy-net
    depends_on:
      - headscale

networks:
  tsdproxy-net:
```

With this setup, TSDProxy reaches Headscale at `http://headscale:8080` over the
shared Docker network. No port exposure needed between the two containers.

## Multiple Providers

You can mix Tailscale SaaS and Headscale in the same config by defining
separate providers:

```yaml {filename="/config/tsdproxy.yaml"}
tailscale:
  providers:
    default:
      clientId: "your_client_id"
      clientSecret: "your_client_secret"
      tags: "tag:prod"

    headscale:
      controlUrl: http://headscale:8080
      authKey: "your_headscale_preauthkey"
```

Then assign the Headscale provider to specific Docker servers or individual
containers using `defaultProxyProvider` or the `tsdproxy.proxyprovider` label.

## Troubleshooting

### Connection refused

If TSDProxy logs show `connection refused` when reaching the control URL:

- Verify Headscale is running and healthy.
- Check that both containers are on the same Docker network.
- Confirm the URL and port match Headscale's listen address.
- Test connectivity from the TSDProxy container:
  ```bash
  docker compose exec tsdproxy wget -qO- http://headscale:8080/health
  ```

### Certificate / TLS errors

If you're serving Headscale over HTTPS with a self-signed certificate, you may
see TLS verification errors. Options:

- Use HTTP for internal Docker network communication (`http://headscale:8080`).
- If HTTPS is required, make sure the certificate is trusted inside the
  TSDProxy container, or place a reverse proxy (e.g. Caddy, Traefik) in front
  of Headscale with a valid certificate.

### Machines not appearing in Headscale

- Confirm the AuthKey hasn't expired. Recreate it if needed.
- Check that the `controlUrl` has no trailing slash.
- Verify the user referenced in the AuthKey exists in Headscale.
