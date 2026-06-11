// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package docker

import (
	"context"
	"net"
	"net/netip"
	"testing"

	ctypes "github.com/moby/moby/api/types/container"
	"github.com/rs/zerolog"
)

func TestGetInternalPortLegacy_WithContainerPort(t *testing.T) {
	t.Parallel()

	c := &container{
		log:    zerolog.Nop(),
		labels: map[string]string{LabelContainerPort: "8080"},
	}

	got := c.getInternalPortLegacy()
	if got != "8080" {
		t.Errorf("got %q, want %q", got, "8080")
	}
}

func TestGetInternalPortLegacy_PicksLowestNumericPort(t *testing.T) {
	t.Parallel()

	c := &container{
		log:    zerolog.Nop(),
		labels: map[string]string{},
		ports:  map[string]string{"3000": "32768", "80": "32769", "443": "32770"},
	}

	got := c.getInternalPortLegacy()
	if got != "80" {
		t.Errorf("got %q, want %q (lowest numeric)", got, "80")
	}
}

func TestGetInternalPortLegacy_NumericBeforeNonNumeric(t *testing.T) {
	t.Parallel()

	c := &container{
		log:    zerolog.Nop(),
		labels: map[string]string{},
		ports:  map[string]string{"abc": "1", "22": "2"},
	}

	got := c.getInternalPortLegacy()
	if got != "22" {
		t.Errorf("got %q, want %q (numeric before non-numeric)", got, "22")
	}
}

func TestGetInternalPortLegacy_EmptyPorts(t *testing.T) {
	t.Parallel()

	c := &container{
		log:    zerolog.Nop(),
		labels: map[string]string{},
		ports:  map[string]string{},
	}

	got := c.getInternalPortLegacy()
	if got != "" {
		t.Errorf("got %q, want empty string", got)
	}
}

func TestGetInternalPortLegacy_LabelOverridesPorts(t *testing.T) {
	t.Parallel()

	c := &container{
		log:    zerolog.Nop(),
		labels: map[string]string{LabelContainerPort: "9090"},
		ports:  map[string]string{"80": "32768"},
	}

	got := c.getInternalPortLegacy()
	if got != "9090" {
		t.Errorf("got %q, want %q (label overrides ports)", got, "9090")
	}
}

func TestGetInternalPortLegacy_NonNumericKeysSorted(t *testing.T) {
	t.Parallel()

	c := &container{
		log:    zerolog.Nop(),
		labels: map[string]string{},
		ports:  map[string]string{"zebra": "1", "alpha": "2"},
	}

	got := c.getInternalPortLegacy()
	if got != "alpha" {
		t.Errorf("got %q, want %q (sorted alphabetically)", got, "alpha")
	}
}

func TestGetLegacyPort_DefaultScheme(t *testing.T) {
	t.Parallel()

	ln := mustListenTCP(t)
	defer ln.Close()

	_, port, _ := mustSplitHostPort(ln.Addr().String())

	c := &container{
		log:                   zerolog.Nop(),
		name:                  "test-container",
		labels:                map[string]string{LabelContainerPort: port},
		ports:                 map[string]string{},
		networkMode:           ctypes.NetworkMode("bridge"),
		defaultTargetHostname: "127.0.0.1",
		ipAddress:             []netip.Addr{netip.MustParseAddr("127.0.0.1")},
	}

	result, err := c.getLegacyPort(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.ProxyProtocol != "https" {
		t.Errorf("proxy protocol: got %q, want %q", result.ProxyProtocol, "https")
	}
}

func TestGetLegacyPort_CustomScheme(t *testing.T) {
	t.Parallel()

	ln := mustListenTCP(t)
	defer ln.Close()

	_, port, _ := mustSplitHostPort(ln.Addr().String())

	c := &container{
		log:                   zerolog.Nop(),
		name:                  "test-container",
		labels:                map[string]string{LabelContainerPort: port, LabelScheme: "http"},
		ports:                 map[string]string{},
		networkMode:           ctypes.NetworkMode("bridge"),
		defaultTargetHostname: "127.0.0.1",
		ipAddress:             []netip.Addr{netip.MustParseAddr("127.0.0.1")},
	}

	result, err := c.getLegacyPort(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.ProxyProtocol != "https" {
		t.Errorf("proxy protocol should always be https in legacy mode, got %q", result.ProxyProtocol)
	}
	// Target scheme should be the custom scheme.
	target := result.GetFirstTarget()
	if target == nil {
		t.Fatal("expected non-nil target")
	}
	if target.Scheme != "http" {
		t.Errorf("target scheme: got %q, want %q", target.Scheme, "http")
	}
}

func TestGetLegacyPort_TLSValidate(t *testing.T) {
	t.Parallel()

	ln := mustListenTCP(t)
	defer ln.Close()

	_, port, _ := mustSplitHostPort(ln.Addr().String())

	c := &container{
		log:                   zerolog.Nop(),
		name:                  "test-container",
		labels:                map[string]string{LabelContainerPort: port, LabelTLSValidate: "true"},
		ports:                 map[string]string{},
		networkMode:           ctypes.NetworkMode("bridge"),
		defaultTargetHostname: "127.0.0.1",
		ipAddress:             []netip.Addr{netip.MustParseAddr("127.0.0.1")},
	}

	result, err := c.getLegacyPort(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.TLSValidate {
		t.Error("TLSValidate should be true")
	}
}

func TestGetLegacyPort_Funnel(t *testing.T) {
	t.Parallel()

	ln := mustListenTCP(t)
	defer ln.Close()

	_, port, _ := mustSplitHostPort(ln.Addr().String())

	c := &container{
		log:                   zerolog.Nop(),
		name:                  "test-container",
		labels:                map[string]string{LabelContainerPort: port, LabelFunnel: "true"},
		ports:                 map[string]string{},
		networkMode:           ctypes.NetworkMode("bridge"),
		defaultTargetHostname: "127.0.0.1",
		ipAddress:             []netip.Addr{netip.MustParseAddr("127.0.0.1")},
	}

	result, err := c.getLegacyPort(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.Tailscale.Funnel {
		t.Error("Funnel should be true")
	}
}

func mustListenTCP(t *testing.T) net.Listener {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	return ln
}

func mustSplitHostPort(addr string) (string, string, error) {
	return net.SplitHostPort(addr)
}
