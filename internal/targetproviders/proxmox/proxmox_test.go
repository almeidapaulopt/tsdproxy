// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package proxmox

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"testing"

	"github.com/rs/zerolog"

	"github.com/almeidapaulopt/tsdproxy/internal/targetproviders"
)

// mockAPIClient implements APIClient for unit testing without a Proxmox VE
// host.
type mockAPIClient struct {
	configs    map[string]*GuestConfig
	interfaces map[string][]GuestInterface
	configErr  map[string]error
	guests     []GuestResource
}

func (m *mockAPIClient) GetVersion(context.Context) (string, error)          { return "9.0.0", nil }
func (m *mockAPIClient) Close()                                              {}
func (m *mockAPIClient) ListGuests(context.Context) ([]GuestResource, error) { return m.guests, nil }

func (m *mockAPIClient) GetGuestConfig(_ context.Context, node, guestType, vmid string) (*GuestConfig, error) {
	id := guestType + "/" + vmid
	if err, ok := m.configErr[id]; ok {
		return nil, err
	}
	cfg, ok := m.configs[id]
	if !ok {
		return nil, fmt.Errorf("no such guest %s on node %s", id, node)
	}
	return cfg, nil
}

func (m *mockAPIClient) GetGuestInterfaces(_ context.Context, node, guestType, vmid string) ([]GuestInterface, error) {
	_ = node
	return m.interfaces[guestType+"/"+vmid], nil
}

func newTestClient(api APIClient) *Client {
	return &Client{
		log:    zerolog.Nop(),
		api:    api,
		name:   "test",
		guests: make(map[string]*guest),
		states: make(map[string]guestState),
		assets: testAssets,
	}
}

func qemuGuest(id string, vmid int, name, status string) GuestResource {
	return GuestResource{ID: id, Node: "pve", VMID: vmid, Name: name, Type: guestTypeQemu, Status: status}
}

func enabledNotes(extra string) *GuestConfig {
	return &GuestConfig{Notes: "tsdproxy:\n  port:\n    443: 443/https:8080/http\n" + extra}
}

func drainEvents(t *testing.T, eventsChan chan targetproviders.TargetEvent) []targetproviders.TargetEvent {
	t.Helper()

	events := make([]targetproviders.TargetEvent, 0)
	for {
		select {
		case event := <-eventsChan:
			events = append(events, event)
		default:
			return events
		}
	}
}

func TestPollOnce_InitialScan(t *testing.T) {
	t.Parallel()

	api := &mockAPIClient{
		guests: []GuestResource{
			qemuGuest("qemu/100", 100, "app", statusRunning),
			qemuGuest("qemu/101", 101, "other", statusRunning),
			qemuGuest("qemu/102", 102, "stopped", statusStopped),
			qemuGuest("qemu/103", 103, "template", statusStopped),
			{ID: "storage/px", Node: "pve", Type: "storage", Status: "available"},
		},
		configs: map[string]*GuestConfig{
			"qemu/100": enabledNotes(""),
			"qemu/101": {Notes: "plain human notes"},
			"qemu/102": enabledNotes(""),
			"qemu/103": {Notes: "tsdproxy:\n  enable: true\n"}, // template must be skipped anyway
		},
	}
	c := newTestClient(api)

	eventsChan := make(chan targetproviders.TargetEvent, eventsChanTestSize)
	if err := c.pollOnce(context.Background(), eventsChan); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	events := drainEvents(t, eventsChan)
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d: %+v", len(events), events)
	}
	if events[0].ID != "qemu/100" || events[0].Action != targetproviders.ActionStartProxy {
		t.Errorf("event: got %+v, want start qemu/100", events[0])
	}

	if _, ok := c.states["qemu/102"]; !ok {
		t.Error("stopped guest must be snapshotted")
	}
}

func TestPollOnce_StopStartRestart(t *testing.T) {
	t.Parallel()

	api := &mockAPIClient{
		guests: []GuestResource{
			qemuGuest("qemu/100", 100, "app", statusRunning),
			qemuGuest("qemu/200", 200, "tracked", statusRunning),
			qemuGuest("qemu/300", 300, "stoppednow", statusStopped),
			qemuGuest("qemu/400", 400, "restarted", statusRunning),
		},
		configs: map[string]*GuestConfig{
			"qemu/100": enabledNotes(""),
			"qemu/200": {Notes: "tsdproxy:\n  enable: false\n"},
			"qemu/300": enabledNotes(""),
			"qemu/400": enabledNotes("  name: renamed\n"),
		},
	}
	c := newTestClient(api)

	// Seed the previous snapshot: all running and enabled.
	c.states = map[string]guestState{
		"qemu/200": {
			resource: qemuGuest("qemu/200", 200, "tracked", statusRunning), status: statusRunning,
			notes: mustParse(t, "tsdproxy:\n  port:\n    443: 443/https:8080/http\n"),
		},
		"qemu/300": {
			resource: qemuGuest("qemu/300", 300, "stoppednow", statusRunning), status: statusRunning,
			notes: mustParse(t, "tsdproxy:\n  port:\n    443: 443/https:8080/http\n"),
		},
		"qemu/400": {
			resource: qemuGuest("qemu/400", 400, "restarted", statusStopped), status: statusStopped,
			notes: mustParse(t, "tsdproxy:\n  port:\n    443: 443/https:8080/http\n"),
		},
	}
	// qemu/200 and qemu/300 have proxies.
	c.guests["qemu/200"] = &guest{}
	c.guests["qemu/300"] = &guest{}

	eventsChan := make(chan targetproviders.TargetEvent, eventsChanTestSize)
	if err := c.pollOnce(context.Background(), eventsChan); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	events := drainEvents(t, eventsChan)
	want := map[string]targetproviders.ActionType{
		"qemu/100": targetproviders.ActionStartProxy, // new since last poll
		"qemu/200": targetproviders.ActionStopProxy,  // disabled while running
		"qemu/300": targetproviders.ActionStopProxy,  // stopped while tracked
		"qemu/400": targetproviders.ActionStartProxy, // stopped → running
	}
	if len(events) != len(want) {
		t.Fatalf("expected %d events, got %d: %+v", len(want), len(events), events)
	}
	for _, event := range events {
		if want[event.ID] != event.Action {
			t.Errorf("event %s: got action %d, want %d", event.ID, event.Action, want[event.ID])
		}
	}
}

func TestPollOnce_ConfigChange(t *testing.T) {
	t.Parallel()

	api := &mockAPIClient{
		guests: []GuestResource{
			qemuGuest("qemu/100", 100, "tracked", statusRunning),
			qemuGuest("qemu/200", 200, "untracked", statusRunning),
		},
		configs: map[string]*GuestConfig{
			"qemu/100": enabledNotes("  name: changed\n"),
			"qemu/200": enabledNotes("  name: changed\n"),
		},
	}
	c := newTestClient(api)

	c.states = map[string]guestState{
		"qemu/100": {
			resource: qemuGuest("qemu/100", 100, "tracked", statusRunning), status: statusRunning,
			notes: mustParse(t, "tsdproxy:\n  port:\n    443: 443/https:8080/http\n"),
		},
		"qemu/200": {
			resource: qemuGuest("qemu/200", 200, "untracked", statusRunning), status: statusRunning,
			notes: mustParse(t, "tsdproxy:\n  port:\n    443: 443/https:8080/http\n"),
		},
	}
	c.guests["qemu/100"] = &guest{}

	eventsChan := make(chan targetproviders.TargetEvent, eventsChanTestSize)
	if err := c.pollOnce(context.Background(), eventsChan); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	events := drainEvents(t, eventsChan)
	want := map[string]targetproviders.ActionType{
		"qemu/100": targetproviders.ActionRestartProxy, // tracked + config changed
		"qemu/200": targetproviders.ActionStartProxy,   // untracked + config changed → retry
	}
	if len(events) != len(want) {
		t.Fatalf("expected %d events, got %d: %+v", len(want), len(events), events)
	}
	for _, event := range events {
		if want[event.ID] != event.Action {
			t.Errorf("event %s: got action %d, want %d", event.ID, event.Action, want[event.ID])
		}
	}
}

func TestPollOnce_Reenabled(t *testing.T) {
	t.Parallel()

	api := &mockAPIClient{
		guests:  []GuestResource{qemuGuest("qemu/100", 100, "app", statusRunning)},
		configs: map[string]*GuestConfig{"qemu/100": enabledNotes("")},
	}
	c := newTestClient(api)

	// Previously disabled while running.
	c.states["qemu/100"] = guestState{
		resource: qemuGuest("qemu/100", 100, "app", statusRunning),
		status:   statusRunning,
		notes:    &notesConfig{Enable: false, Settings: map[string]string{}},
	}

	eventsChan := make(chan targetproviders.TargetEvent, 1)
	if err := c.pollOnce(context.Background(), eventsChan); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	events := drainEvents(t, eventsChan)
	if len(events) != 1 || events[0].Action != targetproviders.ActionStartProxy {
		t.Fatalf("expected start event, got %+v", events)
	}
}

func TestPollOnce_Deleted(t *testing.T) {
	t.Parallel()

	api := &mockAPIClient{
		guests:  []GuestResource{qemuGuest("qemu/200", 200, "alive", statusRunning)},
		configs: map[string]*GuestConfig{"qemu/200": enabledNotes("")},
	}
	c := newTestClient(api)

	c.states["qemu/100"] = guestState{resource: qemuGuest("qemu/100", 100, "gone", statusRunning), status: statusRunning}
	c.guests["qemu/100"] = &guest{}

	eventsChan := make(chan targetproviders.TargetEvent, eventsChanTestSize)
	if err := c.pollOnce(context.Background(), eventsChan); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	events := drainEvents(t, eventsChan)
	if len(events) != 2 {
		t.Fatalf("expected 2 events (start alive, stop deleted), got %+v", events)
	}

	if _, ok := c.states["qemu/100"]; ok {
		t.Error("deleted guest snapshot must be dropped")
	}
}

func TestPollOnce_ConfigErrorKeepsSnapshot(t *testing.T) {
	t.Parallel()

	api := &mockAPIClient{
		guests:    []GuestResource{qemuGuest("qemu/100", 100, "app", statusRunning)},
		configs:   map[string]*GuestConfig{"qemu/100": enabledNotes("")},
		configErr: map[string]error{"qemu/100": errors.New("boom")},
	}
	c := newTestClient(api)

	prev := guestState{
		resource: qemuGuest("qemu/100", 100, "app", statusRunning), status: statusRunning,
		notes: mustParse(t, "tsdproxy:\n  port:\n    443: 443/https:8080/http\n"),
	}
	c.states["qemu/100"] = prev
	c.guests["qemu/100"] = &guest{}

	eventsChan := make(chan targetproviders.TargetEvent, 1)
	if err := c.pollOnce(context.Background(), eventsChan); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if events := drainEvents(t, eventsChan); len(events) != 0 {
		t.Errorf("expected no events on config fetch error, got %+v", events)
	}
	if got := c.states["qemu/100"]; got.notes == nil {
		t.Error("snapshot must be preserved on config fetch error")
	}
}

func TestClient_AddTarget(t *testing.T) {
	t.Parallel()

	api := &mockAPIClient{
		guests: []GuestResource{qemuGuest("qemu/100", 100, "MyApp", statusRunning)},
		configs: map[string]*GuestConfig{
			"qemu/100": {Notes: "tsdproxy:\n  port:\n    443: 443/https:8080/http\n", Tags: "debian;web"},
		},
		interfaces: map[string][]GuestInterface{
			"qemu/100": {{Name: "eth0", Addresses: []netip.Addr{netip.MustParseAddr("192.168.1.50")}}},
		},
	}
	c := newTestClient(api)

	pcfg, err := c.AddTarget("qemu/100")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if pcfg.TargetID != "qemu/100" {
		t.Errorf("TargetID: got %q", pcfg.TargetID)
	}
	if pcfg.Hostname != "myapp" {
		t.Errorf("Hostname: got %q, want lowercased guest name", pcfg.Hostname)
	}
	if pcfg.TargetImage != "debian;web" {
		t.Errorf("TargetImage: got %q, want tags", pcfg.TargetImage)
	}

	port, ok := pcfg.Ports["tsdproxy.port.443"]
	if !ok {
		t.Fatalf("expected port, got %+v", pcfg.Ports)
	}
	if got := port.GetFirstTargetString(); got != "http://192.168.1.50:8080" {
		t.Errorf("target: got %q, want http://192.168.1.50:8080", got)
	}

	if _, tracked := c.guests["qemu/100"]; !tracked {
		t.Error("AddTarget must track the guest")
	}
}

func TestClient_AddTargetErrors(t *testing.T) {
	t.Parallel()

	api := &mockAPIClient{
		guests: []GuestResource{
			qemuGuest("qemu/100", 100, "disabled", statusRunning),
			qemuGuest("qemu/101", 101, "stopped", statusStopped),
		},
		configs: map[string]*GuestConfig{
			"qemu/100": {Notes: "no tsdproxy here"},
			"qemu/101": enabledNotes(""),
		},
	}
	c := newTestClient(api)

	if _, err := c.AddTarget("qemu/100"); !errors.Is(err, ErrGuestNotEnabled) {
		t.Errorf("expected ErrGuestNotEnabled, got %v", err)
	}
	if _, err := c.AddTarget("qemu/101"); !errors.Is(err, ErrGuestNotRunning) {
		t.Errorf("expected ErrGuestNotRunning, got %v", err)
	}
	if _, err := c.AddTarget("qemu/999"); !errors.Is(err, ErrGuestNotFound) {
		t.Errorf("expected ErrGuestNotFound, got %v", err)
	}
}

func TestClient_DeleteProxy(t *testing.T) {
	t.Parallel()

	c := newTestClient(&mockAPIClient{})
	c.guests["qemu/100"] = &guest{}

	if err := c.DeleteProxy("qemu/100"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := c.DeleteProxy("qemu/100"); !errors.Is(err, targetproviders.ErrTargetNotFound) {
		t.Errorf("expected ErrTargetNotFound, got %v", err)
	}
}

func TestClient_ReResolveDoesNotTrack(t *testing.T) {
	t.Parallel()

	api := &mockAPIClient{
		guests:  []GuestResource{qemuGuest("qemu/100", 100, "app", statusRunning)},
		configs: map[string]*GuestConfig{"qemu/100": enabledNotes("")},
	}
	c := newTestClient(api)

	if _, err := c.ReResolve("qemu/100"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, tracked := c.guests["qemu/100"]; tracked {
		t.Error("ReResolve must not track the guest")
	}
}

func mustParse(t *testing.T, notes string) *notesConfig {
	t.Helper()

	cfg, err := parseTsdproxyConfig(notes)
	if err != nil {
		t.Fatalf("error parsing notes: %v", err)
	}
	return cfg
}

const eventsChanTestSize = 16
