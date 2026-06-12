// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package tailscale

import (
	"net"
	"net/netip"

	"tailscale.com/client/local"
	"tailscale.com/tsnet"
)

// TSNetServer abstracts the tsnet.Server methods used by the tailscale proxy provider.
// Production code constructs *tsnet.Server (setting fields like Dir, Logf, Hostname
// before calling methods) and then stores it as this interface.
type TSNetServer interface {
	// Network listeners.
	Listen(network, addr string) (net.Listener, error)
	ListenTLS(network, addr string) (net.Listener, error)
	ListenFunnel(network, addr string, opts ...tsnet.FunnelOption) (net.Listener, error)
	ListenPacket(network, addr string) (net.PacketConn, error)

	// Services (VIP mode).
	ListenService(name string, mode tsnet.ServiceMode) (*tsnet.ServiceListener, error)

	// Identity / info.
	TailscaleIPs() (ip4, ip6 netip.Addr)
	CertDomains() []string

	// Lifecycle.
	Start() error
	Close() error

	// Local client.
	LocalClient() (*local.Client, error)
}

// Compile-time check that *tsnet.Server satisfies TSNetServer.
var _ TSNetServer = (*tsnet.Server)(nil)
