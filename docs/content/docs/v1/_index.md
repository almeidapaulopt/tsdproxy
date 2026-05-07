---
linkTitle: "Legacy v1 Docs"
title: Legacy Documentation (v1)
weight: 900
---

> [!CAUTION]
> This is the legacy v1 documentation. For the current documentation, see the [main docs](/docs/).

👋 Welcome to the TSDProxy v1 (legacy) documentation!

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

TSDProxy differs from other Tailscale proxies in that it does not require a separate Tailscale container per service.

![how tsdproxy works](/images/tsdproxy-compare.svg)

## Features

- **Easy to Use** - Creates virtual Tailscale addresses using Docker container labels.
- **Really Easy to Use** - Creates virtual Tailscale addresses using a proxy list.
- **Lightweight** - No need to spin up a dedicated Tailscale container for every service.
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
{{< /cards >}}
