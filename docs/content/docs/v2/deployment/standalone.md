---
title: Standalone Deployment
prev: /docs/v2/deployment
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

- Docker daemon accessible
- Port 8080 available for Dashboard

> [!NOTE]
> Most users should prefer Docker deployment. Standalone mode is for development.
