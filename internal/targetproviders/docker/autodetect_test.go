// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package docker

import (
	"errors"
	"net"
	"net/netip"
	"net/url"
	"testing"

	ctypes "github.com/moby/moby/api/types/container"
	"github.com/rs/zerolog"
)

func newAutodetectContainer(hostname string, ips []netip.Addr, gateways []netip.Addr, networkMode ctypes.NetworkMode, defaultBridge netip.Addr) *container {
	return &container{
		log:                  zerolog.Nop(),
		name:                 "test-container",
		hostname:             hostname,
		ipAddress:            ips,
		gateways:             gateways,
		networkMode:          networkMode,
		defaultBridgeAddress: defaultBridge,
	}
}

func TestTryConnectContainer_InternalPortSucceeds(t *testing.T) {
	t.Parallel()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	defer ln.Close()

	port := portFromListener(ln)
	ip := netip.MustParseAddr("127.0.0.1")

	c := newAutodetectContainer(
		"test-host",
		[]netip.Addr{ip},
		nil,
		ctypes.NetworkMode("bridge"),
		netip.Addr{},
	)

	result, err := c.tryConnectContainer("http", port, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Scheme != "http" {
		t.Errorf("scheme: got %q, want %q", result.Scheme, "http")
	}
	if result.Host != net.JoinHostPort("127.0.0.1", port) {
		t.Errorf("host: got %q, want %q", result.Host, net.JoinHostPort("127.0.0.1", port))
	}
}

func TestTryConnectContainer_PublishedPortSucceeds(t *testing.T) {
	t.Parallel()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	defer ln.Close()

	gwPort := portFromListener(ln)
	gw := netip.MustParseAddr("127.0.0.1")

	c := newAutodetectContainer(
		"test-host",
		nil, // no container IPs — internal will fail
		[]netip.Addr{gw},
		ctypes.NetworkMode("bridge"),
		netip.Addr{},
	)

	result, err := c.tryConnectContainer("http", "", gwPort)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Scheme != "http" {
		t.Errorf("scheme: got %q, want %q", result.Scheme, "http")
	}
}

func TestTryConnectContainer_BothFail(t *testing.T) {
	t.Parallel()

	c := newAutodetectContainer(
		"test-host",
		[]netip.Addr{netip.MustParseAddr("192.0.2.1")},
		[]netip.Addr{netip.MustParseAddr("192.0.2.2")},
		ctypes.NetworkMode("bridge"),
		netip.Addr{},
	)

	_, err := c.tryConnectContainer("http", "9999", "9999")
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	var notFound *NoValidTargetFoundError
	if !errorAs(err, &notFound) {
		t.Errorf("error type: got %T, want *NoValidTargetFoundError", err)
	}
}

func TestTryConnectContainer_EmptyInternalPort_SkipsInternal(t *testing.T) {
	t.Parallel()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	defer ln.Close()

	gwPort := portFromListener(ln)
	gw := netip.MustParseAddr("127.0.0.1")

	c := newAutodetectContainer(
		"test-host",
		nil,
		[]netip.Addr{gw},
		ctypes.NetworkMode("bridge"),
		netip.Addr{},
	)

	result, err := c.tryConnectContainer("http", "", gwPort)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Scheme != "http" {
		t.Errorf("scheme: got %q, want %q", result.Scheme, "http")
	}
}

func TestTryConnectContainer_EmptyPublishedPort_SkipsPublished(t *testing.T) {
	t.Parallel()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	defer ln.Close()

	port := portFromListener(ln)
	ip := netip.MustParseAddr("127.0.0.1")

	c := newAutodetectContainer(
		"test-host",
		[]netip.Addr{ip},
		nil,
		ctypes.NetworkMode("bridge"),
		netip.Addr{},
	)

	// Empty publishedPort skips published resolution.
	result, err := c.tryConnectContainer("http", port, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Scheme != "http" {
		t.Errorf("scheme: got %q, want %q", result.Scheme, "http")
	}
}

func TestTryInternalPort_BridgeMode_ContainerIP(t *testing.T) {
	t.Parallel()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	defer ln.Close()

	port := portFromListener(ln)
	ip := netip.MustParseAddr("127.0.0.1")

	c := newAutodetectContainer(
		"test-host",
		[]netip.Addr{ip},
		nil,
		ctypes.NetworkMode("bridge"),
		netip.Addr{},
	)

	result, err := c.tryInternalPort("http", "test-host", port)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Scheme != "http" {
		t.Errorf("scheme: got %q, want %q", result.Scheme, "http")
	}
}

func TestTryInternalPort_HostMode_DefaultBridge(t *testing.T) {
	t.Parallel()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	defer ln.Close()

	port := portFromListener(ln)
	bridgeAddr := netip.MustParseAddr("127.0.0.1")

	c := newAutodetectContainer(
		"test-host",
		nil,
		nil,
		ctypes.NetworkMode("host"),
		bridgeAddr,
	)

	result, err := c.tryInternalPort("http", "test-host", port)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Scheme != "http" {
		t.Errorf("scheme: got %q, want %q", result.Scheme, "http")
	}
}

func TestTryInternalPort_AllFail(t *testing.T) {
	t.Parallel()

	c := newAutodetectContainer(
		"test-host",
		[]netip.Addr{netip.MustParseAddr("192.0.2.1")},
		nil,
		ctypes.NetworkMode("bridge"),
		netip.Addr{},
	)

	_, err := c.tryInternalPort("http", "test-host", "9999")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if err.Error() != ErrNoValidTargetFoundForInternalPorts.Error() {
		t.Errorf("error: got %q, want %q", err.Error(), ErrNoValidTargetFoundForInternalPorts.Error())
	}
}

func TestTryPublishedPort_GatewaySucceeds(t *testing.T) {
	t.Parallel()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	defer ln.Close()

	port := portFromListener(ln)
	gw := netip.MustParseAddr("127.0.0.1")

	c := newAutodetectContainer(
		"test-host",
		nil,
		[]netip.Addr{gw},
		ctypes.NetworkMode("bridge"),
		netip.Addr{},
	)

	result, err := c.tryPublishedPort("http", port)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Scheme != "http" {
		t.Errorf("scheme: got %q, want %q", result.Scheme, "http")
	}
}

func TestTryPublishedPort_AllFail(t *testing.T) {
	t.Parallel()

	c := newAutodetectContainer(
		"test-host",
		nil,
		[]netip.Addr{netip.MustParseAddr("192.0.2.1")},
		ctypes.NetworkMode("bridge"),
		netip.Addr{},
	)

	_, err := c.tryPublishedPort("http", "9999")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if err.Error() != ErrNoValidTargetFoundForPublishedPorts.Error() {
		t.Errorf("error: got %q, want %q", err.Error(), ErrNoValidTargetFoundForPublishedPorts.Error())
	}
}

func TestTryPublishedPort_MultipleGateways_FirstSucceeds(t *testing.T) {
	t.Parallel()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	defer ln.Close()

	port := portFromListener(ln)
	gw := netip.MustParseAddr("127.0.0.1")
	unreachable := netip.MustParseAddr("192.0.2.1")

	c := newAutodetectContainer(
		"test-host",
		nil,
		[]netip.Addr{unreachable, gw},
		ctypes.NetworkMode("bridge"),
		netip.Addr{},
	)

	result, err := c.tryPublishedPort("tcp", port)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Scheme != "tcp" {
		t.Errorf("scheme: got %q, want %q", result.Scheme, "tcp")
	}
}

func TestDial_Success(t *testing.T) {
	t.Parallel()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	defer ln.Close()

	host, port, _ := net.SplitHostPort(ln.Addr().String())

	c := &container{log: zerolog.Nop()}
	if err := c.dial(host, port); err != nil {
		t.Fatalf("dial should succeed on listening port: %v", err)
	}
}

func TestDial_Refused(t *testing.T) {
	t.Parallel()

	c := &container{log: zerolog.Nop()}

	err := c.dial("127.0.0.1", "1")
	if err == nil {
		t.Fatal("dial should fail on refused connection")
	}
}

func portFromListener(ln net.Listener) string {
	_, port, _ := net.SplitHostPort(ln.Addr().String())
	return port
}

func errorAs(err error, target any) bool {
	return errorsAs(err, target)
}

func errorsAs(err error, target any) bool {
	//nolint:exhaustive // simple wrapper
	{
		var e *NoValidTargetFoundError
		switch {
		case errors.As(err, &e):
			if ptr, ok := target.(**NoValidTargetFoundError); ok {
				*ptr = e
				return true
			}
		}
	}
	return false
}

func TestTryConnectContainer_ReturnsValidURL(t *testing.T) {
	t.Parallel()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	defer ln.Close()

	port := portFromListener(ln)
	ip := netip.MustParseAddr("127.0.0.1")

	c := newAutodetectContainer(
		"test-host",
		[]netip.Addr{ip},
		nil,
		ctypes.NetworkMode("bridge"),
		netip.Addr{},
	)

	tests := []struct {
		name   string
		scheme string
	}{
		{"http", "http"},
		{"https", "https"},
		{"tcp", "tcp"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := c.tryConnectContainer(tc.scheme, port, "")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			parsed, parseErr := url.Parse(tc.scheme + "://" + net.JoinHostPort("127.0.0.1", port))
			if parseErr != nil {
				t.Fatalf("failed to parse expected URL: %v", parseErr)
			}
			if result.Scheme != parsed.Scheme {
				t.Errorf("scheme: got %q, want %q", result.Scheme, parsed.Scheme)
			}
			if result.Host != parsed.Host {
				t.Errorf("host: got %q, want %q", result.Host, parsed.Host)
			}
		})
	}
}
