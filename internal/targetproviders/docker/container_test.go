// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package docker

import (
	"net/netip"
	"net/url"
	"testing"

	ctypes "github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/rs/zerolog"
)

func newTestContainer(targetHostname string, containerIPs []netip.Addr, ports map[string]string) *container {
	return &container{
		log:                   zerolog.Nop(),
		id:                    "test-container-id",
		name:                  "test-container",
		targetProviderName:    "local",
		defaultTargetHostname: targetHostname,
		ipAddress:             containerIPs,
		ports:                 ports,
		networkMode:           ctypes.NetworkMode("bridge"),
		autodetect:            false,
	}
}

func TestGetTargetURL_HTTPWithPublishedPort(t *testing.T) {
	c := newTestContainer("host.docker.internal", []netip.Addr{netip.MustParseAddr("172.17.0.5")}, map[string]string{
		"3000": "32768",
	})

	inputURL, _ := url.Parse("http://0.0.0.0:3000")
	result, err := c.getTargetURL(inputURL, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Scheme != "http" {
		t.Errorf("scheme: got %q, want \"http\"", result.Scheme)
	}
	if result.Host != "host.docker.internal:32768" {
		t.Errorf("host: got %q, want \"host.docker.internal:32768\"", result.Host)
	}
}

func TestGetTargetURL_TCPWithPublishedPort(t *testing.T) {
	c := newTestContainer("host.docker.internal", []netip.Addr{netip.MustParseAddr("172.17.0.5")}, map[string]string{
		"22": "8222",
	})

	inputURL, _ := url.Parse("tcp://0.0.0.0:22")
	result, err := c.getTargetURL(inputURL, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// TCP now follows the same path as HTTP: published port via defaultTargetHostname.
	if result.Scheme != "tcp" {
		t.Errorf("scheme: got %q, want \"tcp\"", result.Scheme)
	}
	if result.Host != "host.docker.internal:8222" {
		t.Errorf("host: got %q, want \"host.docker.internal:8222\" (published port)", result.Host)
	}
}

func TestGetTargetURL_TCPFallbackUsesContainerIP(t *testing.T) {
	c := newTestContainer("", []netip.Addr{netip.MustParseAddr("172.17.0.5")}, map[string]string{})

	inputURL, _ := url.Parse("tcp://0.0.0.0:22")
	result, err := c.getTargetURL(inputURL, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// When no published port and no defaultTargetHostname,
	// resolvePublished fails and resolveDirectIP falls back to container IP.
	if result.Scheme != "tcp" {
		t.Errorf("scheme: got %q, want \"tcp\"", result.Scheme)
	}
	if result.Host != "172.17.0.5:22" {
		t.Errorf("host: got %q, want \"172.17.0.5:22\" (container IP fallback)", result.Host)
	}
}

func TestResolveNonHTTPDirect_SkipsHTTP(t *testing.T) {
	c := newTestContainer("host.docker.internal", []netip.Addr{netip.MustParseAddr("172.17.0.5")}, map[string]string{})

	inputURL, _ := url.Parse("http://0.0.0.0:80")
	u, ok := c.resolveDirectIP(inputURL, "80")
	if ok {
		t.Errorf("resolveDirectIP should skip HTTP, got URL %s", u)
	}
}

func TestResolveNonHTTPDirect_SkipsHTTPS(t *testing.T) {
	c := newTestContainer("host.docker.internal", []netip.Addr{netip.MustParseAddr("172.17.0.5")}, map[string]string{})

	inputURL, _ := url.Parse("https://0.0.0.0:443")
	u, ok := c.resolveDirectIP(inputURL, "443")
	if ok {
		t.Errorf("resolveDirectIP should skip HTTPS, got URL %s", u)
	}
}

func TestResolveNonHTTPDirect_TCPWithNoIP(t *testing.T) {
	c := newTestContainer("host.docker.internal", []netip.Addr{}, map[string]string{})

	inputURL, _ := url.Parse("tcp://0.0.0.0:22")
	u, ok := c.resolveDirectIP(inputURL, "22")
	if ok {
		t.Errorf("resolveDirectIP should return false with no container IPs, got URL %s", u)
	}
}

func TestResolvePublished_WithPublishedPort(t *testing.T) {
	c := newTestContainer("host.docker.internal", []netip.Addr{netip.MustParseAddr("172.17.0.5")}, map[string]string{})

	inputURL, _ := url.Parse("http://0.0.0.0:80")
	u, ok := c.resolvePublished(inputURL, "32768", "80")
	if !ok {
		t.Fatal("expected resolvePublished to succeed with published port")
	}
	if u.Host != "host.docker.internal:32768" {
		t.Errorf("host: got %q, want \"host.docker.internal:32768\"", u.Host)
	}
}

func TestResolvePublished_FallbackToInternalPort(t *testing.T) {
	c := newTestContainer("host.docker.internal", []netip.Addr{netip.MustParseAddr("172.17.0.5")}, map[string]string{})

	inputURL, _ := url.Parse("http://0.0.0.0:80")
	u, ok := c.resolvePublished(inputURL, "", "80")
	if !ok {
		t.Fatal("expected resolvePublished to succeed with internal port fallback")
	}
	if u.Host != "host.docker.internal:80" {
		t.Errorf("host: got %q, want \"host.docker.internal:80\"", u.Host)
	}
}

func TestResolvePublished_NoPortsReturnsFalse(t *testing.T) {
	c := newTestContainer("host.docker.internal", []netip.Addr{}, map[string]string{})

	inputURL, _ := url.Parse("http://0.0.0.0:80")
	_, ok := c.resolvePublished(inputURL, "", "")
	if ok {
		t.Error("expected resolvePublished to fail with no ports and no hostname")
	}
}

func TestResolvePublished_NoPortsNoHostnameReturnsFalse(t *testing.T) {
	c := newTestContainer("", []netip.Addr{}, map[string]string{})

	inputURL, _ := url.Parse("http://0.0.0.0:80")
	_, ok := c.resolvePublished(inputURL, "", "80")
	if ok {
		t.Error("expected resolvePublished to fail with internal port but no hostname")
	}
}

func TestResolvePublished_PublishedPortNoHostnameReturnsFalse(t *testing.T) {
	c := newTestContainer("", []netip.Addr{netip.MustParseAddr("172.17.0.5")}, map[string]string{
		"22": "8222",
	})

	inputURL, _ := url.Parse("tcp://0.0.0.0:22")
	_, ok := c.resolvePublished(inputURL, "8222", "22")
	if ok {
		t.Error("expected resolvePublished to fail with published port but no hostname")
	}
}

func TestGetTargetURL_TCPPublishedPortNoHostnameFallsBackToContainerIP(t *testing.T) {
	c := newTestContainer("", []netip.Addr{netip.MustParseAddr("172.17.0.5")}, map[string]string{
		"22": "8222",
	})

	inputURL, _ := url.Parse("tcp://0.0.0.0:22")
	result, err := c.getTargetURL(inputURL, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// resolvePublished fails (no hostname), resolveDirectIP falls back to container IP.
	if result.Scheme != "tcp" {
		t.Errorf("scheme: got %q, want \"tcp\"", result.Scheme)
	}
	if result.Host != "172.17.0.5:22" {
		t.Errorf("host: got %q, want \"172.17.0.5:22\" (container IP fallback)", result.Host)
	}
}

func TestSetContainerNetwork_DeterministicOrderByNetworkName(t *testing.T) {
	c := &container{
		log:                  zerolog.Nop(),
		defaultBridgeAddress: netip.Addr{},
	}

	dcontainer := ctypes.InspectResponse{
		NetworkSettings: &ctypes.NetworkSettings{
			Networks: map[string]*network.EndpointSettings{
				"network-bravo": {IPAddress: netip.MustParseAddr("10.0.2.5"), Gateway: netip.MustParseAddr("10.0.2.1")},
				"network-alpha": {IPAddress: netip.MustParseAddr("10.0.1.5"), Gateway: netip.MustParseAddr("10.0.1.1")},
				"network-charlie": {IPAddress: netip.MustParseAddr("10.0.3.5"), Gateway: netip.MustParseAddr("10.0.3.1")},
			},
		},
	}

	c.setContainerNetwork(dcontainer)

	if len(c.ipAddress) != 3 {
		t.Fatalf("expected 3 IPs, got %d", len(c.ipAddress))
	}

	// Sorted by name: alpha, bravo, charlie
	if c.ipAddress[0].String() != "10.0.1.5" {
		t.Errorf("ipAddress[0]: got %q, want \"10.0.1.5\" (network-alpha, first alphabetically)", c.ipAddress[0])
	}
	if c.ipAddress[1].String() != "10.0.2.5" {
		t.Errorf("ipAddress[1]: got %q, want \"10.0.2.5\" (network-bravo)", c.ipAddress[1])
	}
	if c.ipAddress[2].String() != "10.0.3.5" {
		t.Errorf("ipAddress[2]: got %q, want \"10.0.3.5\" (network-charlie)", c.ipAddress[2])
	}
}

func TestSetContainerNetwork_PrefersGatewayMatch(t *testing.T) {
	c := &container{
		log:                  zerolog.Nop(),
		defaultBridgeAddress: netip.MustParseAddr("10.0.1.1"),
	}

	dcontainer := ctypes.InspectResponse{
		NetworkSettings: &ctypes.NetworkSettings{
			Networks: map[string]*network.EndpointSettings{
				"network-bravo": {IPAddress: netip.MustParseAddr("10.0.2.5"), Gateway: netip.MustParseAddr("10.0.2.1")},
				"network-alpha": {IPAddress: netip.MustParseAddr("10.0.1.5"), Gateway: netip.MustParseAddr("10.0.1.1")},
			},
		},
	}

	c.setContainerNetwork(dcontainer)

	if len(c.ipAddress) != 2 {
		t.Fatalf("expected 2 IPs, got %d", len(c.ipAddress))
	}

	// network-alpha's gateway (10.0.1.1) matches defaultBridgeAddress,
	// so it should be promoted to [0] despite both being sorted alphabetically.
	if c.ipAddress[0].String() != "10.0.1.5" {
		t.Errorf("ipAddress[0]: got %q, want \"10.0.1.5\" (gateway-matched network)", c.ipAddress[0])
	}
	if c.ipAddress[1].String() != "10.0.2.5" {
		t.Errorf("ipAddress[1]: got %q, want \"10.0.2.5\" (non-matching network)", c.ipAddress[1])
	}
}

func TestSetContainerNetwork_GatewayMatchOverridesAlphaSort(t *testing.T) {
	c := &container{
		log:                  zerolog.Nop(),
		defaultBridgeAddress: netip.MustParseAddr("172.18.0.1"),
	}

	dcontainer := ctypes.InspectResponse{
		NetworkSettings: &ctypes.NetworkSettings{
			Networks: map[string]*network.EndpointSettings{
				"aaa-network": {IPAddress: netip.MustParseAddr("10.0.1.5"), Gateway: netip.MustParseAddr("10.0.1.1")},
				"zzz-network": {IPAddress: netip.MustParseAddr("172.18.0.42"), Gateway: netip.MustParseAddr("172.18.0.1")},
			},
		},
	}

	c.setContainerNetwork(dcontainer)

	if len(c.ipAddress) != 2 {
		t.Fatalf("expected 2 IPs, got %d", len(c.ipAddress))
	}

	// zzz-network's gateway matches defaultBridgeAddress, so it should be [0]
	if c.ipAddress[0].String() != "172.18.0.42" {
		t.Errorf("ipAddress[0]: got %q, want \"172.18.0.42\" (gateway-matched network)", c.ipAddress[0])
	}
	// aaa-network is the non-matching fallback
	if c.ipAddress[1].String() != "10.0.1.5" {
		t.Errorf("ipAddress[1]: got %q, want \"10.0.1.5\" (non-matching network)", c.ipAddress[1])
	}
}

func TestSetContainerNetwork_EmptyIPsSkipped(t *testing.T) {
	c := &container{
		log:                  zerolog.Nop(),
		defaultBridgeAddress: netip.Addr{},
	}

	dcontainer := ctypes.InspectResponse{
		NetworkSettings: &ctypes.NetworkSettings{
			Networks: map[string]*network.EndpointSettings{
				"network-no-ip": {IPAddress: netip.Addr{}, Gateway: netip.MustParseAddr("10.0.1.1")},
				"network-with-ip": {IPAddress: netip.MustParseAddr("10.0.2.5"), Gateway: netip.MustParseAddr("10.0.2.1")},
			},
		},
	}

	c.setContainerNetwork(dcontainer)

	if len(c.ipAddress) != 1 {
		t.Fatalf("expected 1 IP, got %d: %v", len(c.ipAddress), c.ipAddress)
	}
	if c.ipAddress[0].String() != "10.0.2.5" {
		t.Errorf("ipAddress[0]: got %q, want \"10.0.2.5\"", c.ipAddress[0])
	}
	if len(c.gateways) != 2 {
		t.Errorf("expected 2 gateways, got %d", len(c.gateways))
	}
}

func TestResolveNonHTTPDirect_SkipsHostNetwork(t *testing.T) {
	c := &container{
		log:                   zerolog.Nop(),
		id:                    "test-container-id",
		defaultTargetHostname: "127.0.0.1",
		ipAddress:             []netip.Addr{netip.MustParseAddr("192.168.1.100")},
		networkMode:           ctypes.NetworkMode("host"),
	}

	inputURL, _ := url.Parse("tcp://0.0.0.0:22")
	u, ok := c.resolveDirectIP(inputURL, "22")
	if ok {
		t.Errorf("resolveDirectIP should skip host-network containers, got URL %s", u)
	}
}

func TestGetTargetURL_HostNetworkResolvesViaPublishedPath(t *testing.T) {
	c := &container{
		log:                   zerolog.Nop(),
		id:                    "test-container-id",
		name:                  "test-container",
		targetProviderName:    "local",
		defaultTargetHostname: "host.docker.internal",
		networkMode:           ctypes.NetworkMode("host"),
		ports:                 map[string]string{},
		autodetect:            false,
	}

	inputURL, _ := url.Parse("http://0.0.0.0:8080")
	result, err := c.getTargetURL(inputURL, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Host != "host.docker.internal:8080" {
		t.Errorf("host: got %q, want \"host.docker.internal:8080\"", result.Host)
	}
}

func TestGetTargetURL_HostNetworkWorksWithoutBridgeAddress(t *testing.T) {
	c := &container{
		log:                   zerolog.Nop(),
		id:                    "test-container-id",
		name:                  "test-container",
		targetProviderName:    "local",
		defaultTargetHostname: "172.17.0.1",
		defaultBridgeAddress:  netip.Addr{},
		networkMode:           ctypes.NetworkMode("host"),
		ports:                 map[string]string{},
		autodetect:            false,
	}

	inputURL, _ := url.Parse("http://0.0.0.0:8080")
	result, err := c.getTargetURL(inputURL, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Host != "172.17.0.1:8080" {
		t.Errorf("host: got %q, want \"172.17.0.1:8080\"", result.Host)
	}
}

func TestGetTargetURL_HostNetworkNoHostnameFails(t *testing.T) {
	c := &container{
		log:                   zerolog.Nop(),
		id:                    "test-container-id",
		name:                  "test-container",
		targetProviderName:    "local",
		defaultTargetHostname: "",
		networkMode:           ctypes.NetworkMode("host"),
		ports:                 map[string]string{},
		autodetect:            false,
	}

	inputURL, _ := url.Parse("http://0.0.0.0:8080")
	_, err := c.getTargetURL(inputURL, false)
	if err == nil {
		t.Error("expected error when host-network container has no hostname")
	}
}
