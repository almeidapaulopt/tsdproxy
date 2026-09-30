---
title: Standalone Deployment
prev: /docs/deployment
weight: 1
---

TSDProxy can be deployed as a standalone binary.

## Download

Download from [GitHub Releases](https://github.com/almeidapaulopt/tsdproxy/releases).
Available for `linux/amd64`, `linux/arm64`, `darwin/amd64`, `darwin/arm64`, `windows/amd64`.

## Running

```bash
tsdproxy --config /etc/tsdproxy/tsdproxy.yaml
```

## Systemd Service

```ini
[Unit]
Description=TSDProxy
After=network.target docker.service
Requires=docker.service

[Service]
Type=simple
ExecStart=/usr/local/bin/tsdproxy --config /etc/tsdproxy/tsdproxy.yaml
Restart=always
RestartSec=10

[Install]
WantedBy=multi-user.target
```

## Requirements

- Docker daemon accessible (for Docker provider)
- Port 8080 available for Dashboard (configurable via `http.port`)

> [!NOTE]
> Standalone mode is useful for running TSDProxy directly on a host without
> Docker, or for development. Most users should use the Docker deployment
> described in [Getting Started]({{< ref "/docs/v3/getting-started" >}}).
