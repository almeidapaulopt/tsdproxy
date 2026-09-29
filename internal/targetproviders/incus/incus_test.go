// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package incus

import (
	"context"
	"encoding/json"
	"errors"
	"net/netip"
	"net/url"
	"testing"

	incusapi "github.com/lxc/incus/v7/shared/api"
	"github.com/rs/zerolog"

	"github.com/almeidapaulopt/tsdproxy/internal/model"
	"github.com/almeidapaulopt/tsdproxy/internal/targetproviders"
)

func newTestInstance(config map[string]string, ips []netip.Addr) *instance {
	return &instance{
		log:                   zerolog.Nop(),
		config:                config,
		ips:                   ips,
		assets:                testAssets,
		name:                  "test-instance",
		targetProviderName:    "local",
		providerAutoRestart:   true,
		providerHealthEnabled: true,
	}
}

// testAPIInstance builds an api.Instance without promoted-field literals,
// which require go1.27.
func testAPIInstance(name string, statusCode incusapi.StatusCode, config map[string]string) *incusapi.Instance {
	inst := &incusapi.Instance{
		Name:       name,
		StatusCode: statusCode,
	}
	inst.Config = config
	return inst
}

func testState(networks map[string][]incusapi.InstanceStateNetworkAddress) *incusapi.InstanceState {
	state := &incusapi.InstanceState{Network: map[string]incusapi.InstanceStateNetwork{}}
	for name, addresses := range networks {
		state.Network[name] = incusapi.InstanceStateNetwork{Addresses: addresses}
	}
	return state
}

func addr(family, address, scope string) incusapi.InstanceStateNetworkAddress {
	return incusapi.InstanceStateNetworkAddress{Family: family, Address: address, Scope: scope}
}

func TestInstanceEnabled(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		value string
		want  bool
	}{
		{"Missing", "", false},
		{"True", "true", true},
		{"One", "1", true},
		{"False", "false", false},
		{"Invalid", "yes", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			config := map[string]string{}
			if tc.name != "Missing" {
				config[ConfigIsEnabled] = tc.value
			}
			if got := instanceEnabled(config); got != tc.want {
				t.Errorf("instanceEnabled() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSetInstanceNetwork_IPv4PreferredOverIPv6(t *testing.T) {
	t.Parallel()

	i := newTestInstance(map[string]string{}, nil)
	i.setInstanceNetwork(testState(map[string][]incusapi.InstanceStateNetworkAddress{
		"eth0": {
			addr("inet", "10.0.0.5", "global"),
			addr("inet6", "fd42:1::5", "global"),
		},
	}))

	if len(i.ips) != 2 {
		t.Fatalf("expected 2 IPs, got %d", len(i.ips))
	}
	if i.ips[0].String() != "10.0.0.5" {
		t.Errorf("ips[0]: got %q, want IPv4 first", i.ips[0].String())
	}
}

func TestSetInstanceNetwork_Eth0Preferred(t *testing.T) {
	t.Parallel()

	i := newTestInstance(map[string]string{}, nil)
	i.setInstanceNetwork(testState(map[string][]incusapi.InstanceStateNetworkAddress{
		"aaa-first": {addr("inet", "10.0.1.5", "global")},
		"eth0":      {addr("inet", "10.0.2.5", "global")},
	}))

	if len(i.ips) != 2 {
		t.Fatalf("expected 2 IPs, got %d", len(i.ips))
	}
	if i.ips[0].String() != "10.0.2.5" {
		t.Errorf("ips[0]: got %q, want eth0 address first", i.ips[0].String())
	}
}

func TestSetInstanceNetwork_FiltersScopeAndLoopback(t *testing.T) {
	t.Parallel()

	i := newTestInstance(map[string]string{}, nil)
	i.setInstanceNetwork(testState(map[string][]incusapi.InstanceStateNetworkAddress{
		"lo":   {addr("inet", "127.0.0.1", "local")},
		"eth0": {addr("inet", "10.0.0.5", "link"), addr("inet", "10.0.0.6", "global")},
	}))

	if len(i.ips) != 1 {
		t.Fatalf("expected 1 IP (global only, lo skipped), got %d", len(i.ips))
	}
	if i.ips[0].String() != "10.0.0.6" {
		t.Errorf("ips[0]: got %q, want global address", i.ips[0].String())
	}
}

func TestSetInstanceNetwork_NilState(t *testing.T) {
	t.Parallel()

	i := newTestInstance(map[string]string{}, nil)
	i.setInstanceNetwork(nil)

	if len(i.ips) != 0 {
		t.Errorf("expected no IPs with nil state, got %d", len(i.ips))
	}
}

func TestSetInstanceNetwork_SkipsInvalidAddresses(t *testing.T) {
	t.Parallel()

	i := newTestInstance(map[string]string{}, nil)
	i.setInstanceNetwork(testState(map[string][]incusapi.InstanceStateNetworkAddress{
		"eth0": {
			addr("inet", "not-an-ip", "global"),
			addr("inet", "10.0.0.5", "global"),
		},
	}))

	if len(i.ips) != 1 {
		t.Fatalf("expected 1 valid IP, got %d", len(i.ips))
	}
	if i.ips[0].String() != "10.0.0.5" {
		t.Errorf("ips[0]: got %q, want valid address", i.ips[0].String())
	}
}

func TestGetTargetURL_UsesInstanceIP(t *testing.T) {
	t.Parallel()

	i := newTestInstance(map[string]string{}, []netip.Addr{netip.MustParseAddr("10.108.1.5")})

	inputURL, _ := url.Parse("http://0.0.0.0:8080")
	result, err := i.getTargetURL(inputURL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Host != "10.108.1.5:8080" {
		t.Errorf("host: got %q, want %q", result.Host, "10.108.1.5:8080")
	}
}

func TestGetTargetURL_TargetHostnameOverride(t *testing.T) {
	t.Parallel()

	i := newTestInstance(map[string]string{}, []netip.Addr{netip.MustParseAddr("10.108.1.5")})
	i.defaultTargetHostname = "incus-host.local"

	inputURL, _ := url.Parse("http://0.0.0.0:8080")
	result, err := i.getTargetURL(inputURL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Host != "incus-host.local:8080" {
		t.Errorf("host: got %q, want %q", result.Host, "incus-host.local:8080")
	}
}

func TestGetTargetURL_NoAddressReturnsError(t *testing.T) {
	t.Parallel()

	i := newTestInstance(map[string]string{}, nil)

	inputURL, _ := url.Parse("http://0.0.0.0:8080")
	_, err := i.getTargetURL(inputURL)
	if !errors.Is(err, ErrNoAddressFound) {
		t.Errorf("expected ErrNoAddressFound, got %v", err)
	}
}

func TestGetTargetURL_IPv6Bracketed(t *testing.T) {
	t.Parallel()

	i := newTestInstance(map[string]string{}, []netip.Addr{netip.MustParseAddr("fd42:1::5")})

	inputURL, _ := url.Parse("http://0.0.0.0:8080")
	result, err := i.getTargetURL(inputURL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Host != "[fd42:1::5]:8080" {
		t.Errorf("host: got %q, want %q", result.Host, "[fd42:1::5]:8080")
	}
}

func TestGetProxyHostname(t *testing.T) {
	t.Parallel()

	t.Run("DefaultLowercasesName", func(t *testing.T) {
		t.Parallel()

		i := newTestInstance(map[string]string{}, nil)
		i.name = "My-App"

		hostname, err := i.getProxyHostname()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if hostname != "my-app" {
			t.Errorf("hostname: got %q, want %q", hostname, "my-app")
		}
	})

	t.Run("CustomName", func(t *testing.T) {
		t.Parallel()

		i := newTestInstance(map[string]string{ConfigName: "custom-name"}, nil)

		hostname, err := i.getProxyHostname()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if hostname != "custom-name" {
			t.Errorf("hostname: got %q, want %q", hostname, "custom-name")
		}
	})

	t.Run("InvalidCustomName", func(t *testing.T) {
		t.Parallel()

		i := newTestInstance(map[string]string{ConfigName: "invalid_hostname!"}, nil)

		if _, err := i.getProxyHostname(); err == nil {
			t.Error("expected error for invalid hostname")
		}
	})
}

func TestNewProxyConfig_MapsConfigKeys(t *testing.T) {
	t.Parallel()

	config := map[string]string{
		ConfigIsEnabled:     "true",
		ConfigName:          "myapp",
		ConfigPort + "1":    "443/https:80/http",
		ConfigTags:          "tag:prod",
		ConfigEphemeral:     "true",
		ConfigDomain:        "myapp.example.com",
		ConfigDNSProvider:   "cf",
		imageDescriptionKey: "Alpine 3.21",
	}
	inst := testAPIInstance("myapp", incusapi.Running, config)
	state := testState(map[string][]incusapi.InstanceStateNetworkAddress{
		"eth0": {addr("inet", "10.108.1.5", "global")},
	})

	i := newInstance(zerolog.Nop(), inst, state, withAssets(testAssets))

	pcfg, err := i.newProxyConfig(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if pcfg.TargetID != "myapp" {
		t.Errorf("TargetID: got %q", pcfg.TargetID)
	}
	if pcfg.Hostname != "myapp" {
		t.Errorf("Hostname: got %q", pcfg.Hostname)
	}
	if pcfg.TargetImage != "Alpine 3.21" {
		t.Errorf("TargetImage: got %q", pcfg.TargetImage)
	}
	if pcfg.Domain != "myapp.example.com" {
		t.Errorf("Domain: got %q", pcfg.Domain)
	}
	if pcfg.DNSProvider != "cf" {
		t.Errorf("DNSProvider: got %q", pcfg.DNSProvider)
	}
	if !pcfg.Tailscale.Ephemeral {
		t.Error("Tailscale.Ephemeral: want true")
	}
	if pcfg.Tailscale.Tags != "tag:prod" {
		t.Errorf("Tailscale.Tags: got %q", pcfg.Tailscale.Tags)
	}

	port, ok := pcfg.Ports[ConfigPort+"1"]
	if !ok {
		t.Fatalf("expected port %q in ports", ConfigPort+"1")
	}
	target := port.GetFirstTarget()
	if target == nil {
		t.Fatal("expected target URL")
	}
	if target.String() != "http://10.108.1.5:80" {
		t.Errorf("target: got %q, want %q", target.String(), "http://10.108.1.5:80")
	}
}

func TestNewProxyConfig_PortRange(t *testing.T) {
	t.Parallel()

	config := map[string]string{
		ConfigIsEnabled:  "true",
		ConfigPort + "1": "2222-2223/tcp:2222-2223/tcp",
	}
	inst := testAPIInstance("rangeapp", incusapi.Running, config)
	state := testState(map[string][]incusapi.InstanceStateNetworkAddress{
		"eth0": {addr("inet", "10.108.1.5", "global")},
	})

	i := newInstance(zerolog.Nop(), inst, state, withAssets(testAssets))

	pcfg, err := i.newProxyConfig(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(pcfg.Ports) != 2 {
		t.Fatalf("expected 2 expanded ports, got %d", len(pcfg.Ports))
	}
	for key, port := range pcfg.Ports {
		target := port.GetFirstTarget()
		if target == nil {
			t.Fatalf("port %q: expected target URL", key)
		}
		if target.Scheme != "tcp" {
			t.Errorf("port %q: scheme: got %q, want tcp", key, target.Scheme)
		}
		if target.Host != "10.108.1.5:"+target.Port() {
			t.Errorf("port %q: host: got %q, want instance IP", key, target.Host)
		}
	}
}

func TestApplyPortOptions_FunnelGated(t *testing.T) {
	t.Parallel()

	t.Run("Allowed", func(t *testing.T) {
		t.Parallel()

		i := newTestInstance(map[string]string{}, nil)
		i.allowInstanceFunnel = true

		port, err := model.NewPortLongLabel("443/https:80/http")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		i.applyPortOptions("test", &port, []string{"tailscale_funnel"})
		if !port.Tailscale.Funnel {
			t.Error("expected funnel enabled when allowed")
		}
	})

	t.Run("NotAllowed", func(t *testing.T) {
		t.Parallel()

		i := newTestInstance(map[string]string{}, nil)
		i.allowInstanceFunnel = false

		port, err := model.NewPortLongLabel("443/https:80/http")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		i.applyPortOptions("test", &port, []string{"tailscale_funnel"})
		if port.Tailscale.Funnel {
			t.Error("expected funnel disabled when not allowed")
		}
	})
}

func TestApplyPortOptions_NoTLSValidateGated(t *testing.T) {
	t.Parallel()

	i := newTestInstance(map[string]string{}, nil)
	i.allowTLSValidateDisable = false

	port, err := model.NewPortLongLabel("443/https:80/http")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	i.applyPortOptions("test", &port, []string{"no_tlsvalidate"})
	if !port.TLSValidate {
		t.Error("expected TLSValidate to stay true when not allowed")
	}
}

func TestTsdproxyConfigChanged(t *testing.T) {
	t.Parallel()

	base := map[string]string{
		ConfigIsEnabled:       "true",
		ConfigPort + "1":      "443/https:80/http",
		"volatile.base_image": "abc123",
	}

	t.Run("Unchanged", func(t *testing.T) {
		t.Parallel()

		same := map[string]string{
			ConfigIsEnabled:       "true",
			ConfigPort + "1":      "443/https:80/http",
			"volatile.base_image": "different-churn",
		}
		if tsdproxyConfigChanged(base, same) {
			t.Error("expected no change when only non-tsdproxy keys differ")
		}
	})

	t.Run("Changed", func(t *testing.T) {
		t.Parallel()

		changed := map[string]string{
			ConfigIsEnabled:  "true",
			ConfigPort + "1": "443/https:8080/http",
		}
		if !tsdproxyConfigChanged(base, changed) {
			t.Error("expected change when tsdproxy key differs")
		}
	})

	t.Run("KeyRemoved", func(t *testing.T) {
		t.Parallel()

		removed := map[string]string{ConfigIsEnabled: "true"}
		if !tsdproxyConfigChanged(base, removed) {
			t.Error("expected change when tsdproxy key removed")
		}
	})
}

// mockAPIClient implements APIClient for unit testing without an Incus daemon.
type mockAPIClient struct {
	instances map[string]*incusapi.Instance
	states    map[string]*incusapi.InstanceState
	list      []incusapi.Instance
}

func (m *mockAPIClient) GetInstances(incusapi.InstanceType) ([]incusapi.Instance, error) {
	return m.list, nil
}

func (m *mockAPIClient) GetInstance(name string) (*incusapi.Instance, string, error) {
	inst, ok := m.instances[name]
	if !ok {
		return nil, "", errors.New("not found")
	}
	return inst, "", nil
}

func (m *mockAPIClient) GetInstanceState(name string) (*incusapi.InstanceState, string, error) {
	state, ok := m.states[name]
	if !ok {
		return nil, "", errors.New("not found")
	}
	return state, "", nil
}

func (m *mockAPIClient) GetEventsByType([]string) (EventListener, error) {
	return nil, errors.New("not implemented")
}

func (m *mockAPIClient) Disconnect() {}

func newTestClient(api APIClient) *Client {
	return &Client{
		log:       zerolog.Nop(),
		incus:     api,
		instances: make(map[string]*instance),
		name:      "test",
		assets:    testAssets,
	}
}

func lifecycleEvent(action, name string) incusapi.Event {
	metadata, _ := json.Marshal(incusapi.EventLifecycle{Action: action, Name: name})
	return incusapi.Event{Type: incusapi.EventTypeLifecycle, Metadata: metadata}
}

func TestClient_AddTarget(t *testing.T) {
	// NOTE: no t.Parallel() — mutates shared provider instance map.
	api := &mockAPIClient{
		instances: map[string]*incusapi.Instance{
			"enabled": testAPIInstance("enabled", incusapi.Running, map[string]string{
				ConfigIsEnabled:  "true",
				ConfigPort + "1": "443/https:80/http",
			}),
			"disabled": testAPIInstance("disabled", incusapi.Running, map[string]string{}),
			"stopped":  testAPIInstance("stopped", incusapi.Stopped, map[string]string{ConfigIsEnabled: "true"}),
		},
		states: map[string]*incusapi.InstanceState{
			"enabled": testState(map[string][]incusapi.InstanceStateNetworkAddress{
				"eth0": {addr("inet", "10.108.1.5", "global")},
			}),
		},
	}
	c := newTestClient(api)

	pcfg, err := c.AddTarget("enabled")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pcfg.Hostname != "enabled" {
		t.Errorf("hostname: got %q", pcfg.Hostname)
	}

	if _, err := c.AddTarget("disabled"); !errors.Is(err, ErrInstanceNotEnabled) {
		t.Errorf("expected ErrInstanceNotEnabled, got %v", err)
	}

	if _, err := c.AddTarget("stopped"); !errors.Is(err, ErrInstanceNotRunning) {
		t.Errorf("expected ErrInstanceNotRunning, got %v", err)
	}

	if _, err := c.AddTarget("missing"); err == nil {
		t.Error("expected error for missing instance")
	}
}

func TestClient_DeleteProxy(t *testing.T) {
	// NOTE: no t.Parallel() — mutates provider instance map.
	c := newTestClient(&mockAPIClient{})
	c.addInstance(&instance{config: map[string]string{}}, "tracked")

	if err := c.DeleteProxy("tracked"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	err := c.DeleteProxy("untracked")
	if !errors.Is(err, targetproviders.ErrTargetNotFound) {
		t.Errorf("expected ErrTargetNotFound, got %v", err)
	}
}

func TestClient_HandleEvent(t *testing.T) {
	t.Parallel()

	newAPI := func() *mockAPIClient {
		return &mockAPIClient{
			instances: map[string]*incusapi.Instance{
				"app":   testAPIInstance("app", incusapi.Running, map[string]string{ConfigIsEnabled: "true"}),
				"other": testAPIInstance("other", incusapi.Running, map[string]string{}),
			},
		}
	}

	tests := []struct {
		name       string
		action     string
		instance   string
		wantAction targetproviders.ActionType
		track      bool
		wantEvent  bool
	}{
		{"StartedEnabled", incusapi.EventLifecycleInstanceStarted, "app", targetproviders.ActionStartProxy, false, true},
		{"StartedDisabled", incusapi.EventLifecycleInstanceStarted, "other", 0, false, false},
		{"StoppedEnabled", incusapi.EventLifecycleInstanceStopped, "app", targetproviders.ActionStopProxy, false, true},
		{"RestartedEnabled", incusapi.EventLifecycleInstanceRestarted, "app", targetproviders.ActionRestartProxy, false, true},
		{"FrozenEmitsStop", incusapi.EventLifecycleInstancePaused, "app", targetproviders.ActionStopProxy, false, true},
		{"UnfrozenEmitsStart", incusapi.EventLifecycleInstanceResumed, "app", targetproviders.ActionStartProxy, false, true},
		{"DeletedTracked", incusapi.EventLifecycleInstanceDeleted, "app", targetproviders.ActionStopProxy, true, true},
		{"DeletedUntracked", incusapi.EventLifecycleInstanceDeleted, "app", 0, false, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			c := newTestClient(newAPI())
			if tc.track {
				c.addInstance(&instance{config: map[string]string{ConfigIsEnabled: "true"}}, tc.instance)
			}

			eventsChan := make(chan targetproviders.TargetEvent, 1)
			c.handleEvent(context.Background(), lifecycleEvent(tc.action, tc.instance), eventsChan)

			if !tc.wantEvent {
				select {
				case event := <-eventsChan:
					t.Errorf("unexpected event: %+v", event)
				default:
				}
				return
			}

			select {
			case event := <-eventsChan:
				if event.ID != tc.instance {
					t.Errorf("event ID: got %q, want %q", event.ID, tc.instance)
				}
				if event.Action != tc.wantAction {
					t.Errorf("event action: got %d, want %d", event.Action, tc.wantAction)
				}
			default:
				t.Fatal("expected event, got none")
			}
		})
	}
}

func TestClient_HandleInstanceUpdated(t *testing.T) {
	t.Parallel()

	t.Run("ConfigChangedRestarts", func(t *testing.T) {
		t.Parallel()

		api := &mockAPIClient{
			instances: map[string]*incusapi.Instance{
				"app": testAPIInstance("app", incusapi.Running, map[string]string{
					ConfigIsEnabled:  "true",
					ConfigPort + "1": "443/https:8080/http",
				}),
			},
		}
		c := newTestClient(api)
		c.addInstance(&instance{config: map[string]string{
			ConfigIsEnabled:  "true",
			ConfigPort + "1": "443/https:80/http",
		}}, "app")

		eventsChan := make(chan targetproviders.TargetEvent, 1)
		c.handleEvent(context.Background(), lifecycleEvent(incusapi.EventLifecycleInstanceUpdated, "app"), eventsChan)

		select {
		case event := <-eventsChan:
			if event.Action != targetproviders.ActionRestartProxy {
				t.Errorf("action: got %d, want restart", event.Action)
			}
		default:
			t.Fatal("expected restart event")
		}
	})

	t.Run("UnrelatedChangeIgnored", func(t *testing.T) {
		t.Parallel()

		api := &mockAPIClient{
			instances: map[string]*incusapi.Instance{
				"app": testAPIInstance("app", incusapi.Running, map[string]string{
					ConfigIsEnabled:       "true",
					"volatile.cloud-init": "done",
				}),
			},
		}
		c := newTestClient(api)
		c.addInstance(&instance{config: map[string]string{
			ConfigIsEnabled: "true",
		}}, "app")

		eventsChan := make(chan targetproviders.TargetEvent, 1)
		c.handleEvent(context.Background(), lifecycleEvent(incusapi.EventLifecycleInstanceUpdated, "app"), eventsChan)

		select {
		case event := <-eventsChan:
			t.Errorf("unexpected event: %+v", event)
		default:
		}
	})

	t.Run("NewlyEnabledStarts", func(t *testing.T) {
		t.Parallel()

		api := &mockAPIClient{
			instances: map[string]*incusapi.Instance{
				"app": testAPIInstance("app", incusapi.Running, map[string]string{ConfigIsEnabled: "true"}),
			},
		}
		c := newTestClient(api)

		eventsChan := make(chan targetproviders.TargetEvent, 1)
		c.handleEvent(context.Background(), lifecycleEvent(incusapi.EventLifecycleInstanceUpdated, "app"), eventsChan)

		select {
		case event := <-eventsChan:
			if event.Action != targetproviders.ActionStartProxy {
				t.Errorf("action: got %d, want start", event.Action)
			}
		default:
			t.Fatal("expected start event")
		}
	})

	t.Run("NewlyDisabledStops", func(t *testing.T) {
		t.Parallel()

		api := &mockAPIClient{
			instances: map[string]*incusapi.Instance{
				"app": testAPIInstance("app", incusapi.Running, map[string]string{}),
			},
		}
		c := newTestClient(api)
		c.addInstance(&instance{config: map[string]string{ConfigIsEnabled: "true"}}, "app")

		eventsChan := make(chan targetproviders.TargetEvent, 1)
		c.handleEvent(context.Background(), lifecycleEvent(incusapi.EventLifecycleInstanceUpdated, "app"), eventsChan)

		select {
		case event := <-eventsChan:
			if event.Action != targetproviders.ActionStopProxy {
				t.Errorf("action: got %d, want stop", event.Action)
			}
		default:
			t.Fatal("expected stop event")
		}
	})
}

func TestClient_StartAllProxies(t *testing.T) {
	t.Parallel()

	api := &mockAPIClient{
		list: []incusapi.Instance{
			*testAPIInstance("running-enabled", incusapi.Running, map[string]string{ConfigIsEnabled: "true"}),
			*testAPIInstance("running-disabled", incusapi.Running, map[string]string{}),
			*testAPIInstance("stopped-enabled", incusapi.Stopped, map[string]string{ConfigIsEnabled: "true"}),
		},
	}
	c := newTestClient(api)

	eventsChan := make(chan targetproviders.TargetEvent, 2)
	errChan := make(chan error, 1)
	c.startAllProxies(context.Background(), eventsChan, errChan)

	select {
	case event := <-eventsChan:
		if event.ID != "running-enabled" {
			t.Errorf("event ID: got %q, want running-enabled", event.ID)
		}
	default:
		t.Fatal("expected start event")
	}

	select {
	case event := <-eventsChan:
		t.Errorf("unexpected extra event: %+v", event)
	default:
	}
}
