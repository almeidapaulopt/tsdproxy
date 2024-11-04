---
title: TSDPRoxy
layout: hextra-home
---
{{< hextra/hero-container image="/images/tsdproxy.svg" >}}
{{< hextra/hero-badge >}}
  <div class="hx-w-2 hx-h-2 hx-rounded-full hx-bg-primary-400"></div>
  <span>Free, open source</span>
  {{< icon name="arrow-circle-right" attributes="height=14" >}}
{{< /hextra/hero-badge >}}

<div class="hx-mt-6 hx-mb-6">
{{< hextra/hero-headline >}}
  Very simple proxy&nbsp;<br class="sm:hx-block hx-hidden" />with Tailscale and Docker
{{< /hextra/hero-headline >}}
</div>

<div class="hx-mb-12">
{{< hextra/hero-subtitle >}}
  Fast, simple and easy&nbsp;<br class="sm:hx-block hx-hidden" />for virtual services in Tailscale
{{< /hextra/hero-subtitle >}}
</div>

<div class="hx-mb-6">
{{< hextra/hero-button text="Get Started" link="docs" >}}
</div>

{{< /hextra/hero-container >}}

<div class="hx-mt-6"></div>

{{< hextra/feature-grid >}}
  {{< hextra/feature-card
    title="Easy to Use"
    subtitle="Proxies traffic to virtual Tailscale addresses using Docker container labels"
  >}}
  {{< hextra/feature-card
    title="Lightweight as a Feather"
    subtitle="No need to spin up a dedicated Tailscale container for every service."
  >}}
  {{< hextra/feature-card
    title="Work saver"
    subtitle="No need to configure virtual hosts in Tailscale network."
  >}}
  {{< hextra/feature-card
    title="Automatically supports TLS"
    subtitle="Automatically supports Tailscale/LetsEncrypt certificates with MagicDNS."
  >}}
  {{< hextra/feature-card
    title="Free"
    subtitle="TSDPRoxy is free, open source and always will be."
  >}}
  {{< hextra/feature-card
    title="And More..."
    icon="sparkles"
    subtitle="More features are coming."
  >}}
{{< /hextra/feature-grid >}}
