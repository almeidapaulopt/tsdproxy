---
title: Providers
prev: /docs/serverconfig
weight: 3
---

Providers are the sources of services that TSDProxy proxies to your Tailscale network.

- **Docker** discovers containers via labels and the Docker event stream
- **Incus** discovers containers and VMs via `user.tsdproxy.*` config keys and the Incus event stream
- **Proxmox** discovers VMs and containers via a `tsdproxy:` YAML block in guest notes, polled from the cluster API
- **Lists** reads static YAML files with target URLs (supports non-Docker services)

{{< cards >}}
  {{< card link="docker" title="Docker" icon="view-boards"
    subtitle="Auto-discover containers by label"
  >}}
  {{< card link="docker-reference" title="Docker Labels Reference" icon="clipboard"
    subtitle="Quick reference for all labels and port syntax"
  >}}
  {{< card link="incus" title="Incus" icon="cube"
    subtitle="Auto-discover containers and VMs by config key"
  >}}
  {{< card link="proxmox" title="Proxmox" icon="chip"
    subtitle="Auto-discover Proxmox VE guests by guest notes"
  >}}
  {{< card link="lists" title="Lists" icon="server"
    subtitle="Static YAML proxy lists for any service"
  >}}
{{< /cards >}}
