---
title: Frequently Asked Questions
weight: 450
prev: /docs/troubleshooting
next: /docs/changelog
---

## What is TSDProxy?

TSDProxy is a reverse proxy that automatically creates Tailscale machines for
your Docker containers (or any service listed in a proxy list file). Add a label
or a config entry, and your service gets its own Tailscale address with automatic
HTTPS. No extra Tailscale containers, no manual virtual hosts.

See the [introduction]({{< ref "/docs/_index" >}}) for a full overview.

## Do I need a separate Tailscale container for each service?

No. That's exactly the problem TSDProxy solves. One TSDProxy instance manages
all your proxies. Each service gets its own Tailscale machine without running a
separate Tailscale sidecar.

## Is TSDProxy free?

Yes. TSDProxy is open source under the [MIT license](https://github.com/almeidapaulopt/tsdproxy/blob/main/LICENSE).
You do need a [Tailscale](https://tailscale.com/) account (or Headscale), which
has its own pricing.

## What's the difference between v1 and v2?

Version 2 adds multi-port support per proxy, OAuth authentication, TCP proxying
(SSH, databases), identity headers, Funnel support, and a real-time dashboard.
Some v1 configuration keys changed. See the
[upgrading guide]({{< ref "/docs/upgrading/from-v1" >}}) for migration details.

## Can I use TSDProxy without Docker?

Yes. You can run TSDProxy as a standalone binary and use the
[Lists provider]({{< ref "/docs/providers/lists" >}}) to define proxies by
IP address or hostname. See the
[standalone deployment]({{< ref "/docs/deployment/standalone" >}}) page for
instructions.

## Does TSDProxy work with Headscale?

Yes. Set the `controlUrl` in your Tailscale provider configuration to point to
your Headscale instance:

```yaml {filename="tsdproxy.yaml"}
tailscale:
  providers:
    default:
      controlUrl: https://headscale.example.com
```

## Do I need a Tailscale account?

Yes. TSDProxy creates machines in your tailnet, so you need either a Tailscale
account or a self-hosted Headscale server. TSDProxy handles the machine
registration, but the account itself is yours.

## What authentication method should I use?

**OAuth is recommended for production.** It automates authentication, supports
tags, and works with `preventDuplicates`. AuthKeys are simpler for quick setups
but don't support the Tailscale API features. See
[Authentication Methods]({{< ref "/docs/security/auth-methods" >}}) for a
comparison.

> [!TIP]
> If you just want to try TSDProxy, you can start without any auth method.
> Proxies will wait for manual authentication through the dashboard. This is
> fine for testing but tedious for more than a couple of services.

## Why is my proxy stuck at "Authenticating"?

The proxy needs credentials to register with Tailscale. You can fix this by
configuring OAuth or an AuthKey in your Tailscale provider, or by clicking the
proxy in the [dashboard]({{< ref "/docs/advanced/dashboard" >}}) to
authenticate manually. Check the logs for specific errors.

## Can I use different auth keys for different containers?

Yes. You can set `tsdproxy.authkey` (or `tsdproxy.authkeyfile`) as a per-container
label, or define multiple Tailscale providers in your config, each with its own
credentials. Then assign providers to containers with the `tsdproxy.proxyprovider`
label.

## How does TSDProxy reach my containers?

TSDProxy connects to the Docker daemon via the socket (or a remote host) and
routes traffic to container IPs on the Docker network. The
`targetHostname` setting (typically `host.docker.internal`) tells TSDProxy where
to reach the Docker host. See the
[Docker provider]({{< ref "/docs/providers/docker" >}}) docs for details.

## My container uses `network_mode: host`

Host-mode containers don't have Docker port mappings, so TSDProxy can't
auto-detect the target. Use `tsdproxy.autodetect: "false"` or add
`no_autodetect` to the port label. See
[host network mode]({{< ref "/docs/advanced/host-mode" >}}) for examples.

## Can I proxy services not running in Docker?

Yes. The [Lists provider]({{< ref "/docs/providers/lists" >}}) lets you
define proxies with arbitrary IP addresses or hostnames as targets. No Docker
required.

## Can I access my proxies from the public internet?

Yes, using [Tailscale Funnel]({{< ref "/docs/security/funnel" >}}). Add the
`tailscale_funnel` option to your port configuration and enable Funnel in your
Tailscale ACL.

> [!CAUTION]
> Funnel bypasses Tailscale authentication. Anyone on the internet can reach
> your service. Make sure your backend has its own authentication.

## Can I proxy non-HTTP services?

Yes. TSDProxy supports raw TCP proxying for SSH, databases (PostgreSQL, MySQL,
Redis), gRPC, and any other TCP protocol. See
[TCP Proxy & SSH]({{< ref "/docs/advanced/tcp-proxy" >}}) for configuration
examples.

## How do I expose the TSDProxy dashboard via Tailscale?

Add labels to the TSDProxy container itself (or add a Lists entry pointing to
`http://127.0.0.1:8080`). The [dashboard]({{< ref "/docs/advanced/dashboard" >}})
page has step-by-step instructions for both methods.

## Can I run multiple TSDProxy instances?

Yes. Check the [scenarios]({{< ref "/docs/scenarios" >}}) section for common
deployment patterns with multiple instances and Tailscale accounts.

## How do I use custom Tailscale tags?

Tags require OAuth authentication. Define tags in your Tailscale provider config
(to apply to all services) or per-proxy via the `tsdproxy.tailscale.tags` label.
See the [tags section]({{< ref "/docs/advanced/tailscale#tags" >}}) for
details.

## Does TSDProxy support load balancing?

Not yet. The internal data structures already support multiple targets per port,
but only the first is used today. Round-robin load balancing is on the
[roadmap]({{< ref "/docs/advanced/roadmap#multi-target-load-balancing" >}}).

## What happens if TSDProxy goes down?

Your Tailscale machines stay registered in your tailnet, but traffic won't reach
your services until TSDProxy comes back up. No data is lost. TSDProxy reconnects
to existing machines using the data stored in its `dataDir`.

## What happens if I lose the data directory?

TSDProxy creates new machines on startup. The old machines become stale entries
in your tailnet, sometimes with a `-1` suffix. You can enable `preventDuplicates`
to auto-clean offline duplicates (requires OAuth). The safest approach is keeping
the `dataDir` on a persistent Docker volume.

> [!NOTE]
> `preventDuplicates` deletes devices from your tailnet. Read the
> [prevent duplicates section]({{< ref "/docs/advanced/tailscale#prevent-duplicate-machines" >}})
> before enabling it.

## Can I run TSDProxy in production?

Version 2 is in beta but is already used by many people. Back up your `dataDir`
volume and test your setup before relying on it. File bugs on
[GitHub Issues](https://github.com/almeidapaulopt/tsdproxy/issues) if you run
into problems.
