// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package tailscale

// SharedSNIExposure implements the traffic exposure strategy for shared DNS/TLS mode.
// Multiple proxies share one Tailscale node (tsnet.Server) with SNI-based routing.
//
// Traffic flow:
//
//	Client → Tailscale network → tsnet.Listener → PortRouter → VirtualListener → reverse proxy → container
//
// Features:
//   - SNI hostname-based routing for HTTPS
//   - HTTP Host-header routing for HTTP
//   - Direct TCP port mapping (one domain per port)
//   - UDP packet routing
//   - Ref-counted tsnet.Server lifecycle (auto-stop on idle)
//   - HTTPS-only for SNI-routed ports (SNI requires TLS ClientHello)
//
// This mode is selected when `shared: true` is set in the provider config.
type SharedSNIExposure = SharedServer
