# TsDProxy - Tailscale Docker Proxy

This Docker-based application automatically creates a proxy to virtual addresses in your Tailscale network based on Docker container labels. It simplifies traffic redirection to services running inside Docker containers, without the need for a separate Tailscale container for each service.

## Features

- Proxies traffic to virtual Tailscale addresses using Docker container labels.
- No need to spin up a dedicated Tailscale container for every service.
- No need to configure virtual hosts in Tailscale network.
- Automatically supports Tailscale/LetsEncrypt certificates.
- Supports multiple schemes (HTTP, HTTPS).
- Easy configuration using Docker labels.
- Lightweight, Docker-based architecture.

## Requirements

Before using this application, make sure you have:

- [Tailscale](https://tailscale.com/) installed and configured on your host machine.
- [Docker](https://www.docker.com/) installed and running.

## Configuration

This application scans running Docker containers for specific labels to configure proxies to Tailscale virtual addresses.

### Example Labels for a Docker Container

Add the following labels to the Docker containers you wish to proxy:

```yaml
labels:
  - "tsdproxy.enabled=true"
  - "tsdproxy.url=https://example"
  - "tsdproxy.container_port=80"
```

- `tsdproxy.enabled`: Set to `true` to indicate that this container should be proxied.
- `tsdproxy.url`: The URL of the virtual Tailscale hostname that will be the proxy. (defaults are https and the container hostname). You only need to set the subdomain, TsDProxy will automatically append the Tailscale domain.
- `tsdproxy.container_port`: The port on the container. (Container first exposed port by default)

## Running the Tailscale Docker Proxy

To run the TsDProxy itself, use the following Docker command:

```bash
docker run -d --name tsdproxy -v /var/run/docker.sock:/var/run/docker.sock ghcr.io/almeidapaulopt/tsdproxy:latest
```

- `-v /var/run/docker.sock:/var/run/docker.sock`: This gives the proxy app access to the Docker daemon so it can monitor and interact with your containers.

## Example Docker Compose Setup

Here’s an example of how you can configure your services using Docker Compose:

TsDProxy docker-compose.yaml

```yaml
services:
  tailscale-docker-proxy:
    image: ghcr.io/almeidapaulopt/tsdproxy:latest
    container_name: tailscale-docker-proxy
    ports:
      - "80:8080"
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
    restart: always
```

YourService docker-compose.yaml

```yaml
services:
  my-service:
    image: my-service-image
    labels:
      - "tsdproxy.enabled=true"
    ports:
      - "8080:8080"
```

### How It Works

- **Labels**: The app looks for Docker containers with `tsdproxy.enabled=true` to create a proxy.
- **Tailscale Virtual Hostname**: It uses the virtual Tailscale hostname specified in the labels to route traffic to the correct containers.
- **Dynamic Proxy Creation**: The application automatically sets up proxies as new containers start and removes them when containers are stopped.

## License

This project is licensed under the MIT License. See the [LICENSE](LICENSE) file for details.

## Contributing

Contributions are welcome! Feel free to open issues or submit pull requests to help improve the app.
