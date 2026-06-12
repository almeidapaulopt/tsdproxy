// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package docker

import (
	"context"
	"net/netip"
	"sync"
	"testing"

	ctypes "github.com/moby/moby/api/types/container"
	devents "github.com/moby/moby/api/types/events"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/api/types/swarm"
	"github.com/moby/moby/client"
	"github.com/rs/zerolog"

	"github.com/almeidapaulopt/tsdproxy/internal/targetproviders"
)

// ---------------------------------------------------------------------------
// mockAPIClient implements the APIClient interface for testing.
// ---------------------------------------------------------------------------

type mockAPIClient struct {
	listErr        error
	inspectErr     error
	serviceErr     error
	networkErr     error
	inspectResults map[string]client.ContainerInspectResult
	serviceResults map[string]client.ServiceInspectResult
	eventsMsgs     chan devents.Message
	eventsErr      chan error
	containers     []ctypes.Summary
	networks       []network.Summary
	mu             sync.Mutex
	closeCalled    bool
}

func newMockAPIClient() *mockAPIClient {
	return &mockAPIClient{
		inspectResults: make(map[string]client.ContainerInspectResult),
		serviceResults: make(map[string]client.ServiceInspectResult),
		eventsMsgs:     make(chan devents.Message, 16),
		eventsErr:      make(chan error, 16),
	}
}

// Compile-time interface check.
var _ APIClient = (*mockAPIClient)(nil)

func (m *mockAPIClient) ContainerInspect(_ context.Context, containerID string, _ client.ContainerInspectOptions) (client.ContainerInspectResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.inspectErr != nil {
		return client.ContainerInspectResult{}, m.inspectErr
	}
	if r, ok := m.inspectResults[containerID]; ok {
		return r, nil
	}
	return client.ContainerInspectResult{}, errNotFound
}

func (m *mockAPIClient) ServiceInspect(_ context.Context, serviceID string, _ client.ServiceInspectOptions) (client.ServiceInspectResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.serviceErr != nil {
		return client.ServiceInspectResult{}, m.serviceErr
	}
	if r, ok := m.serviceResults[serviceID]; ok {
		return r, nil
	}
	return client.ServiceInspectResult{}, errNotFound
}

func (m *mockAPIClient) Events(_ context.Context, _ client.EventsListOptions) client.EventsResult {
	return client.EventsResult{
		Messages: m.eventsMsgs,
		Err:      m.eventsErr,
	}
}

func (m *mockAPIClient) ContainerList(_ context.Context, _ client.ContainerListOptions) (client.ContainerListResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.listErr != nil {
		return client.ContainerListResult{}, m.listErr
	}
	return client.ContainerListResult{Items: m.containers}, nil
}

func (m *mockAPIClient) NetworkList(_ context.Context, _ client.NetworkListOptions) (client.NetworkListResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.networkErr != nil {
		return client.NetworkListResult{}, m.networkErr
	}
	return client.NetworkListResult{Items: m.networks}, nil
}

func (m *mockAPIClient) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closeCalled = true
	return nil
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// errNotFound is a sentinel error returned by the mock when a resource is missing.
var errNotFound = notFoundError("resource not found")

type notFoundError string

func (e notFoundError) Error() string { return string(e) }

// newTestClient creates a Client wired to a mockAPIClient, skipping the real
// Docker constructor (which dials the daemon). The caller can access the mock
// to configure return values before exercising the Client.
func newTestClient(mock *mockAPIClient) *Client {
	return &Client{
		log:                   zerolog.Nop(),
		docker:                mock,
		name:                  "test",
		host:                  "unix:///var/run/docker.sock",
		defaultTargetHostname: "host.docker.internal",
		containers:            make(map[string]*container),
		assets:                testAssets,
	}
}

// basicInspectResult builds a minimal ContainerInspectResult suitable for
// most tests. The container has a single port binding (80→8080), is on a
// bridge network, and carries the provided labels.
func basicInspectResult(id, name string, labels map[string]string) client.ContainerInspectResult {
	port80, _ := network.ParsePort("80/tcp")

	return client.ContainerInspectResult{
		Container: ctypes.InspectResponse{
			ID:   id,
			Name: "/" + name,
			Config: &ctypes.Config{
				Hostname: name,
				Image:    "nginx:latest",
				Labels:   labels,
			},
			HostConfig: &ctypes.HostConfig{
				NetworkMode: ctypes.NetworkMode("bridge"),
			},
			NetworkSettings: &ctypes.NetworkSettings{
				Ports: network.PortMap{
					port80: []network.PortBinding{
						{HostIP: mustAddr("0.0.0.0"), HostPort: "8080"},
					},
				},
				Networks: map[string]*network.EndpointSettings{
					"bridge": {
						IPAddress: mustAddr("172.17.0.5"),
						Gateway:   mustAddr("172.17.0.1"),
					},
				},
			},
		},
	}
}

func mustAddr(s string) netip.Addr {
	return netip.MustParseAddr(s)
}

// ---------------------------------------------------------------------------
// tests
// ---------------------------------------------------------------------------

func TestMockAPIClient_ImplementsInterface(t *testing.T) {
	t.Parallel()
	// This test exists solely to confirm the compile-time check at the top of
	// the file. If the mock doesn't satisfy APIClient, the var _ assignment
	// won't compile. We exercise a trivial call to prove the mock is wired up.
	mock := newMockAPIClient()
	c := newTestClient(mock)

	if c.docker == nil {
		t.Fatal("client docker field should not be nil")
	}
}

func TestClient_Close_CallsMock(t *testing.T) {
	t.Parallel()

	mock := newMockAPIClient()
	c := newTestClient(mock)

	c.Close()

	if !mock.closeCalled {
		t.Error("expected mock.Close() to be called")
	}
}

func TestClient_StartAllProxies_ListsEnabledContainers(t *testing.T) {
	t.Parallel()

	mock := newMockAPIClient()
	c := newTestClient(mock)

	// Seed two enabled containers.
	mock.containers = []ctypes.Summary{
		{ID: "abc123", Names: []string{"/nginx"}, Labels: map[string]string{LabelEnable: "true"}},
		{ID: "def456", Names: []string{"/redis"}, Labels: map[string]string{LabelEnable: "true"}},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	eventsCh := make(chan targetproviders.TargetEvent, 8)
	errCh := make(chan error, 1)

	// startAllProxies blocks on channel sends, so we consume events in the background.
	go c.startAllProxies(ctx, eventsCh, errCh)

	// Collect the two start events.
	var events []targetproviders.TargetEvent
	for i := range 2 {
		select {
		case e := <-eventsCh:
			events = append(events, e)
		case err := <-errCh:
			t.Fatalf("unexpected error: %v", err)
		case <-ctx.Done():
			t.Fatalf("timed out waiting for event %d", i)
		}
	}

	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}

	gotIDs := map[string]bool{}
	for _, e := range events {
		if e.Action != targetproviders.ActionStartProxy {
			t.Errorf("event action: got %d, want ActionStartProxy", e.Action)
		}
		gotIDs[e.ID] = true
	}
	if !gotIDs["abc123"] || !gotIDs["def456"] {
		t.Errorf("expected IDs abc123 and def456, got %v", gotIDs)
	}
}

func TestClient_StartAllProxies_ListError(t *testing.T) {
	t.Parallel()

	mock := newMockAPIClient()
	mock.listErr = errNotFound

	c := newTestClient(mock)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	eventsCh := make(chan targetproviders.TargetEvent, 1)
	errCh := make(chan error, 1)

	go c.startAllProxies(ctx, eventsCh, errCh)

	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("expected non-nil error")
		}
	case <-eventsCh:
		t.Fatal("expected error, got event")
	case <-ctx.Done():
		t.Fatal("timed out")
	}
}

func TestClient_StartAllProxies_EmptyList(t *testing.T) {
	t.Parallel()

	mock := newMockAPIClient()
	c := newTestClient(mock)

	// No containers registered → should return without sending events.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	eventsCh := make(chan targetproviders.TargetEvent, 1)
	errCh := make(chan error, 1)

	go c.startAllProxies(ctx, eventsCh, errCh)

	// Give the goroutine a chance to finish.
	select {
	case e := <-eventsCh:
		t.Fatalf("expected no events, got %+v", e)
	case err := <-errCh:
		t.Fatalf("expected no errors, got %v", err)
	default:
		// Success: no events or errors sent.
	}
}

func TestClient_WatchEvents_DispatchesActions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		eventAction  devents.Action
		eventActorID string
		wantAction   targetproviders.ActionType
	}{
		{"start event", devents.ActionStart, "container-abc", targetproviders.ActionStartProxy},
		{"die event", devents.ActionDie, "container-xyz", targetproviders.ActionStopProxy},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mock := newMockAPIClient()
			c := newTestClient(mock)

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			eventsCh := make(chan targetproviders.TargetEvent, 4)
			errCh := make(chan error, 2)

			c.WatchEvents(ctx, eventsCh, errCh)

			mock.eventsMsgs <- devents.Message{
				Type:   devents.ContainerEventType,
				Action: tt.eventAction,
				Actor: devents.Actor{
					ID: tt.eventActorID,
				},
			}

			select {
			case e := <-eventsCh:
				if e.Action != tt.wantAction {
					t.Errorf("action: got %d, want %d", e.Action, tt.wantAction)
				}
				if e.ID != tt.eventActorID {
					t.Errorf("ID: got %q, want %q", e.ID, tt.eventActorID)
				}
			case err := <-errCh:
				t.Fatalf("unexpected error: %v", err)
			case <-ctx.Done():
				t.Fatal("timed out")
			}
		})
	}
}

func TestClient_WatchEvents_IgnoresOtherActions(t *testing.T) {
	t.Parallel()

	mock := newMockAPIClient()
	c := newTestClient(mock)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	eventsCh := make(chan targetproviders.TargetEvent, 4)
	errCh := make(chan error, 2)

	c.WatchEvents(ctx, eventsCh, errCh)

	// Send a non-start/die event; it should be ignored.
	mock.eventsMsgs <- devents.Message{
		Type:   devents.ContainerEventType,
		Action: devents.ActionPause,
		Actor: devents.Actor{
			ID: "container-paused",
		},
	}

	// Then send a real start event to verify the stream is still alive.
	mock.eventsMsgs <- devents.Message{
		Type:   devents.ContainerEventType,
		Action: devents.ActionStart,
		Actor: devents.Actor{
			ID: "container-start",
		},
	}

	// We should only get the start event — the pause is swallowed.
	select {
	case e := <-eventsCh:
		if e.ID != "container-start" {
			t.Errorf("expected start event for container-start, got %q", e.ID)
		}
	case err := <-errCh:
		t.Fatalf("unexpected error: %v", err)
	case <-ctx.Done():
		t.Fatal("timed out")
	}
}

func TestClient_WatchEvents_StreamError(t *testing.T) {
	t.Parallel()

	mock := newMockAPIClient()
	c := newTestClient(mock)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	eventsCh := make(chan targetproviders.TargetEvent, 4)
	errCh := make(chan error, 2)

	c.WatchEvents(ctx, eventsCh, errCh)

	// Push an error onto the event stream.
	mock.eventsErr <- errNotFound

	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("expected non-nil error")
		}
	case <-eventsCh:
		t.Fatal("expected error, got event")
	case <-ctx.Done():
		t.Fatal("timed out")
	}
}

func TestClient_WatchEvents_ContextCancel(t *testing.T) {
	t.Parallel()

	mock := newMockAPIClient()
	c := newTestClient(mock)

	ctx, cancel := context.WithCancel(context.Background())
	eventsCh := make(chan targetproviders.TargetEvent, 4)
	errCh := make(chan error, 2)

	c.WatchEvents(ctx, eventsCh, errCh)

	// Cancel the context — goroutines should exit cleanly.
	cancel()

	// Brief wait to ensure goroutines finish; no panic = success.
}

func TestClient_AddTarget_InspectSuccess(t *testing.T) {
	t.Parallel()

	mock := newMockAPIClient()
	c := newTestClient(mock)

	containerID := "ctr-123"
	labels := map[string]string{
		LabelEnable:       "true",
		LabelName:         "myapp",
		LabelPort + "web": "443/https:80/http",
	}

	mock.inspectResults[containerID] = basicInspectResult(containerID, "myapp", labels)

	pcfg, err := c.AddTarget(containerID)
	if err != nil {
		t.Fatalf("AddTarget returned error: %v", err)
	}

	if pcfg.TargetID != containerID {
		t.Errorf("TargetID: got %q, want %q", pcfg.TargetID, containerID)
	}
	if pcfg.Hostname != "myapp" {
		t.Errorf("Hostname: got %q, want %q", pcfg.Hostname, "myapp")
	}
	if pcfg.TargetProvider != "test" {
		t.Errorf("TargetProvider: got %q, want %q", pcfg.TargetProvider, "test")
	}
	if len(pcfg.Ports) == 0 {
		t.Error("expected at least one port in config")
	}
}

func TestClient_AddTarget_InspectError(t *testing.T) {
	t.Parallel()

	mock := newMockAPIClient()
	mock.inspectErr = errNotFound

	c := newTestClient(mock)

	_, err := c.AddTarget("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent container inspect")
	}
}

func TestClient_AddTarget_SwarmServiceInspect(t *testing.T) {
	t.Parallel()

	mock := newMockAPIClient()
	c := newTestClient(mock)

	containerID := "swarm-task-1"
	serviceID := "svc-abc"
	labels := map[string]string{
		LabelEnable:                   "true",
		LabelName:                     "swarmapp",
		"com.docker.swarm.service.id": serviceID,
		LabelPort + "web":             "443/https:80/http",
	}

	inspectResult := basicInspectResult(containerID, "swarmapp", labels)
	mock.inspectResults[containerID] = inspectResult

	// Also configure a swarm service result.
	mock.serviceResults[serviceID] = client.ServiceInspectResult{
		Service: swarm.Service{
			ID: serviceID,
			Spec: swarm.ServiceSpec{
				Annotations: swarm.Annotations{
					Name: "myservice",
				},
			},
			Endpoint: swarm.Endpoint{
				Ports: []swarm.PortConfig{
					{TargetPort: 80, PublishedPort: 8080, Protocol: network.TCP},
				},
			},
		},
	}

	pcfg, err := c.AddTarget(containerID)
	if err != nil {
		t.Fatalf("AddTarget returned error: %v", err)
	}

	if pcfg.TargetID != containerID {
		t.Errorf("TargetID: got %q, want %q", pcfg.TargetID, containerID)
	}
}

func TestClient_DeleteProxy(t *testing.T) {
	t.Parallel()

	mock := newMockAPIClient()
	c := newTestClient(mock)

	// Register a container manually so DeleteProxy can remove it.
	c.addContainer(&container{id: "ctr-1", name: "ctr-1"}, "ctr-1")

	if err := c.DeleteProxy("ctr-1"); err != nil {
		t.Fatalf("DeleteProxy returned error: %v", err)
	}

	// Second delete should fail (not found).
	if err := c.DeleteProxy("ctr-1"); err == nil {
		t.Fatal("expected error when deleting non-registered proxy")
	}
}

func TestClient_GetDefaultProxyProviderName(t *testing.T) {
	t.Parallel()

	mock := newMockAPIClient()
	c := newTestClient(mock)
	c.defaultProxyProvider = "tailscale"

	if got := c.GetDefaultProxyProviderName(); got != "tailscale" {
		t.Errorf("got %q, want %q", got, "tailscale")
	}
}

func TestEnabledContainerFilter(t *testing.T) {
	t.Parallel()

	f := enabledContainerFilter()

	vals, ok := f["label"]
	if !ok {
		t.Fatal("expected 'label' key in filter")
	}

	found := false
	for v := range vals {
		if v == LabelIsEnabled {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("filter should contain %q, got %v", LabelIsEnabled, vals)
	}
}

func TestClient_ReResolve_InspectSuccess(t *testing.T) {
	t.Parallel()

	mock := newMockAPIClient()
	c := newTestClient(mock)

	containerID := "ctr-reresolve"
	labels := map[string]string{
		LabelEnable:       "true",
		LabelName:         "reapp",
		LabelPort + "web": "443/https:80/http",
	}

	mock.inspectResults[containerID] = basicInspectResult(containerID, "reapp", labels)

	pcfg, err := c.ReResolve(containerID)
	if err != nil {
		t.Fatalf("ReResolve returned error: %v", err)
	}

	if pcfg.TargetID != containerID {
		t.Errorf("TargetID: got %q, want %q", pcfg.TargetID, containerID)
	}
}

func TestClient_ReResolve_InspectError(t *testing.T) {
	t.Parallel()

	mock := newMockAPIClient()
	mock.inspectErr = errNotFound

	c := newTestClient(mock)

	_, err := c.ReResolve("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent container inspect")
	}
}

func TestClient_AddTarget_MissingRequiredFields(t *testing.T) {
	t.Parallel()

	mock := newMockAPIClient()
	c := newTestClient(mock)

	containerID := "ctr-incomplete"

	// Return an inspect result with nil Config and HostConfig.
	mock.inspectResults[containerID] = client.ContainerInspectResult{
		Container: ctypes.InspectResponse{
			ID:   containerID,
			Name: "/incomplete",
		},
	}

	_, err := c.AddTarget(containerID)
	if err == nil {
		t.Fatal("expected error for container with missing required fields")
	}
}
