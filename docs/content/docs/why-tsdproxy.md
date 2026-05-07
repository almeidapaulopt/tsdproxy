---
title: Why TSDProxy
weight: 2
prev: /docs/getting-started
next: /docs/providers/docker
---

## The Problem

Exposing Docker containers on a Tailscale network today is harder than it should be. You generally end up with one of these approaches:

**A sidecar per service.** Every container gets its own Tailscale container attached. This works, but it means running N extra containers for N services. Each one needs its own configuration block in your compose file, its own data volume, and its own authentication step. At scale, the YAML gets verbose and the resource overhead adds up.

**Manual `tailscale serve` commands.** You run `tailscale serve` on a single Tailscale node and point it at each service. This keeps the container count down, but you have to configure each backend by hand. If a service moves to a different port or a new container spins up, you run the command again. Nothing is automatic.

**Binding to the Tailscale IP directly.** You grab the Tailscale IP from a node and configure your services to listen on it. This is fragile because IPs can change, there is no automatic cleanup when services go away, and you lose the nice MagicDNS hostnames that make Tailscale pleasant to use.

All three approaches share a common flaw: they put the configuration burden on you, every time you add or remove a service.

## The Solution

TSDProxy takes a different approach. One container watches your Docker daemon, and you opt services in with a single label:

```yaml
labels:
  tsdproxy.enable: "true"
```

That is it. TSDProxy detects the container, creates a Tailscale machine for it, obtains a TLS certificate, and starts reverse-proxying traffic. When the container stops, the machine is cleaned up automatically. No sidecars, no CLI commands, no static IPs.

## Comparison

| Approach | Extra Containers | Config Method | Auto HTTPS | Auto Cleanup | Multi-port | TCP Proxy |
|----------|:----------------:|---------------|:----------:|:------------:|:----------:|:---------:|
| Tailscale Sidecar | 1 per service | Per-service YAML | Yes | Manual | Limited | Manual |
| tailscale serve | 0 | CLI per service | Yes | Manual | Limited | Manual |
| **tsdproxy** | **0** | **Labels and/or YAML** | **Yes** | **Yes** | **Yes** | **Yes** |

> [!NOTE]
> This table reflects the author's best understanding of each project as of early 2025. Check each project's documentation for the most current feature set.

### What "multi-port" means here

Multi-port support means a single proxy can expose more than one port on the same Tailscale machine. For example, you can serve HTTPS on port 443 and proxy SSH on port 22 from the same container, using a single set of labels:

```yaml
labels:
  tsdproxy.enable: "true"
  tsdproxy.name: "myserver"
  tsdproxy.port.1: "443/https:80/http"
  tsdproxy.port.2: "22/tcp:22/tcp"
```

This is useful for services like Home Assistant (HTTP + TCP add-ons), database servers with both a web UI and a raw connection port, or any service that exposes multiple protocols.

## When to Use TSDProxy

### Good fit

- **Homelabs and self-hosted services.** You run a handful (or a few dozen) Docker services at home and want them reachable from your tailnet without manual configuration.
- **Development environments.** Spin up a test service, add a label, share it with a teammate on your tailnet. Tear it down when you are done.
- **Mixed workloads.** You need both HTTP and TCP proxying (web apps plus SSH, databases, or custom protocols) from the same tool.
- **Non-Docker services.** The [list provider]({{< ref "/docs/providers/lists" >}}) lets you expose services that do not run in Docker, using a simple YAML file.

### Not the best fit

- **Production Kubernetes clusters.** If you are running Kubernetes, you already have Ingress controllers and service meshes. Use those instead. TSDProxy is designed for smaller-scale environments.
- **Single-service setups.** If you only need to expose one service, a single Tailscale sidecar might be simpler. TSDProxy shines when the number of services grows.

## Key Differentiators

**Multi-port per proxy.** Each Tailscale machine can expose multiple ports with independent protocols and options. This goes beyond what most label-based proxies offer.

**TCP proxying.** Raw TCP forwarding for SSH, databases, gRPC, and other non-HTTP protocols. See [TCP Proxy & SSH]({{< ref "/docs/advanced/tcp-proxy" >}}) for details.

**Tailscale Funnel support.** Expose a service to the public internet by adding the `tailscale_funnel` option to a port. No separate configuration needed. See [Funnel]({{< ref "/docs/security/funnel" >}}).

**Dashboard.** A real-time web UI shows all your proxies, their status, and authentication state. Useful for monitoring and for completing the initial Tailscale authentication flow. See [Dashboard]({{< ref "/docs/advanced/dashboard" >}}).

**List provider.** Need to proxy something that is not a Docker container? Define it in a YAML file and TSDProxy treats it like any other target. See [Lists]({{< ref "/docs/providers/lists" >}}).

**Dynamic lifecycle.** Containers start, stop, and get removed. TSDProxy reacts in real time. No restarts needed when services change.

**Live config reload.** Change the TSDProxy configuration file and it takes effect without restarting the container. See [Server Configuration]({{< ref "/docs/serverconfig" >}}).

## What TSDProxy Does Not Do

Being upfront about the limitations:

- It is not an Ingress controller. It does not do path-based routing, header-based routing, or traffic splitting.
- It does not handle load balancing across multiple backend instances (on the roadmap).

For most homelab and self-hosted setups, these are not blockers. But they are worth knowing before you commit to the tool.

## Next Steps

{{< cards >}}
  {{< card link="/docs/providers/docker" title="Docker Labels" icon="document-text"
    subtitle="Configure proxies with container labels"
  >}}
  {{< card link="/docs/advanced/tcp-proxy" title="TCP Proxy & SSH" icon="server"
    subtitle="Expose SSH, databases, and raw TCP services"
  >}}
  {{< card link="/docs/examples/jellyfin" title="Examples" icon="template"
    subtitle="Ready-to-use compose files for common services"
  >}}
{{< /cards >}}
