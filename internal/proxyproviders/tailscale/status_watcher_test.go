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
	if evt.Status != model.ProxyStatusError {
		t.Fatalf("expected Error, got %v", evt.Status)
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

func TestWatch_NeedsLogin_NoAuthURL(t *testing.T) {
	mockSrc := newMockStatusSource()
	mockSrc.setStatusResp(statusResponse{
		backendState: "NeedsLogin",
		authURL:      "",
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
		if evt.Status != model.ProxyStatusError {
			t.Fatalf("expected Error, got %v", evt.Status)
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
