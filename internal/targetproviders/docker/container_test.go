// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package docker

import (
	"context"
	"net/netip"
	"net/url"
	"testing"

	ctypes "github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/rs/zerolog"

	"github.com/almeidapaulopt/tsdproxy/web"
)

var testAssets = web.NewAssets()

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
		assets:                testAssets,
	}
}

func TestGetTargetURL_HTTPWithPublishedPort(t *testing.T) {
	c := newTestContainer("host.docker.internal", []netip.Addr{netip.MustParseAddr("172.17.0.5")}, map[string]string{
		"3000": "32768",
	})

	inputURL, _ := url.Parse("http://0.0.0.0:3000")
	result, err := c.getTargetURL(context.Background(), inputURL, false)
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
	result, err := c.getTargetURL(context.Background(), inputURL, false)
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
	result, err := c.getTargetURL(context.Background(), inputURL, false)
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

func TestResolveContainerIP_NoIPReturnsFalse(t *testing.T) {
	c := newTestContainer("host.docker.internal", []netip.Addr{}, map[string]string{})

	u, ok := c.resolveContainerIP("tcp", "22")
	if ok {
		t.Errorf("resolveContainerIP should return false with no container IPs, got URL %s", u)
	}
}

func TestResolveContainerIP_UsesContainerIP(t *testing.T) {
	c := newTestContainer("host.docker.internal", []netip.Addr{netip.MustParseAddr("172.26.0.3")}, map[string]string{})

	u, ok := c.resolveContainerIP("tcp", "22")
	if !ok {
		t.Fatal("expected resolveContainerIP to succeed")
	}
	if u.Host != "172.26.0.3:22" {
		t.Errorf("host: got %q, want \"172.26.0.3:22\"", u.Host)
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
	result, err := c.getTargetURL(context.Background(), inputURL, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

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
				"network-bravo":   {IPAddress: netip.MustParseAddr("10.0.2.5"), Gateway: netip.MustParseAddr("10.0.2.1")},
				"network-alpha":   {IPAddress: netip.MustParseAddr("10.0.1.5"), Gateway: netip.MustParseAddr("10.0.1.1")},
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

func TestSetContainerNetwork_GatewayPriority(t *testing.T) {
	tests := []struct {
		name                  string
		defaultBridge         string
		networks              map[string]*network.EndpointSettings
		wantFirst, wantSecond string
	}{
		{
			name:          "PrefersGatewayMatch",
			defaultBridge: "10.0.1.1",
			networks: map[string]*network.EndpointSettings{
				"network-bravo": {IPAddress: netip.MustParseAddr("10.0.2.5"), Gateway: netip.MustParseAddr("10.0.2.1")},
				"network-alpha": {IPAddress: netip.MustParseAddr("10.0.1.5"), Gateway: netip.MustParseAddr("10.0.1.1")},
			},
			wantFirst:  "10.0.1.5",
			wantSecond: "10.0.2.5",
		},
		{
			name:          "GatewayMatchOverridesAlphaSort",
			defaultBridge: "172.18.0.1",
			networks: map[string]*network.EndpointSettings{
				"aaa-network": {IPAddress: netip.MustParseAddr("10.0.1.5"), Gateway: netip.MustParseAddr("10.0.1.1")},
				"zzz-network": {IPAddress: netip.MustParseAddr("172.18.0.42"), Gateway: netip.MustParseAddr("172.18.0.1")},
			},
			wantFirst:  "172.18.0.42",
			wantSecond: "10.0.1.5",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := &container{
				log:                  zerolog.Nop(),
				defaultBridgeAddress: netip.MustParseAddr(tc.defaultBridge),
			}

			dcontainer := ctypes.InspectResponse{
				NetworkSettings: &ctypes.NetworkSettings{
					Networks: tc.networks,
				},
			}

			c.setContainerNetwork(dcontainer)

			if len(c.ipAddress) != 2 {
				t.Fatalf("expected 2 IPs, got %d", len(c.ipAddress))
			}
			if c.ipAddress[0].String() != tc.wantFirst {
				t.Errorf("ipAddress[0]: got %q, want %q", c.ipAddress[0], tc.wantFirst)
			}
			if c.ipAddress[1].String() != tc.wantSecond {
				t.Errorf("ipAddress[1]: got %q, want %q", c.ipAddress[1], tc.wantSecond)
			}
		})
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
				"network-no-ip":   {IPAddress: netip.Addr{}, Gateway: netip.MustParseAddr("10.0.1.1")},
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

func TestResolveViaGateway_UsesPublishedPort(t *testing.T) {
	c := &container{
		log:      zerolog.Nop(),
		gateways: []netip.Addr{netip.MustParseAddr("172.26.0.1")},
	}

	u, ok := c.resolveViaGateway("tcp", "2222")
	if !ok {
		t.Fatal("expected resolveViaGateway to succeed")
	}
	if u.Host != "172.26.0.1:2222" {
		t.Errorf("host: got %q, want \"172.26.0.1:2222\"", u.Host)
	}
}

func TestResolveViaGateway_NoPublishedPortReturnsFalse(t *testing.T) {
	c := &container{
		log:      zerolog.Nop(),
		gateways: []netip.Addr{netip.MustParseAddr("172.26.0.1")},
	}

	_, ok := c.resolveViaGateway("tcp", "")
	if ok {
		t.Error("expected resolveViaGateway to fail with no published port")
	}
}

func TestResolveViaGateway_WorksForAllProtocols(t *testing.T) {
	c := &container{
		log:      zerolog.Nop(),
		gateways: []netip.Addr{netip.MustParseAddr("172.26.0.1")},
	}

	for _, scheme := range []string{"http", "https", "tcp", "udp"} {
		u, ok := c.resolveViaGateway(scheme, "8080")
		if !ok {
			t.Errorf("%s: expected resolveViaGateway to succeed", scheme)
			continue
		}
		if u.Scheme != scheme {
			t.Errorf("%s: scheme: got %q", scheme, u.Scheme)
		}
		if u.Host != "172.26.0.1:8080" {
			t.Errorf("%s: host: got %q, want \"172.26.0.1:8080\"", scheme, u.Host)
		}
	}
}

func TestGetTargetURL_AcrossNetworksUsesPublishedHostname(t *testing.T) {
	c := &container{
		log:                   zerolog.Nop(),
		id:                    "test-container-id",
		name:                  "opencode-web",
		targetProviderName:    "local",
		defaultTargetHostname: "host.docker.internal",
		ipAddress:             []netip.Addr{netip.MustParseAddr("172.26.0.3")},
		gateways:              []netip.Addr{netip.MustParseAddr("172.26.0.1")},
		ports:                 map[string]string{"22": "2222"},
		networkMode:           ctypes.NetworkMode("bridge"),
		autodetect:            false,
	}

	// TCP and HTTP should resolve identically
	for _, tc := range []struct {
		scheme string
		url    string
	}{
		{"tcp", "tcp://0.0.0.0:22"},
		{"http", "http://0.0.0.0:22"},
	} {
		inputURL, _ := url.Parse(tc.url)
		result, err := c.getTargetURL(context.Background(), inputURL, false)
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", tc.scheme, err)
		}
		if result.Host != "host.docker.internal:2222" {
			t.Errorf("%s host: got %q, want \"host.docker.internal:2222\"", tc.scheme, result.Host)
		}
	}
}

func TestGetTargetURL_FallsBackToGateway(t *testing.T) {
	c := &container{
		log:         zerolog.Nop(),
		ipAddress:   []netip.Addr{netip.MustParseAddr("172.26.0.3")},
		gateways:    []netip.Addr{netip.MustParseAddr("172.26.0.1")},
		ports:       map[string]string{"80": "8080"},
		networkMode: ctypes.NetworkMode("bridge"),
		autodetect:  false,
	}

	inputURL, _ := url.Parse("http://0.0.0.0:80")
	result, err := c.getTargetURL(context.Background(), inputURL, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Host != "172.26.0.1:8080" {
		t.Errorf("host: got %q, want \"172.26.0.1:8080\" (gateway + published port)", result.Host)
	}
}

func TestGetTargetURL_FallsBackToContainerIP(t *testing.T) {
	c := &container{
		log:         zerolog.Nop(),
		ipAddress:   []netip.Addr{netip.MustParseAddr("172.26.0.3")},
		ports:       map[string]string{},
		networkMode: ctypes.NetworkMode("bridge"),
		autodetect:  false,
	}

	inputURL, _ := url.Parse("http://0.0.0.0:80")
	result, err := c.getTargetURL(context.Background(), inputURL, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Host != "172.26.0.3:80" {
		t.Errorf("host: got %q, want \"172.26.0.3:80\" (container IP fallback)", result.Host)
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
	result, err := c.getTargetURL(context.Background(), inputURL, false)
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
	result, err := c.getTargetURL(context.Background(), inputURL, false)
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
	_, err := c.getTargetURL(context.Background(), inputURL, false)
	if err == nil {
		t.Error("expected error when host-network container has no hostname")
	}
}

func TestGetTargetURL_ReResolveSamePath(t *testing.T) {
	c := newTestContainer("host.docker.internal", []netip.Addr{netip.MustParseAddr("172.17.0.5")}, map[string]string{
		"3000": "32768",
	})

	inputURL, _ := url.Parse("http://0.0.0.0:3000")

	result1, err := c.getTargetURL(context.Background(), inputURL, false)
	if err != nil {
		t.Fatalf("first resolution failed: %v", err)
	}

	result2, err := c.getTargetURL(context.Background(), inputURL, false)
	if err != nil {
		t.Fatalf("second resolution failed: %v", err)
	}

	if result1.String() != result2.String() {
		t.Errorf("re-resolution mismatch: first=%q second=%q", result1.String(), result2.String())
	}
}

func TestGetTargetURL_ReResolveDifferentIP(t *testing.T) {
	// First resolution: container at 172.17.0.5
	c1 := newTestContainer("", []netip.Addr{netip.MustParseAddr("172.17.0.5")}, map[string]string{
		"80": "8080",
	})

	inputURL, _ := url.Parse("http://0.0.0.0:80")
	result1, err := c1.getTargetURL(context.Background(), inputURL, false)
	if err != nil {
		t.Fatalf("first resolution failed: %v", err)
	}

	// Simulate container restart with new IP: 172.17.0.99
	c2 := newTestContainer("", []netip.Addr{netip.MustParseAddr("172.17.0.99")}, map[string]string{
		"80": "8080",
	})

	result2, err := c2.getTargetURL(context.Background(), inputURL, false)
	if err != nil {
		t.Fatalf("re-resolution failed: %v", err)
	}

	// Results should differ because the container IP changed
	if result1.String() == result2.String() {
		t.Errorf("expected different targets after IP change, got same: %q", result1.String())
	}

	// Second result should use the new IP
	if result2.Host != "172.17.0.99:80" {
		t.Errorf("expected new IP in target, got %q", result2.Host)
	}
}

func TestGetTargetURL_TCPReResolveConsistent(t *testing.T) {
	c := newTestContainer("host.docker.internal", []netip.Addr{netip.MustParseAddr("172.17.0.5")}, map[string]string{
		"22": "8222",
	})

	inputURL, _ := url.Parse("tcp://0.0.0.0:22")

	result1, err := c.getTargetURL(context.Background(), inputURL, false)
	if err != nil {
		t.Fatalf("first TCP resolution failed: %v", err)
	}

	result2, err := c.getTargetURL(context.Background(), inputURL, false)
	if err != nil {
		t.Fatalf("second TCP resolution failed: %v", err)
	}

	if result1.String() != result2.String() {
		t.Errorf("TCP re-resolution mismatch: first=%q second=%q", result1.String(), result2.String())
	}
}
