# TsDProxy - <ins>T</ins>ail<ins>s</ins>cale <ins>D</ins>ocker <ins>Proxy</ins>

TsDProxy simplifies the process of securely exposing Docker containers to your Tailscale network by automatically creating Tailscale machines for each tagged container. This allows services to be accessible via unique, secure URLs without the need for complex configurations or additional Tailscale containers.

## Core Functionality

- **Automatic Tailscale Machine Creation**: For each Docker container tagged with the appropriate labels, TsDProxy creates a new Tailscale machine.
- **Default Serving**: By default, each service is accessible via `https://{machine-name}.funny-name.ts.net`, where `{machine-name}` is derived from your container name or custom label.

## Key Features

- **Simplified Networking**: Eliminates the need for a separate Tailscale container for each service.
- **Label-Based Configuration**: Easy setup using Docker container labels.
- **Automatic HTTPS**: Leverages Tailscale's built-in LetsEncrypt certificate support.
- **Flexible Protocol Support**: Handles HTTP and HTTPS traffic (defaulting to HTTPS).
- **Lightweight Architecture**: Efficient, Docker-based design for minimal overhead.

## How It Works

TsDProxy operates by creating a seamless integration between your Docker containers and Tailscale network:

1. **Container Scanning**: TsDProxy continuously monitors your Docker environment for containers with the `tsdproxy.enable=true` label.
2. **Tailscale Machine Creation**: When a tagged container is detected, TsDProxy automatically creates a new Tailscale machine for that container.
3. **Hostname Assignment**: The Tailscale machine is assigned a hostname based on the `tsdproxy.name` label or the container's name.
4. **Port Mapping**: TsDProxy maps the container's internal port to the Tailscale machine.
5. **Traffic Routing**: Incoming requests to the Tailscale machine are routed to the appropriate Docker container and port.
6. **Dynamic Management**: As containers start and stop, TsDProxy automatically creates and removes the corresponding Tailscale machines and routing configurations.

## Full Documentation

- [Official Documentation](https://almeidapaulopt.github.io/tsdproxy/)

## Requirements

Before using this application, make sure you have:

- [Tailscale](https://tailscale.com/) installed and configured on your host machine.
- [Docker](https://www.docker.com/) installed and running.

## Configuration

Add the following labels to the Docker containers you wish to proxy:

```yaml
labels:
  - "tsdproxy.enable=true"
  - "tsdproxy.name=example"
  - "tsdproxy.container_port=8080"
  - "tsdproxy.funnel=false"
```

- `tsdproxy.enable` (**required**): Set to `true` to indicate that this container should be proxied.
- `tsdproxy.name` (optional): The machine name to assign to the container (defaults to container's name).
- `tsdproxy.container_port` (optional): The container's internal port you wish to expose (defaults to first exposed port).
- `tsdproxy.funnel` (optional): Set to `true` to enable Tailscale funnel (exposes the container to the public internet).

## Running the Tailscale Docker Proxy

### Docker Run Command

```bash
docker run -d --name tsdproxy -v /var/run/docker.sock:/var/run/docker.sock almeidapaulopt/tsdproxy:latest
```

- `-v /var/run/docker.sock:/var/run/docker.sock`: This gives the proxy app access to the Docker daemon so it can monitor and interact with your containers.

### Docker Compose

#### TsDProxy docker-compose.yaml

```yaml
services:
  tailscale-docker-proxy:
    image: almeidapaulopt/tsdproxy:latest
    container_name: tailscale-docker-proxy
    ports:
      - "80:8080"
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
      - datadir:/data
    restart: unless-stopped
    environment:
      DOCKER_HOST: unix:///var/run/docker.sock
      TSDPROXY_AUTHKEY: tskey-auth-SecretKey
      TSDPROXY_HOSTNAME: 10.0.10.0
      # TSDPROXY_AUTHKEYFILE: /run/secrets/authkey
      # TSDPROXY_DATADIR: /data
      # TSDPROXY_LOG_LEVEL: info
      # TSDPROXY_CONTAINERACCESSLOG: true

    # secrets:
    #   - authkey

# secrets:
#   authkey:
#     file: tsdproxy_authkey.txt

volumes:
  datadir:
```

##### Environment Variables Explanation

| Variable                    | Required | Description                                                                                               |
| --------------------------- | -------- | --------------------------------------------------------------------------------------------------------- |
| DOCKER_HOST                 | Yes      | Path to Docker socket                                                                                     |
| TSDPROXY_AUTHKEY            | Yes      | Your Tailscale authkey (generate in Tailscale web UI)                                                     |
| TSDPROXY_HOSTNAME           | Yes      | LAN IP address or name of docker host machine (cannot use localhost or 127.0.0.1 if using bridge network) |
| TSDPROXY_AUTHKEYFILE        | No       | Path to file containing the authkey (incompatible with Docker Secrets)                                    |
| TSDPROXY_DATADIR            | No       | Custom internal directory for data (defaults to /data)                                                    |
| TSDPROXY_LOG_LEVEL          | No       | Log level (defaults to info)                                                                              |
| TSDPROXY_CONTAINERACCESSLOG | No       | Enable proxy access log for tagged containers (defaults to true)                                          |

#### Example target container using TsDProxy

```yaml
services:
  my-service:
    image: my-service-image
    labels:
      - "tsdproxy.enable=true"
      # - "tsdproxy.name=my-custom-name"
      # - "tsdproxy.container_port=2000"
      # - "tsdproxy.funnel=false"
    ports: # external:internal
      - "8080:80"
      - "8443:443"
      - "8888:2000"
```

##### Labels Explanation

| Label                   | Required | Description                                                            |
| ----------------------- | -------- | ---------------------------------------------------------------------- |
| tsdproxy.enable         | Yes      | Enables TsDProxy for this service                                      |
| tsdproxy.name           | No       | Custom name for the service (defaults to service name)                 |
| tsdproxy.container_port | No       | Specify a different port to be served (defaults to first exposed port) |
| tsdproxy.funnel         | No       | Allows the service to be accessible to the internet if set to true     |

In this example:

- The service's name is `my-service`. It can be changed using the `tsdproxy.name` label.
- The service exposes three ports. By default, TsDProxy will use the internal value of the first port exposed (80 in this case).
- The service is only accessible to the Tailscale network by default. This can be changed using the `tsdproxy.funnel` label.

## Troubleshooting

- **Incorrect Port Mapping**: Ensure `tsdproxy.container_port` matches the target internal port of your container.
- **Tailscale Authentication Issues**: Verify that your Tailscale auth key is valid and correctly configured.
- **Access Problems**: Check that your Tailscale network is properly set up and that the service is running.

## License

This project is licensed under the MIT License. See the [LICENSE](LICENSE) file for details.

## Contributing

Contributions are welcome! Feel free to open issues or submit pull requests to help improve the app.
