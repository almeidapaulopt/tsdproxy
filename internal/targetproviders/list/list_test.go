// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package list

import (
	"context"
	"net/url"
	"path/filepath"
	"sync"
	"testing"

	"github.com/almeidapaulopt/tsdproxy/internal/config"
	"github.com/almeidapaulopt/tsdproxy/internal/model"
	"github.com/almeidapaulopt/tsdproxy/internal/targetproviders"

	"github.com/fsnotify/fsnotify"
	"github.com/rs/zerolog"
)

func newTestClient(t *testing.T, proxies configProxyList) *Client {
	t.Helper()
	if proxies == nil {
		proxies = make(configProxyList)
	}
	return &Client{
		log:           zerolog.Nop(),
		name:          "test-list",
		configProxies: proxies,
		proxies:       make(configProxyList),
		config: config.ListTargetProviderConfig{
			DefaultProxyProvider: "default-proxy",
		},
		eventsChan: make(chan targetproviders.TargetEvent, 100),
	}
}

func TestNewClient(t *testing.T) {
	cfg := config.ListTargetProviderConfig{Filename: "testdata/nonexistent.yaml"}
	_, err := New(zerolog.Nop(), "test", &cfg)
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
}

func TestClient_GetDefaultProxyProviderName(t *testing.T) {
	c := newTestClient(t, nil)
	if got := c.GetDefaultProxyProviderName(); got != "default-proxy" {
		t.Fatalf("expected default-proxy, got %s", got)
	}
}

func TestClient_AddTarget(t *testing.T) {
	c := newTestClient(t, configProxyList{
		"myapp": {
			Ports: map[string]port{
				"80/tcp": {
					Targets: []string{"http://localhost:8080"},
				},
			},
		},
	})

	pcfg, err := c.AddTarget("myapp")
	if err != nil {
		t.Fatalf("AddTarget failed: %v", err)
	}
	if pcfg.Hostname != "myapp" {
		t.Fatalf("expected hostname myapp, got %s", pcfg.Hostname)
	}
	if pcfg.TargetProvider != "test-list" {
		t.Fatalf("expected target provider test-list, got %s", pcfg.TargetProvider)
	}
	if pcfg.ProxyProvider != "default-proxy" {
		t.Fatalf("expected proxy provider default-proxy, got %s", pcfg.ProxyProvider)
	}
}

func TestClient_AddTarget_PerEntryProxyProvider(t *testing.T) {
	c := newTestClient(t, configProxyList{
		"myapp": {
			ProxyProvider: "custom-proxy",
		},
	})

	pcfg, err := c.AddTarget("myapp")
	if err != nil {
		t.Fatalf("AddTarget failed: %v", err)
	}
	if pcfg.ProxyProvider != "custom-proxy" {
		t.Fatalf("expected custom-proxy, got %s", pcfg.ProxyProvider)
	}
}

func TestClient_AddTarget_NotFound(t *testing.T) {
	c := newTestClient(t, nil)
	_, err := c.AddTarget("nonexistent")
	if err == nil {
		t.Fatal("expected error for unknown target")
	}
}

func TestClient_AddTarget_ThenDelete(t *testing.T) {
	c := newTestClient(t, configProxyList{
		"myapp": {},
	})

	_, err := c.AddTarget("myapp")
	if err != nil {
		t.Fatalf("AddTarget failed: %v", err)
	}

	if err := c.DeleteProxy("myapp"); err != nil {
		t.Fatalf("DeleteProxy failed: %v", err)
	}
}

func TestClient_DeleteProxy_NotFound(t *testing.T) {
	c := newTestClient(t, nil)
	if err := c.DeleteProxy("nonexistent"); err == nil {
		t.Fatal("expected error for unknown target")
	}
}

func TestClient_ReResolve(t *testing.T) {
	c := newTestClient(t, configProxyList{
		"myapp": {
			Ports: map[string]port{
				"80/tcp": {
					Targets: []string{"http://localhost:8080"},
				},
			},
		},
	})

	pcfg, err := c.ReResolve("myapp")
	if err != nil {
		t.Fatalf("ReResolve failed: %v", err)
	}
	if pcfg.Hostname != "myapp" {
		t.Fatalf("expected hostname myapp, got %s", pcfg.Hostname)
	}
}

func TestClient_ReResolve_NotFound(t *testing.T) {
	c := newTestClient(t, nil)
	_, err := c.ReResolve("nonexistent")
	if err == nil {
		t.Fatal("expected error for unknown target")
	}
}

func TestClient_BuildConfig_WithDomainAndProviders(t *testing.T) {
	c := newTestClient(t, nil)
	p := proxyConfig{
		Domain:      "app.example.com",
		DNSProvider: "cloudflare",
		TLSProvider: "acme",
	}

	pcfg, err := c.buildConfig("myapp", p)
	if err != nil {
		t.Fatalf("buildConfig failed: %v", err)
	}
	if pcfg.Domain != "app.example.com" {
		t.Fatalf("expected domain app.example.com, got %s", pcfg.Domain)
	}
	if pcfg.DNSProvider != "cloudflare" {
		t.Fatalf("expected dns provider cloudflare, got %s", pcfg.DNSProvider)
	}
	if pcfg.TLSProvider != "acme" {
		t.Fatalf("expected tls provider acme, got %s", pcfg.TLSProvider)
	}
}

func TestClient_BuildConfig_PortWithTargets(t *testing.T) {
	c := newTestClient(t, nil)
	p := proxyConfig{
		Ports: map[string]port{
			"80/tcp": {
				Targets: []string{"http://backend:8080"},
			},
		},
	}

	pcfg, err := c.buildConfig("myapp", p)
	if err != nil {
		t.Fatalf("buildConfig failed: %v", err)
	}
	if len(pcfg.Ports) != 1 {
		t.Fatalf("expected 1 port, got %d", len(pcfg.Ports))
	}
	for key, pc := range pcfg.Ports {
		targets := pc.GetTargets()
		if len(targets) != 1 {
			t.Fatalf("port %s: expected 1 target, got %d", key, len(targets))
		}
	}
}

func TestClient_GetPorts(t *testing.T) {
	c := newTestClient(t, nil)
	ports := c.getPorts(map[string]port{
		"80/tcp": {
			Targets:     []string{"http://localhost:8080"},
			IsRedirect:  false,
			TLSValidate: true,
		},
	})
	if len(ports) != 1 {
		t.Fatalf("expected 1 port, got %d", len(ports))
	}
	for _, pc := range ports {
		if len(pc.GetTargets()) != 1 {
			t.Fatal("expected 1 target")
		}
	}
}

func TestClient_ParseAndAddTargets_Valid(t *testing.T) {
	c := newTestClient(t, nil)
	pc := model.PortConfig{}
	result := c.parseAndAddTargets(&pc, []string{"http://backend:8080"}, "test-port", "no targets found")
	if !result {
		t.Fatal("expected true for valid target")
	}
	if len(pc.GetTargets()) != 1 {
		t.Fatalf("expected 1 target, got %d", len(pc.GetTargets()))
	}
}

func TestClient_ParseAndAddTargets_InvalidURL(t *testing.T) {
	c := newTestClient(t, nil)
	pc := model.PortConfig{}
	result := c.parseAndAddTargets(&pc, []string{"not-a-valid-url"}, "test-port", "no targets found")
	if result {
		t.Fatal("expected false for invalid target")
	}
}

func TestClient_ParseAndAddTargets_NoTargets(t *testing.T) {
	c := newTestClient(t, nil)
	pc := model.PortConfig{}
	result := c.parseAndAddTargets(&pc, nil, "test-port", "no targets found")
	if result {
		t.Fatal("expected false when no targets")
	}
}

func TestClient_ProcessSinglePort_Valid(t *testing.T) {
	c := newTestClient(t, nil)
	ports := make(model.PortConfigList)
	c.processSinglePort(ports, "443/https", port{
		Targets: []string{"http://localhost:8080"},
	})
	if len(ports) != 1 {
		t.Fatalf("expected 1 port for 443/https, got %d", len(ports))
	}
}

func TestClient_GetPorts_PortRangeExpands(t *testing.T) {
	t.Parallel()

	c := newTestClient(t, nil)
	ports := c.getPorts(map[string]port{
		"2222-2224/tcp": {
			Targets: []string{"http://backend:8080"},
		},
	})

	if len(ports) != 3 {
		t.Fatalf("expected 3 ports from range 2222-2224, got %d", len(ports))
	}
	for key, pc := range ports {
		if len(pc.GetTargets()) != 1 {
			t.Fatalf("port %s: expected 1 target, got %d", key, len(pc.GetTargets()))
		}
	}
}

func TestClient_GetPorts_PortRangeWithRedirect(t *testing.T) {
	t.Parallel()

	c := newTestClient(t, nil)
	ports := c.getPorts(map[string]port{
		"2222-2223/tcp": {
			Targets:    []string{"https://app.example.com"},
			IsRedirect: true,
		},
	})

	if len(ports) != 2 {
		t.Fatalf("expected 2 ports from range, got %d", len(ports))
	}
	for key, pc := range ports {
		if !pc.IsRedirect {
			t.Fatalf("port %s: expected IsRedirect to be inherited from parent", key)
		}
		if len(pc.GetTargets()) != 1 {
			t.Fatalf("port %s: expected 1 target, got %d", key, len(pc.GetTargets()))
		}
	}
}

func TestClient_GetPorts_PortRangeNoTargets(t *testing.T) {
	t.Parallel()

	c := newTestClient(t, nil)
	ports := c.getPorts(map[string]port{
		"2222-2224/tcp": {
			Targets: nil,
		},
	})

	if len(ports) != 0 {
		t.Fatalf("expected 0 ports when range has no targets, got %d", len(ports))
	}
}

func TestClient_GetPorts_PortRangeWithTLSValidate(t *testing.T) {
	t.Parallel()

	c := newTestClient(t, nil)
	ports := c.getPorts(map[string]port{
		"2222-2224/tcp": {
			Targets:     []string{"https://backend:8443"},
			TLSValidate: false,
		},
	})

	if len(ports) != 3 {
		t.Fatalf("expected 3 ports from range, got %d", len(ports))
	}
	for key, pc := range ports {
		if pc.TLSValidate {
			t.Fatalf("port %s: expected TLSValidate to be inherited as false", key)
		}
	}
}

func TestClient_ProcessSinglePort_InvalidLabel(t *testing.T) {
	c := newTestClient(t, nil)
	ports := make(model.PortConfigList)
	c.processSinglePort(ports, "invalid-label", port{})
	if len(ports) != 0 {
		t.Fatal("expected no ports for invalid label")
	}
}

func TestClient_TrySendEvent(t *testing.T) {
	c := newTestClient(t, nil)
	ctx := context.Background()
	ok := c.trySendEvent(ctx, "myapp", targetproviders.ActionStartProxy)
	if !ok {
		t.Fatal("expected trySendEvent to succeed")
	}
}

func TestClient_TrySendEvent_NoChannel(t *testing.T) {
	c := newTestClient(t, nil)
	c.eventsChan = nil
	ok := c.trySendEvent(context.Background(), "myapp", targetproviders.ActionStartProxy)
	if ok {
		t.Fatal("expected trySendEvent to return false when eventsChan is nil")
	}
}

func TestClient_TrySendEvent_CancelledContext(t *testing.T) {
	c := newTestClient(t, nil)
	c.eventsChan = make(chan targetproviders.TargetEvent)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	ok := c.trySendEvent(ctx, "myapp", targetproviders.ActionStartProxy)
	if ok {
		t.Fatal("expected trySendEvent to return false when context is done")
	}
}

func TestClient_WatchEvents_EmitsStartEvents(t *testing.T) {
	c := newTestClient(t, configProxyList{
		"app1": {},
		"app2": {},
	})
	dir := t.TempDir()
	filename := filepath.Join(dir, "proxies.yaml")
	writeProxyConfigFile(t, filename, configProxyList{})
	c.file = config.NewConfigFile(zerolog.Nop(), filename, &configProxyList{})
	t.Cleanup(func() { c.Close() })

	eventsChan := make(chan targetproviders.TargetEvent, 10)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	c.WatchEvents(ctx, eventsChan, make(chan error, 1))

	received := make(map[string]bool)
	for i := 0; i < 2; i++ {
		ev := <-eventsChan
		received[ev.ID] = true
		if ev.Action != targetproviders.ActionStartProxy {
			t.Fatalf("expected ActionStartProxy for %s, got %v", ev.ID, ev.Action)
		}
	}
	if !received["app1"] || !received["app2"] {
		t.Fatal("expected both apps to emit start events")
	}
}

func writeProxyConfigFile(t *testing.T, filename string, proxies configProxyList) {
	t.Helper()
	f := config.NewConfigFile(zerolog.Nop(), filename, proxies)
	if err := f.Save(); err != nil {
		t.Fatalf("save test config: %v", err)
	}
}

func TestClient_OnFileChange_StartsAndStops(t *testing.T) {
	dir := t.TempDir()
	filename := dir + "/proxies.yaml"

	initial := configProxyList{"remove": {}}
	writeProxyConfigFile(t, filename, initial)

	c := &Client{
		log:           zerolog.Nop(),
		name:          "test-list",
		configProxies: initial,
		proxies:       make(configProxyList),
		config:        config.ListTargetProviderConfig{},
		eventsChan:    make(chan targetproviders.TargetEvent, 10),
		ctx:           context.Background(),
		mtx:           sync.RWMutex{},
		file:          config.NewConfigFile(zerolog.Nop(), filename, initial),
	}

	updated := configProxyList{"add": {}}
	writeProxyConfigFile(t, filename, updated)

	c.onFileChange(fsnotify.Event{})

	events := make(map[string]targetproviders.ActionType)
	for i := 0; i < 2; i++ {
		ev := <-c.eventsChan
		events[ev.ID] = ev.Action
	}

	if events["remove"] != targetproviders.ActionStopProxy {
		t.Fatal("expected remove to emit ActionStopProxy")
	}
	if events["add"] != targetproviders.ActionStartProxy {
		t.Fatal("expected add to emit ActionStartProxy")
	}
}

func TestClient_OnFileChange_RestartOnModify(t *testing.T) {
	dir := t.TempDir()
	filename := dir + "/proxies.yaml"

	initial := configProxyList{"myapp": {ProxyProvider: "old"}}
	writeProxyConfigFile(t, filename, initial)

	c := &Client{
		log:           zerolog.Nop(),
		name:          "test-list",
		configProxies: initial,
		proxies:       make(configProxyList),
		config:        config.ListTargetProviderConfig{},
		eventsChan:    make(chan targetproviders.TargetEvent, 10),
		ctx:           context.Background(),
		mtx:           sync.RWMutex{},
		file:          config.NewConfigFile(zerolog.Nop(), filename, initial),
	}

	updated := configProxyList{"myapp": {ProxyProvider: "new"}}
	writeProxyConfigFile(t, filename, updated)

	c.onFileChange(fsnotify.Event{})

	ev := <-c.eventsChan
	if ev.ID != "myapp" {
		t.Fatalf("expected myapp, got %s", ev.ID)
	}
	if ev.Action != targetproviders.ActionRestartProxy {
		t.Fatalf("expected ActionRestartProxy for modified, got %v", ev.Action)
	}
}

func TestClient_Close(t *testing.T) {
	c := newTestClient(t, nil)
	c.ctx = context.Background()
	c.eventsChan = make(chan targetproviders.TargetEvent, 10)
	c.proxies["proxy1"] = proxyConfig{}
	c.proxies["proxy2"] = proxyConfig{}

	c.Close()

	received := make(map[string]bool)
	for i := 0; i < 2; i++ {
		ev := <-c.eventsChan
		received[ev.ID] = true
		if ev.Action != targetproviders.ActionStopProxy {
			t.Fatalf("expected ActionStopProxy for %s, got %v", ev.ID, ev.Action)
		}
	}
	if !received["proxy1"] || !received["proxy2"] {
		t.Fatal("expected both proxies to emit stop events")
	}
}

func TestClient_DeleteProxy(t *testing.T) {
	c := newTestClient(t, nil)
	c.proxies["existing"] = proxyConfig{}
	if err := c.DeleteProxy("existing"); err != nil {
		t.Fatalf("DeleteProxy failed: %v", err)
	}
	if err := c.DeleteProxy("existing"); err == nil {
		t.Fatal("expected error on double delete")
	}
}

func TestClient_TargetURL(t *testing.T) {
	u, err := url.Parse("http://backend:8080/path")
	if err != nil {
		t.Fatalf("url.Parse failed: %v", err)
	}
	pc := model.PortConfig{}
	pc.AddTarget(u)
	targets := pc.GetTargets()
	if len(targets) != 1 {
		t.Fatalf("expected 1 target, got %d", len(targets))
	}
	if targets[0].String() != "http://backend:8080/path" {
		t.Fatalf("expected http://backend:8080/path, got %s", targets[0].String())
	}
}
