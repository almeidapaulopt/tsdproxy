// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package tailscale

import (
	"context"
	"net"

	"github.com/almeidapaulopt/tsdproxy/internal/model"
)

// TrafficExposure defines the contract for how a Tailscale node exposes
// traffic to containers. Each mode (per-proxy, shared SNI, services/VIP)
// implements this interface to handle port listeners, routing, and teardown.
type TrafficExposure interface {
	Start(ctx context.Context, runtime *NodeRuntime, cfg *model.Config) error
	Close(ctx context.Context) error
}

// ListenerExposure is an optional interface for exposures that provide
// protocol-level listeners (HTTP, HTTPS, TCP).
type ListenerExposure interface {
	TrafficExposure
	GetListener(port model.PortConfig) (net.Listener, error)
}

// RawTCPExposure is an optional interface for exposures that provide
// raw TCP listeners (for custom TLS termination).
type RawTCPExposure interface {
	TrafficExposure
	GetRawTCPListener(port model.PortConfig) (net.Listener, error)
}

// PacketExposure is an optional interface for exposures that support
// UDP packet connections.
type PacketExposure interface {
	TrafficExposure
	GetPacketConn(port model.PortConfig) (net.PacketConn, error)
}

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
type PerProxyExposure struct{}

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
type SharedSNIExposure struct{}

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
type ServicesVIPExposure struct{}

// TODO(#tailscale-unification): implement TrafficExposure on PerProxyExposure,
// SharedSNIExposure, and ServicesVIPExposure. When complete, uncomment:
//
//	var _ TrafficExposure    = (*PerProxyExposure)(nil)
//	var _ ListenerExposure   = (*PerProxyExposure)(nil)
//	var _ RawTCPExposure     = (*PerProxyExposure)(nil)
//	var _ PacketExposure     = (*PerProxyExposure)(nil)
//	var _ TrafficExposure    = (*SharedSNIExposure)(nil)
//	var _ ListenerExposure   = (*SharedSNIExposure)(nil)
//	var _ TrafficExposure    = (*ServicesVIPExposure)(nil)
//	var _ ListenerExposure   = (*ServicesVIPExposure)(nil)
