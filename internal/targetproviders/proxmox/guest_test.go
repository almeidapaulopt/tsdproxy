// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package proxmox

import (
	"context"
	"fmt"
	"net/netip"
	"strconv"
	"testing"

	"github.com/rs/zerolog"
)

func newTestGuest(notes string, interfaces []GuestInterface, opts ...GuestOption) *guest {
	return newGuest(zerolog.Nop(),
		qemuGuest("qemu/100", 100, "test-guest", statusRunning),
		&GuestConfig{Notes: notes},
		testNotesConfig(notes),
		interfaces,
		opts...,
	)
}

// testNotesConfig parses test notes; notes without tsdproxy data yield an
// empty (enabled) config, mirroring AddTarget's contract where the caller
// has already established enablement.
func testNotesConfig(notes string) *notesConfig {
	cfg, err := parseTsdproxyConfig(notes)
	if err != nil {
		panic(fmt.Sprintf("invalid test notes %q: %v", notes, err))
	}
	if cfg == nil {
		return &notesConfig{Enable: true, Settings: map[string]string{}}
	}
	return cfg
}

func TestSetInterfaces_OrderingAndFilters(t *testing.T) {
	t.Parallel()

	g := newTestGuest("", []GuestInterface{
		{Name: "lo", Addresses: []netip.Addr{netip.MustParseAddr("127.0.0.1")}},
		{Name: "aaa", Addresses: []netip.Addr{netip.MustParseAddr("10.0.1.5")}},
		{Name: "eth0", Addresses: []netip.Addr{
			netip.MustParseAddr("169.254.1.1"),
			netip.MustParseAddr("fd42::5"),
			netip.MustParseAddr("10.0.0.5"),
		}},
	})

	if len(g.ips) != 3 {
		t.Fatalf("expected 3 usable addresses, got %d: %v", len(g.ips), g.ips)
	}
	if g.ips[0].String() != "10.0.0.5" {
		t.Errorf("ips[0]: got %q, want eth0 IPv4 first", g.ips[0])
	}
	if g.ips[1].String() != "10.0.1.5" {
		t.Errorf("ips[1]: got %q, want remaining IPv4", g.ips[1])
	}
	if g.ips[2].String() != "fd42::5" {
		t.Errorf("ips[2]: got %q, want IPv6 last", g.ips[2])
	}
}

func TestGetTargetURL_TargetHostnameOverride(t *testing.T) {
	t.Parallel()

	g := newTestGuest("", nil, withDefaultTargetHostname("192.168.0.1"))
	g.settings = map[string]string{ConfigPort + "443": "443/https:8080/http"}

	pcfg, err := g.newProxyConfig(context.Background(), "qemu/100")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	port, ok := pcfg.Ports["tsdproxy.port.443"]
	if !ok {
		t.Fatal("expected port")
	}
	if got := port.GetFirstTargetString(); got != "http://192.168.0.1:8080" {
		t.Errorf("target: got %q, want http://192.168.0.1:8080", got)
	}
}

func TestGetPorts_FlatLabelGrammar(t *testing.T) {
	t.Parallel()

	g := newTestGuest("tsdproxy:\n  port:\n    443: 443/https:8080/http\n    9090: 9090/tcp:9090/tcp\n",
		[]GuestInterface{{Name: "eth0", Addresses: []netip.Addr{netip.MustParseAddr("10.1.2.3")}}})

	pcfg, err := g.newProxyConfig(context.Background(), "qemu/100")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	flat443, ok := pcfg.Ports["tsdproxy.port.443"]
	if !ok {
		t.Fatal("expected flat 443 port")
	}
	if got := flat443.GetFirstTargetString(); got != "http://10.1.2.3:8080" {
		t.Errorf("443 target: got %q", got)
	}
	flat9090, ok := pcfg.Ports["tsdproxy.port.9090"]
	if !ok {
		t.Fatal("expected flat 9090 port")
	}
	if got := flat9090.GetFirstTargetString(); got != "tcp://10.1.2.3:9090" {
		t.Errorf("9090 target: got %q", got)
	}
}

func TestGetPorts_ListStyleExplicitTargets(t *testing.T) {
	t.Parallel()

	g := newTestGuest(`
tsdproxy:
  ports:
    "443/https":
      targets:
        - "http://10.0.0.9:8080"
        - "http://10.0.0.10:8080"
    "8080/tcp:9090/tcp":
      targets:
        - "tcp://override:1234"
`, nil)

	pcfg, err := g.newProxyConfig(context.Background(), "qemu/100")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	port, ok := pcfg.Ports["443/https"]
	if !ok {
		t.Fatal("expected 443/https port")
	}
	targets := port.GetTargets()
	if len(targets) != 2 {
		t.Fatalf("expected 2 targets, got %v", targets)
	}
	if targets[0].String() != "http://10.0.0.9:8080" || targets[1].String() != "http://10.0.0.10:8080" {
		t.Errorf("targets: got %v", targets)
	}

	// Long label placeholder must be replaced by the explicit target.
	long, ok := pcfg.Ports["8080/tcp:9090/tcp"]
	if !ok {
		t.Fatal("expected long label port")
	}
	if got := long.GetFirstTargetString(); got != "tcp://override:1234" {
		t.Errorf("long label target: got %q, want placeholder replaced", got)
	}
}

func TestGetPorts_ListStyleGeneratedTargets(t *testing.T) {
	t.Parallel()

	g := newTestGuest(`
tsdproxy:
  ports:
    "443/https":
    "8080/tcp:9090/tcp":
    "56000-56001/udp":
`, []GuestInterface{{Name: "eth0", Addresses: []netip.Addr{netip.MustParseAddr("10.2.3.4")}}})

	pcfg, err := g.newProxyConfig(context.Background(), "qemu/100")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	short, ok := pcfg.Ports["443/https"]
	if !ok {
		t.Fatal("expected short label port")
	}
	if got := short.GetFirstTargetString(); got != "http://10.2.3.4:443" {
		t.Errorf("short label generated target: got %q, want http://10.2.3.4:443", got)
	}
	longLabel, ok := pcfg.Ports["8080/tcp:9090/tcp"]
	if !ok {
		t.Fatal("expected long label port")
	}
	if got := longLabel.GetFirstTargetString(); got != "tcp://10.2.3.4:9090" {
		t.Errorf("long label generated target: got %q, want tcp://10.2.3.4:9090", got)
	}
	for _, key := range []string{"56000-56001/udp.range_0", "56000-56001/udp.range_1"} {
		port, ok := pcfg.Ports[key]
		if !ok {
			t.Fatalf("expected range port %q, got %+v", key, pcfg.Ports)
		}
		want := "udp://10.2.3.4:" + strconv.Itoa(port.ProxyPort)
		if got := port.GetFirstTargetString(); got != want {
			t.Errorf("%s target: got %q, want %q", key, got, want)
		}
	}
}

func TestGetPorts_ListStyleGates(t *testing.T) {
	t.Parallel()

	notes := `
tsdproxy:
  ports:
    "443/https":
      tlsValidate: false
      tailscale:
        funnel: true
`

	g := newTestGuest(notes, nil, withDefaultTargetHostname("10.9.9.9")) // gates off
	pcfg, err := g.newProxyConfig(context.Background(), "qemu/100")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	port := pcfg.Ports["443/https"]
	if !port.TLSValidate {
		t.Error("tlsValidate=false must be ignored without allowTlsValidateDisable")
	}
	if port.Tailscale.Funnel {
		t.Error("funnel must be ignored without allowGuestFunnel")
	}

	g2 := newTestGuest(notes, nil, withDefaultTargetHostname("10.9.9.9"),
		withAllowGuestFunnel(true), withAllowTLSValidateDisable(true))
	pcfg2, err := g2.newProxyConfig(context.Background(), "qemu/100")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	port2 := pcfg2.Ports["443/https"]
	if port2.TLSValidate {
		t.Error("tlsValidate=false must be honored with allowTlsValidateDisable")
	}
	if !port2.Tailscale.Funnel {
		t.Error("funnel must be honored with allowGuestFunnel")
	}
}

func TestGetPorts_ListStyleRedirectWithoutTargetsDropped(t *testing.T) {
	t.Parallel()

	g := newTestGuest(`
tsdproxy:
  ports:
    "80/http":
      isRedirect: true
`, nil)

	pcfg, err := g.newProxyConfig(context.Background(), "qemu/100")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pcfg.Ports) != 0 {
		t.Errorf("redirect without targets must be dropped, got %+v", pcfg.Ports)
	}
}

func TestGetPorts_NoAddressNoTargetDropped(t *testing.T) {
	t.Parallel()

	g := newTestGuest(`
tsdproxy:
  ports:
    "443/https":
`, nil) // no interfaces, no targetHostname

	pcfg, err := g.newProxyConfig(context.Background(), "qemu/100")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pcfg.Ports) != 0 {
		t.Errorf("port without resolvable target must be dropped, got %+v", pcfg.Ports)
	}
}

func TestGetProxyHostname(t *testing.T) {
	t.Parallel()

	t.Run("GuestNameLowercased", func(t *testing.T) {
		t.Parallel()
		g := newTestGuest("", nil)
		hostname, err := g.getProxyHostname()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if hostname != "test-guest" {
			t.Errorf("hostname: got %q", hostname)
		}
	})

	t.Run("SettingOverride", func(t *testing.T) {
		t.Parallel()
		g := newTestGuest("tsdproxy:\n  name: Custom-Name\n", nil)
		hostname, err := g.getProxyHostname()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if hostname != "custom-name" {
			t.Errorf("hostname: got %q", hostname)
		}
	})

	t.Run("InvalidOverride", func(t *testing.T) {
		t.Parallel()
		g := newTestGuest("tsdproxy:\n  name: bad_host!\n", nil)
		if _, err := g.getProxyHostname(); err == nil {
			t.Error("expected error for invalid hostname")
		}
	})
}
