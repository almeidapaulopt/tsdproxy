// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package tailscale

// PerProxyExposure implements the traffic exposure strategy for per-proxy mode.
// Each proxy gets its own Tailscale node (tsnet.Server) with direct port listeners.
//
// Traffic flow:
//
//	Client → Tailscale network → tsnet.Listener → reverse proxy → container port
//
// Features:
//   - Direct listeners per port (HTTP, HTTPS, TCP, UDP)
//   - Funnel support for public internet exposure
//   - TLS cert provisioning via Tailscale
//   - MagicDNS for automatic hostname resolution
//
// This is the default mode when neither `shared` nor `services` is set.
