---
title: Providers
prev: /docs/v2/serverconfig
weight: 3
---

Providers are the sources of services that TSDProxy proxies to your Tailscale network.

- **Docker** discovers containers via labels and the Docker event stream
- **Lists** reads static YAML files with target URLs (supports non-Docker services)

{{< cards >}}
  {{< card link="docker" title="Docker" icon="view-boards"
    subtitle="Auto-discover containers by label"
  >}}
  {{< card link="docker-reference" title="Docker Labels Reference" icon="clipboard"
    subtitle="Quick reference for all labels and port syntax"
  >}}
  {{< card link="lists" title="Lists" icon="server"
    subtitle="Static YAML proxy lists for any service"
  >}}
{{< /cards >}}
