---
title: Configuration Scenarios
prev: /docs/providers
next: /docs/scenarios/1i-2docker-1tailscale
weight: 10
---

Real-world deployment examples for common setups.

{{< cards >}}
  {{< card link="1i-2docker-1tailscale" title="Single Instance, 2 Docker Hosts" icon="server"
    subtitle="One TSDProxy managing containers on two Docker servers with one Tailscale provider"
  >}}
  {{< card link="1i-2docker-3tailscale" title="Single Instance, 3 Providers" icon="server"
    subtitle="One TSDProxy with multiple Tailscale providers for different tags or accounts"
  >}}
  {{< card link="2i-2docker-1tailscale" title="Two Instances, 1 Provider" icon="collection"
    subtitle="Two independent TSDProxy instances sharing one Tailscale auth key"
  >}}
  {{< card link="2i-2docker-3tailscale" title="Two Instances, 3 Providers" icon="collection"
    subtitle="Two TSDProxy instances with per-container provider overrides"
  >}}
  {{< card link="1i-3docker-shared-tailscale" title="Shared Tailscale" icon="server"
    subtitle="Multiple containers sharing one Tailscale machine with custom domains"
  >}}
  {{< card link="1i-1docker-1tailscale-1servarr" title="Servarr + VPN" icon="play"
    subtitle="Prowlarr behind a Gluetun VPN container using network_mode: service:vpn"
  >}}
{{< /cards >}}
