// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package tailscale

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/almeidapaulopt/tsdproxy/internal/model"
)

const testPollInterval = 10 * time.Millisecond

func TestClassifyState_NeedsLogin_WithAuthURL(t *testing.T) {
	evt := classifyState("NeedsLogin", "https://login.ts.net", "")
	if evt.Status != model.ProxyStatusAuthenticating {
		t.Fatalf("expected Authenticating, got %v", evt.Status)
	}
	if evt.AuthURL != "https://login.ts.net" {
		t.Fatalf("expected auth URL, got %q", evt.AuthURL)
	}
}

func TestClassifyState_NeedsLogin_NoAuthURL(t *testing.T) {
	evt := classifyState("NeedsLogin", "", "")
	if evt.Status != model.ProxyStatusAuthFailed {
		t.Fatalf("expected AuthFailed, got %v", evt.Status)
	}
	if evt.ErrorMessage == "" {
		t.Fatalf("expected error message for NeedsLogin without auth URL")
	}
}

func TestClassifyState_Starting(t *testing.T) {
	evt := classifyState("Starting", "", "")
	if evt.Status != model.ProxyStatusStarting {
		t.Fatalf("expected Starting, got %v", evt.Status)
	}
}

func TestClassifyState_Running(t *testing.T) {
	evt := classifyState("Running", "", "myhost.tailnet.ts.net")
	if evt.Status != model.ProxyStatusRunning {
		t.Fatalf("expected Running, got %v", evt.Status)
	}
	if evt.URL != "myhost.tailnet.ts.net" {
		t.Fatalf("expected URL, got %q", evt.URL)
	}
}

func TestClassifyState_NoState(t *testing.T) {
	evt := classifyState("NoState", "", "")
	if evt.Status != model.ProxyStatusStarting {
		t.Fatalf("expected Starting, got %v", evt.Status)
	}
}

func TestClassifyState_Stopped(t *testing.T) {
	evt := classifyState("Stopped", "", "")
	if evt.Status != model.ProxyStatusStopped {
		t.Fatalf("expected Stopped, got %v", evt.Status)
	}
}

func TestClassifyState_Unknown(t *testing.T) {
	evt := classifyState("SomethingWeird", "", "")
	if evt.Status != model.ProxyStatusError {
		t.Fatalf("expected Error, got %v", evt.Status)
	}
	if evt.ErrorMessage != "unknown state: SomethingWeird" {
		t.Fatalf("unexpected error message: %q", evt.ErrorMessage)
	}
}

func TestClassifyState_NeedsMachineAuth_WithAuthURL(t *testing.T) {
	evt := classifyState("NeedsMachineAuth", "https://login.ts.net/approve", "")
	if evt.Status != model.ProxyStatusAwaitingApproval {
		t.Fatalf("expected AwaitingApproval, got %v", evt.Status)
	}
	if evt.AuthURL != "https://login.ts.net/approve" {
		t.Fatalf("expected auth URL, got %q", evt.AuthURL)
	}
}

func TestClassifyState_NeedsMachineAuth_NoAuthURL(t *testing.T) {
	evt := classifyState("NeedsMachineAuth", "", "")
	if evt.Status != model.ProxyStatusAwaitingApproval {
		t.Fatalf("expected AwaitingApproval, got %v", evt.Status)
	}
}

type statusResponse struct {
	err          error
	backendState string
	authURL      string
	dnsName      string
	selfOK       bool
}

type mockStatusSource struct {
	resp        statusResponse
	statusCalls int
	mu          sync.Mutex
}

func newMockStatusSource() *mockStatusSource {
	return &mockStatusSource{}
}

func (s *mockStatusSource) getStatus(_ context.Context) (string, string, string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.statusCalls++
	r := s.resp
	return r.backendState, r.authURL, r.dnsName, r.selfOK, r.err
}

func (s *mockStatusSource) setStatusResp(resp statusResponse) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.resp = resp
}

func (s *mockStatusSource) getStatusCalls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.statusCalls
}

func TestWatch_NilClient_CallsOnDone(t *testing.T) {
	done := make(chan struct{}, 1)
	w := NewStatusWatcher(StatusWatcherConfig{
		OnDone:       func() { done <- struct{}{} },
		PollInterval: testPollInterval,
	})

	w.Watch(context.Background(), nil)

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("onDone was not called")
	}
}

func TestWatch_StatusError_TransientRetry(t *testing.T) {
	mockSrc := newMockStatusSource()
	mockSrc.setStatusResp(statusResponse{err: errors.New("transient status error")})

	done := make(chan struct{}, 1)

	w := NewStatusWatcher(StatusWatcherConfig{
		OnDone:       func() { done <- struct{}{} },
		PollInterval: testPollInterval,
	})
	w.source = mockSrc

	ctx, cancel := context.WithCancel(context.Background())
	go w.Watch(ctx, nil)

	time.Sleep(3 * time.Second)

	calls := mockSrc.getStatusCalls()
	t.Logf("status calls after transient error: %d", calls)
	if calls < 2 {
		cancel()
		t.Fatalf("expected >1 status calls despite transient error, got %d", calls)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("onDone not called after cancel")
	}
}

func TestWatch_StatusError_NetErrClosed_Retry(t *testing.T) {
	mockSrc := newMockStatusSource()

	done := make(chan struct{}, 1)
	w := NewStatusWatcher(StatusWatcherConfig{
		OnDone:       func() { done <- struct{}{} },
		PollInterval: testPollInterval,
	})
	w.source = mockSrc

	ctx, cancel := context.WithCancel(context.Background())
	mockSrc.setStatusResp(statusResponse{err: net.ErrClosed})

	go w.Watch(ctx, nil)

	time.Sleep(60 * time.Millisecond)

	calls := mockSrc.getStatusCalls()
	t.Logf("status calls after net.ErrClosed: %d", calls)
	if calls < 2 {
		t.Fatalf("expected >1 status calls despite net.ErrClosed, got %d", calls)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("onDone not called after cancel")
	}
}

func TestWatch_NeedsLogin_WithAuthURL(t *testing.T) {
	mockSrc := newMockStatusSource()
	mockSrc.setStatusResp(statusResponse{
		backendState: "NeedsLogin",
		authURL:      "https://login.ts.net/abc",
	})

	events := make(chan NodeEvent, 2)
	done := make(chan struct{}, 1)

	w := NewStatusWatcher(StatusWatcherConfig{
		OnEvent:      func(evt NodeEvent) { events <- evt },
		OnDone:       func() { done <- struct{}{} },
		PollInterval: testPollInterval,
	})
	w.source = mockSrc

	ctx, cancel := context.WithCancel(context.Background())
	go w.Watch(ctx, nil)

	select {
	case evt := <-events:
		if evt.Status != model.ProxyStatusAuthenticating {
			t.Fatalf("expected Authenticating, got %v", evt.Status)
		}
		if evt.AuthURL != "https://login.ts.net/abc" {
			t.Fatalf("expected auth URL, got %q", evt.AuthURL)
		}
	case <-time.After(time.Second):
		t.Fatal("no event received")
	}

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("onDone not called")
	}
}

// waitForStatus drains events until one with the wanted status arrives or the
// deadline elapses. It returns the matching event.
func waitForStatus(t *testing.T, events <-chan NodeEvent, want model.ProxyStatus) NodeEvent {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		select {
		case evt := <-events:
			if evt.Status == want {
				return evt
			}
		case <-deadline:
			t.Fatalf("did not observe %v event before deadline", want)
		}
	}
}

// waitForStatusNoAuthFailed behaves like waitForStatus but fails the test if an
// AuthFailed event is observed before the wanted status. This prevents a
// debounce regression from being silently swallowed by the drain loop.
func waitForStatusNoAuthFailed(t *testing.T, events <-chan NodeEvent, want model.ProxyStatus) NodeEvent {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		select {
		case evt := <-events:
			if evt.Status == model.ProxyStatusAuthFailed {
				t.Fatalf("unexpected AuthFailed before %v event", want)
			}
			if evt.Status == want {
				return evt
			}
		case <-deadline:
			t.Fatalf("did not observe %v event before deadline", want)
		}
	}
}

// TestWatch_NeedsLogin_NoAuthURL_PersistsToAuthFailed verifies that a genuinely
// stuck NeedsLogin-with-no-authURL state still surfaces AuthFailed once it has
// persisted past the first-boot grace window, so bad auth keys are not masked.
func TestWatch_NeedsLogin_NoAuthURL_PersistsToAuthFailed(t *testing.T) {
	mockSrc := newMockStatusSource()
	mockSrc.setStatusResp(statusResponse{
		backendState: "NeedsLogin",
		authURL:      "",
	})

	events := make(chan NodeEvent, 16)
	done := make(chan struct{}, 1)

	w := NewStatusWatcher(StatusWatcherConfig{
		OnEvent:      func(evt NodeEvent) { events <- evt },
		OnDone:       func() { done <- struct{}{} },
		PollInterval: testPollInterval,
	})
	w.source = mockSrc

	ctx, cancel := context.WithCancel(context.Background())
	go w.Watch(ctx, nil)

	evt := waitForStatus(t, events, model.ProxyStatusAuthFailed)
	if evt.ErrorMessage == "" {
		t.Fatalf("expected error message on persisted AuthFailed")
	}

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("onDone not called")
	}
}

// scriptedStatusSource returns a fixed sequence of responses, one per poll,
// repeating the final entry once the script is exhausted. This makes the exact
// poll order deterministic regardless of the watcher's timing.
type scriptedStatusSource struct {
	script []statusResponse
	idx    int
	mu     sync.Mutex
}

func (s *scriptedStatusSource) getStatus(_ context.Context) (string, string, string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.script[s.idx]
	if s.idx < len(s.script)-1 {
		s.idx++
	}
	return r.backendState, r.authURL, r.dnsName, r.selfOK, r.err
}

// TestWatch_NeedsLogin_NoAuthURL_FirstBootDebounced verifies that a transient
// NeedsLogin-with-no-authURL window that clears to Running within the grace
// threshold never emits the misleading AuthFailed status (issue #478).
func TestWatch_NeedsLogin_NoAuthURL_FirstBootDebounced(t *testing.T) {
	src := &scriptedStatusSource{script: []statusResponse{
		{backendState: "NeedsLogin", authURL: ""},
		{backendState: "NeedsLogin", authURL: ""},
		{backendState: "Running", dnsName: "host.ts.net", selfOK: true},
	}}

	events := make(chan NodeEvent, 16)
	done := make(chan struct{}, 1)

	w := NewStatusWatcher(StatusWatcherConfig{
		OnEvent:      func(evt NodeEvent) { events <- evt },
		OnDone:       func() { done <- struct{}{} },
		PollInterval: testPollInterval,
	})
	w.source = src

	ctx, cancel := context.WithCancel(context.Background())
	go w.Watch(ctx, nil)

	// Within the grace window the misleading AuthFailed is suppressed (Starting),
	// then the node reaches Running before the threshold is crossed.
	first := waitForStatusNoAuthFailed(t, events, model.ProxyStatusStarting)
	if first.Status != model.ProxyStatusStarting {
		t.Fatalf("expected Starting during grace window, got %v", first.Status)
	}

	run := waitForStatusNoAuthFailed(t, events, model.ProxyStatusRunning)
	if run.URL != "host.ts.net" {
		t.Fatalf("expected URL host.ts.net, got %q", run.URL)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("onDone not called")
	}

	// Drain remaining events; none should be AuthFailed.
	for {
		select {
		case evt := <-events:
			if evt.Status == model.ProxyStatusAuthFailed {
				t.Fatalf("unexpected AuthFailed during debounced first boot")
			}
		default:
			return
		}
	}
}

// TestWatch_NeedsLogin_NoAuthURL_StreakResets verifies that an intervening
// non-NeedsLogin poll resets the debounce streak, so an alternating sequence
// never prematurely trips the AuthFailed threshold.
func TestWatch_NeedsLogin_NoAuthURL_StreakResets(t *testing.T) {
	// Alternate NeedsLogin(no authURL) with Starting more times than the grace
	// threshold; because the streak resets each time, AuthFailed must not fire.
	var script []statusResponse
	for i := 0; i < firstBootAuthGraceCycles+2; i++ {
		script = append(script,
			statusResponse{backendState: "NeedsLogin", authURL: ""},
			statusResponse{backendState: "Starting"},
		)
	}
	script = append(script, statusResponse{backendState: "Running", dnsName: "host.ts.net", selfOK: true})

	src := &scriptedStatusSource{script: script}

	events := make(chan NodeEvent, 64)
	done := make(chan struct{}, 1)

	w := NewStatusWatcher(StatusWatcherConfig{
		OnEvent:      func(evt NodeEvent) { events <- evt },
		OnDone:       func() { done <- struct{}{} },
		PollInterval: testPollInterval,
	})
	w.source = src

	ctx, cancel := context.WithCancel(context.Background())
	go w.Watch(ctx, nil)

	// Wait until the script reaches Running, proving we polled past the grace
	// threshold's worth of NeedsLogin entries without ever emitting AuthFailed.
	// The strict helper fails if any AuthFailed slips through rather than
	// silently discarding it.
	waitForStatusNoAuthFailed(t, events, model.ProxyStatusRunning)

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("onDone not called")
	}

	for {
		select {
		case evt := <-events:
			if evt.Status == model.ProxyStatusAuthFailed {
				t.Fatalf("AuthFailed fired despite streak resets")
			}
		default:
			return
		}
	}
}

func TestClassifyState_AuthFailed(t *testing.T) {
	evt := classifyState("NeedsLogin", "", "")
	if evt.Status != model.ProxyStatusAuthFailed {
		t.Fatalf("expected AuthFailed, got %v", evt.Status)
	}
}

func TestProxyStatus_DeviceConflict_String(t *testing.T) {
	ps := model.ProxyStatusDeviceConflict
	if ps.String() != "DeviceConflict" {
		t.Fatalf("expected DeviceConflict string, got %q", ps.String())
	}
}

func TestProxyStatus_Reconciling_String(t *testing.T) {
	ps := model.ProxyStatusReconciling
	if ps.String() != "Reconciling" {
		t.Fatalf("expected Reconciling string, got %q", ps.String())
	}
}

func TestWatch_Starting(t *testing.T) {
	mockSrc := newMockStatusSource()
	mockSrc.setStatusResp(statusResponse{backendState: "Starting"})

	events := make(chan NodeEvent, 2)
	done := make(chan struct{}, 1)

	w := NewStatusWatcher(StatusWatcherConfig{
		OnEvent:      func(evt NodeEvent) { events <- evt },
		OnDone:       func() { done <- struct{}{} },
		PollInterval: testPollInterval,
	})
	w.source = mockSrc

	ctx, cancel := context.WithCancel(context.Background())
	go w.Watch(ctx, nil)

	select {
	case evt := <-events:
		if evt.Status != model.ProxyStatusStarting {
			t.Fatalf("expected Starting, got %v", evt.Status)
		}
	case <-time.After(time.Second):
		t.Fatal("no event received")
	}

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("onDone not called")
	}
}

func TestWatch_Running(t *testing.T) {
	mockSrc := newMockStatusSource()
	mockSrc.setStatusResp(statusResponse{
		backendState: "Running",
		dnsName:      "myhost.tailnet.ts.net",
		selfOK:       true,
	})

	events := make(chan NodeEvent, 2)
	done := make(chan struct{}, 1)

	w := NewStatusWatcher(StatusWatcherConfig{
		OnEvent:      func(evt NodeEvent) { events <- evt },
		OnDone:       func() { done <- struct{}{} },
		PollInterval: testPollInterval,
	})
	w.source = mockSrc

	ctx, cancel := context.WithCancel(context.Background())
	go w.Watch(ctx, nil)

	select {
	case evt := <-events:
		if evt.Status != model.ProxyStatusRunning {
			t.Fatalf("expected Running, got %v", evt.Status)
		}
		if evt.URL != "myhost.tailnet.ts.net" {
			t.Fatalf("expected URL, got %q", evt.URL)
		}
	case <-time.After(time.Second):
		t.Fatal("no event received")
	}

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("onDone not called")
	}
}

func TestWatch_Running_SelfNil_Skipped(t *testing.T) {
	mockSrc := newMockStatusSource()
	mockSrc.setStatusResp(statusResponse{
		backendState: "Running",
		dnsName:      "myhost.tailnet.ts.net",
		selfOK:       false,
	})

	events := make(chan NodeEvent, 2)
	done := make(chan struct{}, 1)

	w := NewStatusWatcher(StatusWatcherConfig{
		OnEvent:      func(evt NodeEvent) { events <- evt },
		OnDone:       func() { done <- struct{}{} },
		PollInterval: testPollInterval,
	})
	w.source = mockSrc

	ctx, cancel := context.WithCancel(context.Background())
	go w.Watch(ctx, nil)

	time.Sleep(50 * time.Millisecond)

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("onDone not called")
	}

	select {
	case evt := <-events:
		t.Fatalf("expected no event for Self==nil, got %v", evt.Status)
	default:
	}
}

func TestWatch_ContextCancel_CleanExit(t *testing.T) {
	mockSrc := newMockStatusSource()
	mockSrc.setStatusResp(statusResponse{
		backendState: "Running",
		selfOK:       true,
		dnsName:      "x.ts.net",
	})

	done := make(chan struct{}, 1)

	w := NewStatusWatcher(StatusWatcherConfig{
		OnEvent:      func(NodeEvent) {},
		OnDone:       func() { done <- struct{}{} },
		PollInterval: testPollInterval,
	})
	w.source = mockSrc

	ctx, cancel := context.WithCancel(context.Background())
	go w.Watch(ctx, nil)

	cancel()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("onDone not called after context cancel")
	}
}

func TestWatch_StatusError_ContextCanceled_Exits(t *testing.T) {
	mockSrc := newMockStatusSource()
	mockSrc.setStatusResp(statusResponse{err: context.Canceled})

	events := make(chan NodeEvent, 4)
	done := make(chan struct{}, 1)

	w := NewStatusWatcher(StatusWatcherConfig{
		OnEvent:      func(evt NodeEvent) { events <- evt },
		OnDone:       func() { done <- struct{}{} },
		PollInterval: testPollInterval,
	})
	w.source = mockSrc

	w.Watch(context.Background(), nil)

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("onDone not called")
	}

	select {
	case evt := <-events:
		t.Fatalf("expected no event for context.Canceled status error, got %v", evt.Status)
	default:
	}
}

func TestWatch_MultipleEvents(t *testing.T) {
	mockSrc := newMockStatusSource()

	events := make(chan NodeEvent, 8)
	done := make(chan struct{}, 1)

	w := NewStatusWatcher(StatusWatcherConfig{
		OnEvent:      func(evt NodeEvent) { events <- evt },
		OnDone:       func() { done <- struct{}{} },
		PollInterval: testPollInterval,
	})
	w.source = mockSrc

	ctx, cancel := context.WithCancel(context.Background())
	go w.Watch(ctx, nil)

	states := []statusResponse{
		{backendState: "Starting"},
		{backendState: "NeedsLogin", authURL: "https://login.ts.net"},
		{backendState: "Running", dnsName: "host.ts.net", selfOK: true},
	}

	expected := []model.ProxyStatus{
		model.ProxyStatusStarting,
		model.ProxyStatusAuthenticating,
		model.ProxyStatusRunning,
	}

	for i, s := range states {
		mockSrc.setStatusResp(s)
		select {
		case evt := <-events:
			if evt.Status != expected[i] {
				t.Fatalf("event %d: expected %v, got %v", i, expected[i], evt.Status)
			}
		case <-time.After(time.Second):
			t.Fatalf("event %d not received", i)
		}
	}

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("onDone not called")
	}
}

func TestWatch_NilOnEvent_NoPanic(t *testing.T) {
	t.Parallel()

	mockSrc := newMockStatusSource()
	mockSrc.setStatusResp(statusResponse{
		backendState: "Running",
		dnsName:      "test.ts.net",
		selfOK:       true,
	})

	done := make(chan struct{}, 1)

	w := NewStatusWatcher(StatusWatcherConfig{
		OnEvent:      nil,
		OnDone:       func() { done <- struct{}{} },
		PollInterval: testPollInterval,
	})
	w.source = mockSrc

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	var panicked bool
	recoverCh := make(chan struct{}, 1)

	go func() {
		defer func() {
			if r := recover(); r != nil {
				panicked = true
			}
			recoverCh <- struct{}{}
		}()
		w.Watch(ctx, nil)
	}()

	select {
	case <-recoverCh:
		if panicked {
			t.Fatal("Watch panicked with nil OnEvent")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Watch did not complete in time")
	}

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("onDone was not called")
	}
}

func TestNewStatusWatcher_NilOnDone(t *testing.T) {
	w := NewStatusWatcher(StatusWatcherConfig{
		OnEvent:      func(NodeEvent) {},
		OnDone:       nil,
		PollInterval: testPollInterval,
	})

	done := make(chan struct{}, 1)
	w.onDone = func() { done <- struct{}{} }

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	w.Watch(ctx, nil)

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("onDone was not called")
	}
}
