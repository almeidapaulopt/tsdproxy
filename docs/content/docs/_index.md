---
linkTitle: "Documentation"
title: Introduction
---

👋 Welcome to the TSDProxy documentation!

## What is TSDProxy?

TSDProxy is a Tailscale + Docker application that automatically creates a proxy to
virtual addresses in your Tailscale network based on Docker container labels.
It simplifies traffic redirection to services running inside Docker containers,
without the need for a separate Tailscale container for each service.

{{< callout type="info" >}}
TSDProxy just needs a label in your new docker service and it will be automatically
created in your Tailscale network and the proxy will be ready to be used.

{{< /callout >}}

## Why another proxy?

TSDProxy was created to address the need for a proxy that can handle multiple services
without the need for a dedicated Tailscale container for each service, without configuring
virtual hosts in Tailscale network, without entry configuration in a proxy like Caddy/nginx.

![how tsdproxy works](/images/tsdproxy.svg)

## What's different with TSDProxy?

TSDProxy differs from other Tailscale proxies in that it does not require a separate Tailscale.

![how tsdproxy works](/images/tsdproxy-compare.svg)

## Features

- **Easy to Use** - creates virtual Tailscale addresses using Docker container labels
- **Lightweight** -No need to spin up a dedicated Tailscale container for every service.
- **Quick deploy** - No need to configure virtual hosts in Tailscale network.
- **Automatically supports TLS** - Automatically supports Tailscale/LetsEncrypt certificates
with MagicDNS.

## Questions or Feedback?

{{< callout emoji="❓" >}}
  TSDProxy is still in active development.
  Have a question or feedback? Feel free to [open an issue](https://github.com/almeidapaulopt/tsdproxy/issues)!
{{< /callout >}}

## Next

Dive right into the following section to get started:

{{< cards >}}
  {{< card link="getting-started" title="Getting Started" icon="document-text"
    subtitle="Learn how to get started with TSDProxy"
  >}}
{{< /cards >}}
