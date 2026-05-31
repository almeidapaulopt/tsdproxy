// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package tailscale

// ServicesVIPExposure implements the traffic exposure strategy for Tailscale VIP Services mode.
// Multiple proxies share one Tailscale node (tsnet.Server) with VIP Service listeners.
//
// Traffic flow:
//
//	Client → Tailscale network → tsnet.ServiceListener → reverse proxy → container port
//
// Features:
//   - VIP Service-based listeners per port
//   - Automatic FQDN assignment via Tailscale
//   - Ref-counted tsnet.Server lifecycle (auto-stop on idle)
//   - No custom domain support (VIP Services assign FQDNs automatically)
//
// This mode is selected when `services: true` is set in the provider config.
type ServicesVIPExposure = ServicesServer
