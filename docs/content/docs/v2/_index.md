---
linkTitle: "Documentation v2"
title: Introduction
weight: 400 
---

👋 Welcome to the TSDProxy documentation!

> [!CAUTION]
> Version 2 still in beta, but it's already available for testing.
>
> As a beta version, it may have some bugs, missing features, documentation errors,
> or other issues.

## What is TSDProxy?

TSDProxy is an application that automatically creates a proxy to
virtual addresses in your Tailscale network.
Easy to configure and deploy, based on Docker container labels or a simple proxy
list file.
It simplifies traffic redirection to services running inside Docker containers,
without the need for a separate Tailscale container for each service.

> [!NOTE]
> TSDProxy just needs a label in your new docker service or a proxy list file and
> it will be automatically created in your Tailscale network and the proxy will be
> ready to be used.

## Why another proxy?

TSDProxy was created to address the need for a proxy that can handle multiple services
without the need for a dedicated Tailscale container for each service and without configuring
virtual hosts in Tailscale network.

![how tsdproxy works](/images/tsdproxy.svg)

## What's different with TSDProxy?

TSDProxy differs from other Tailscale proxies in that it does not require a separate Tailscale.

![how tsdproxy works](/images/tsdproxy-compare.svg)

## Features

- **Easy to Use** - creates virtual Tailscale addresses using Docker container labels
- **Really Easy to Use** - creates virtual Tailscale addresses using a proxy list
- **Lightweight** -No need to spin up a dedicated Tailscale container for every service.
- **Quick deploy** - No need to configure virtual hosts in Tailscale network.
- **Automatically supports TLS** - Automatically supports Tailscale/LetsEncrypt certificates
with MagicDNS.

## Questions or Feedback?

> [!IMPORTANT]
  TSDProxy is still in active development.
  Have a question or feedback? Feel free to [open an issue](https://github.com/almeidapaulopt/tsdproxy/issues)!

## Next

Dive right into the following section to get started:

{{< cards >}}
  {{< card link="getting-started" title="Getting Started" icon="document-text"
    subtitle="Learn how to get started with TSDProxy"
  >}}

  {{< card link="upgrading" title="Upgrading" icon="arrow-circle-up"
    subtitle="Migrate from v1 to v2"
  >}}
{{< /cards >}}
