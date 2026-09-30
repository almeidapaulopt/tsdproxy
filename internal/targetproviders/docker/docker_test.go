// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package docker

import (
	"context"
	"testing"

	devents "github.com/moby/moby/api/types/events"
	"github.com/rs/zerolog"

	"github.com/almeidapaulopt/tsdproxy/internal/targetproviders"
)

func newTestDockerClient(t *testing.T) *Client {
	t.Helper()
	return &Client{
		log:        zerolog.Nop(),
		containers: make(map[string]*container),
	}
}

func TestGetStartEvent(t *testing.T) {
	t.Parallel()

	c := newTestDockerClient(t)
	ev := c.getStartEvent("ctr-123")

	if ev.ID != "ctr-123" {
		t.Errorf("ID = %q, want %q", ev.ID, "ctr-123")
	}
	if ev.Action != targetproviders.ActionStartProxy {
		t.Errorf("Action = %v, want %v", ev.Action, targetproviders.ActionStartProxy)
	}
	if ev.TargetProvider != c {
		t.Error("TargetProvider should be the client itself")
	}
}

func TestGetStopEvent(t *testing.T) {
	t.Parallel()

	c := newTestDockerClient(t)
	ev := c.getStopEvent("ctr-456")

	if ev.ID != "ctr-456" {
		t.Errorf("ID = %q, want %q", ev.ID, "ctr-456")
	}
	if ev.Action != targetproviders.ActionStopProxy {
		t.Errorf("Action = %v, want %v", ev.Action, targetproviders.ActionStopProxy)
	}
	if ev.TargetProvider != c {
		t.Error("TargetProvider should be the client itself")
	}
}

func TestAddContainer(t *testing.T) {
	t.Parallel()

	c := newTestDockerClient(t)
	ctn := &container{id: "ctr-789", name: "test-container"}

	c.addContainer(ctn, "ctr-789")

	if len(c.containers) != 1 {
		t.Fatalf("containers length = %d, want 1", len(c.containers))
	}
	if c.containers["ctr-789"] != ctn {
		t.Error("stored container does not match")
	}
}

func TestAddContainer_Overwrite(t *testing.T) {
	t.Parallel()

	c := newTestDockerClient(t)
	ctn1 := &container{id: "ctr-1", name: "first"}
	ctn2 := &container{id: "ctr-1", name: "second"}

	c.addContainer(ctn1, "ctr-1")
	c.addContainer(ctn2, "ctr-1")

	if c.containers["ctr-1"].name != "second" {
		t.Errorf("container name = %q, want %q", c.containers["ctr-1"].name, "second")
	}
}

func TestDeleteProxy_Success(t *testing.T) {
	t.Parallel()

	c := newTestDockerClient(t)
	c.containers["ctr-1"] = &container{id: "ctr-1"}

	if err := c.DeleteProxy("ctr-1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := c.containers["ctr-1"]; ok {
		t.Error("container should be deleted")
	}
}

func TestDeleteProxy_NotFound(t *testing.T) {
	t.Parallel()

	c := newTestDockerClient(t)

	if err := c.DeleteProxy("nonexistent"); err == nil {
		t.Fatal("expected error for nonexistent container")
	}
}

func TestGetDefaultProxyProviderName(t *testing.T) {
	t.Parallel()

	c := &Client{
		log:                  zerolog.Nop(),
		defaultProxyProvider: "my-provider",
	}

	if name := c.GetDefaultProxyProviderName(); name != "my-provider" {
		t.Errorf("got %q, want %q", name, "my-provider")
	}
}

func TestGetDefaultProxyProviderName_Empty(t *testing.T) {
	t.Parallel()

	c := &Client{
		log: zerolog.Nop(),
	}

	if name := c.GetDefaultProxyProviderName(); name != "" {
		t.Errorf("got %q, want empty", name)
	}
}

func TestSignalDisconnect_SendsError(t *testing.T) {
	t.Parallel()

	c := newTestDockerClient(t)
	errChan := make(chan error, 1)

	c.signalDisconnect(context.Background(), errChan)

	select {
	case err := <-errChan:
		if err == nil {
			t.Fatal("expected non-nil error")
		}
	default:
		t.Fatal("expected error to be sent to channel")
	}
}

func TestSignalDisconnect_CancelledContext(t *testing.T) {
	t.Parallel()

	c := newTestDockerClient(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	errChan := make(chan error, 1)
	c.signalDisconnect(ctx, errChan)

	select {
	case <-errChan:
		t.Fatal("should not send error when context is canceled")
	default:
	}
}

func TestSignalDisconnect_BlockingChannel(t *testing.T) {
	t.Parallel()

	c := newTestDockerClient(t)
	// Unbuffered channel with canceled context should not block
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	errChan := make(chan error)
	c.signalDisconnect(ctx, errChan)
}

func TestHandleDockerEvent_Start(t *testing.T) {
	t.Parallel()

	c := newTestDockerClient(t)
	eventsChan := make(chan targetproviders.TargetEvent, 1)

	devent := devents.Message{
		Actor: devents.Actor{
			ID: "ctr-start",
		},
		Action: devents.ActionStart,
	}

	ok := c.handleDockerEvent(context.Background(), devent, eventsChan)
	if !ok {
		t.Fatal("handleDockerEvent returned false")
	}

	ev := <-eventsChan
	if ev.ID != "ctr-start" {
		t.Errorf("ID = %q, want %q", ev.ID, "ctr-start")
	}
	if ev.Action != targetproviders.ActionStartProxy {
		t.Errorf("Action = %v, want %v", ev.Action, targetproviders.ActionStartProxy)
	}
}

func TestHandleDockerEvent_Stop(t *testing.T) {
	t.Parallel()

	c := newTestDockerClient(t)
	eventsChan := make(chan targetproviders.TargetEvent, 1)

	devent := devents.Message{
		Actor: devents.Actor{
			ID: "ctr-stop",
		},
		Action: devents.ActionDie,
	}

	ok := c.handleDockerEvent(context.Background(), devent, eventsChan)
	if !ok {
		t.Fatal("handleDockerEvent returned false")
	}

	ev := <-eventsChan
	if ev.ID != "ctr-stop" {
		t.Errorf("ID = %q, want %q", ev.ID, "ctr-stop")
	}
	if ev.Action != targetproviders.ActionStopProxy {
		t.Errorf("Action = %v, want %v", ev.Action, targetproviders.ActionStopProxy)
	}
}

func TestHandleDockerEvent_UnknownAction(t *testing.T) {
	t.Parallel()

	c := newTestDockerClient(t)
	eventsChan := make(chan targetproviders.TargetEvent, 1)

	devent := devents.Message{
		Actor: devents.Actor{
			ID: "ctr-unknown",
		},
		Action: devents.ActionExecStart,
	}

	ok := c.handleDockerEvent(context.Background(), devent, eventsChan)
	if !ok {
		t.Fatal("handleDockerEvent should return true for unknown actions")
	}

	select {
	case <-eventsChan:
		t.Fatal("should not send event for unknown action")
	default:
	}
}

func TestHandleDockerEvent_CancelledContext(t *testing.T) {
	t.Parallel()

	c := newTestDockerClient(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Unbuffered channel — select races between ctx.Done() and send.
	eventsChan := make(chan targetproviders.TargetEvent)

	devent := devents.Message{
		Actor: devents.Actor{
			ID: "ctr-start",
		},
		Action: devents.ActionStart,
	}

	ok := c.handleDockerEvent(ctx, devent, eventsChan)

	// Both outcomes are valid: the select randomly picks between
	// ctx.Done() (returns false) and send (succeeds with buffered
	// channel or in the race window). We just verify no panic/block.
	if ok {
		<-eventsChan
	}
}
