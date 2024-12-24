# TsDProxy - Tailscale Proxy

TsDProxy simplifies the process of securely exposing services and Docker containers
to your Tailscale network by automatically creating Tailscale machines for each
tagged container. This allows services to be accessible via unique, secure URLs
without the need for complex configurations or additional Tailscale containers.

## Breaking Changes in 1.0.0

This version changes the configuration method.
Please read the [Documentation](https://almeidapaulopt.github.io/tsdproxy/docs)
for details.

## Docker Images

1. almeidapaulopt/tsdproxy:v1.x.x  - Version 1.x.x
2. almeidapaulopt/tsdproxy:latest  - Latest stable
3. almeidapaulopt/tsdproxy:next    - Latest Release Candidate
4. almeidapaulopt/tsdproxy:dev     - Latest Development Build

## Core Functionality

- **Automatic Tailscale Machine Creation**: For each Docker container tagged
with the appropriate labels, TsDProxy creates a new Tailscale machine.
- **Default Serving**: By default, each service is accessible via
`https://{machine-name}.funny-name.ts.net`, where `{machine-name}` is derived
from your container name or custom label.

## Key Features

- **Simplified Networking**: Eliminates the need for a separate Tailscale
container for each service.
- **Label-Based Configuration**: Easy setup using Docker container labels.
- **Automatic HTTPS**: Leverages Tailscale's built-in LetsEncrypt certificate support.
- **Flexible Protocol Support**: Handles HTTP and HTTPS traffic (defaulting to HTTPS).
- **Lightweight Architecture**: Efficient, Docker-based design for minimal overhead.

## How It Works

TsDProxy operates by creating a seamless integration between your Docker
containers and Tailscale network:

1. **Container Scanning**: TsDProxy continuously monitors your Docker
environment for containers with the `tsdproxy.enable=true` label.
2. **Tailscale Machine Creation**: When a tagged container is detected, TsDProxy
automatically creates a new Tailscale machine for that container.
3. **Hostname Assignment**: The Tailscale machine is assigned a hostname based
on the `tsdproxy.name` label or the container's name.
4. **Port Mapping**: TsDProxy maps the container's internal port to the Tailscale
machine.
5. **Traffic Routing**: Incoming requests to the Tailscale machine are routed to
the appropriate Docker container and port.
6. **Dynamic Management**: As containers start and stop, TsDProxy automatically
creates and removes the corresponding Tailscale machines and routing configurations.

## Full Documentation

- [Official Documentation](https://almeidapaulopt.github.io/tsdproxy/)

## Requirements

Before using this application, make sure you have:

- [Tailscale](https://tailscale.com/) installed and configured on your host machine.
- [Docker](https://www.docker.com/) installed and running.

## License

This project is licensed under the MIT License. See the [LICENSE](LICENSE) file
for details.

## Contributing

Contributions are welcome! Feel free to open issues or submit pull requests to help improve the app.
