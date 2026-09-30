// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package tailscale

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/almeidapaulopt/tsdproxy/internal/model"
)

// testCmd is a simple command type used in EventLoop tests.
type testCmd struct {
	value int
}

// ---------------------------------------------------------------------------
// NewEventLoop
// ---------------------------------------------------------------------------

func TestEventLoopNewCreatesChannels(t *testing.T) {
	t.Parallel()

	el := NewEventLoop[testCmd](4)
	defer el.Close()

	if el.Cmds() == nil {
		t.Fatal("Cmds() should return a non-nil channel")
	}
	if el.Done() == nil {
		t.Fatal("Done() should return a non-nil channel")
	}
}

// ---------------------------------------------------------------------------
// Cmds() / Done()
// ---------------------------------------------------------------------------

func TestEventLoopCmdsReturnsReadOnlyChannel(t *testing.T) {
	t.Parallel()

	el := NewEventLoop[testCmd](2)
	defer el.Close()

	// Verify we can receive from the channel.
	go el.SendCmd(testCmd{value: 42})

	cmd := <-el.Cmds()
	if cmd.value != 42 {
		t.Fatalf("expected value 42, got %d", cmd.value)
	}
}

func TestEventLoopDoneNotClosedInitially(t *testing.T) {
	t.Parallel()

	el := NewEventLoop[testCmd](1)
	defer el.Close()

	select {
	case <-el.Done():
		t.Fatal("Done() should not be closed initially")
	default:
		// expected
	}
}

// ---------------------------------------------------------------------------
// SendCmd
// ---------------------------------------------------------------------------

func TestEventLoopSendCmdDeliversCommand(t *testing.T) {
	t.Parallel()

	el := NewEventLoop[testCmd](1)
	defer el.Close()

	el.SendCmd(testCmd{value: 7})

	cmd := <-el.Cmds()
	if cmd.value != 7 {
		t.Fatalf("expected value 7, got %d", cmd.value)
	}
}

// ---------------------------------------------------------------------------
// SendProducer (nil ctx)
// ---------------------------------------------------------------------------

func TestEventLoopSendProducerNilCtxReturnsTrue(t *testing.T) {
	t.Parallel()

	el := NewEventLoop[testCmd](1)

	ok := el.SendProducer(context.TODO(), testCmd{value: 1}) //nolint:staticcheck
	if !ok {
		t.Fatal("SendProducer with nil ctx should return true")
	}

	cmd := <-el.Cmds()
	if cmd.value != 1 {
		t.Fatalf("expected value 1, got %d", cmd.value)
	}

	el.Close()
}

func TestEventLoopSendProducerNilCtxReturnsFalseWhenClosed(t *testing.T) {
	t.Parallel()

	el := NewEventLoop[testCmd](0)
	el.Close()

	// Use a buffered done channel read to avoid blocking:
	// After Close, done is closed so SendProducer should return false immediately.
	ok := el.SendProducer(context.Background(), testCmd{value: 1})
	if ok {
		t.Fatal("SendProducer should return false after Close")
	}
}

// ---------------------------------------------------------------------------
// SendProducer (with context)
// ---------------------------------------------------------------------------

func TestEventLoopSendProducerWithCtxReturnsTrue(t *testing.T) {
	t.Parallel()

	el := NewEventLoop[testCmd](1)
	defer el.Close()

	ctx := context.Background()
	ok := el.SendProducer(ctx, testCmd{value: 2})
	if !ok {
		t.Fatal("SendProducer with background ctx should return true")
	}

	cmd := <-el.Cmds()
	if cmd.value != 2 {
		t.Fatalf("expected value 2, got %d", cmd.value)
	}
}

func TestEventLoopSendProducerWithCtxReturnsFalseWhenCancelled(t *testing.T) {
	t.Parallel()

	el := NewEventLoop[testCmd](0) // unbuffered — send will block
	defer el.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	ok := el.SendProducer(ctx, testCmd{value: 3})
	if ok {
		t.Fatal("SendProducer should return false with canceled context")
	}
}

func TestEventLoopSendProducerWithCtxReturnsFalseWhenClosed(t *testing.T) {
	t.Parallel()

	el := NewEventLoop[testCmd](0)
	el.Close()

	ctx := context.Background()
	ok := el.SendProducer(ctx, testCmd{value: 4})
	if ok {
		t.Fatal("SendProducer should return false after loop is closed")
	}
}

// ---------------------------------------------------------------------------
// SendPublic
// ---------------------------------------------------------------------------

func TestEventLoopSendPublicReturnsTrue(t *testing.T) {
	t.Parallel()

	el := NewEventLoop[testCmd](1)
	defer el.Close()

	ok := el.SendPublic(testCmd{value: 5})
	if !ok {
		t.Fatal("SendPublic should return true when loop is open")
	}

	cmd := <-el.Cmds()
	if cmd.value != 5 {
		t.Fatalf("expected value 5, got %d", cmd.value)
	}
}

func TestEventLoopSendPublicReturnsFalseWhenClosed(t *testing.T) {
	t.Parallel()

	el := NewEventLoop[testCmd](1)
	el.Close()

	ok := el.SendPublic(testCmd{value: 6})
	if ok {
		t.Fatal("SendPublic should return false after Close")
	}
}

// ---------------------------------------------------------------------------
// Close / IsClosed
// ---------------------------------------------------------------------------

func TestEventLoopCloseSignalsDone(t *testing.T) {
	t.Parallel()

	el := NewEventLoop[testCmd](1)
	el.Close()

	select {
	case <-el.Done():
		// expected
	case <-time.After(time.Second):
		t.Fatal("Done() should be closed after Close()")
	}
}

func TestEventLoopCloseIsIdempotent(t *testing.T) {
	t.Parallel()

	el := NewEventLoop[testCmd](1)

	// Calling Close twice should not panic (double close of channel would panic).
	el.Close()
	el.Close()

	// Done channel should be closed.
	select {
	case <-el.Done():
		// expected
	case <-time.After(time.Second):
		t.Fatal("Done() should be closed after Close()")
	}
}

func TestEventLoopIsClosedBeforeAndAfterClose(t *testing.T) {
	t.Parallel()

	el := NewEventLoop[testCmd](1)
	if el.IsClosed() {
		t.Fatal("IsClosed() should return false before Close()")
	}

	el.Close()
	if !el.IsClosed() {
		t.Fatal("IsClosed() should return true after Close()")
	}
}

// ---------------------------------------------------------------------------
// ScheduleIdleTimer
// ---------------------------------------------------------------------------

func TestEventLoopScheduleIdleTimerSendsCommand(t *testing.T) {
	t.Parallel()

	el := NewEventLoop[testCmd](1)
	defer el.Close()

	var received atomic.Int32

	timer := el.ScheduleIdleTimer(42, 10*time.Millisecond, func(gen int) testCmd {
		return testCmd{value: gen}
	})
	defer timer.Stop()

	cmd := <-el.Cmds()
	if cmd.value != 42 {
		t.Fatalf("expected generation 42, got %d", cmd.value)
	}
	if received.Load() != 0 {
		t.Fatal("unexpected atomic value")
	}
}

func TestEventLoopScheduleIdleTimerFiresCommand(t *testing.T) {
	t.Parallel()

	el := NewEventLoop[testCmd](1)
	defer el.Close()

	_ = el.ScheduleIdleTimer(1, 20*time.Millisecond, func(gen int) testCmd {
		return testCmd{value: gen}
	})

	select {
	case cmd := <-el.Cmds():
		if cmd.value != 1 {
			t.Errorf("command value = %d, want 1", cmd.value)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for idle timer command")
	}
}

// ---------------------------------------------------------------------------
// SendAndWait
// ---------------------------------------------------------------------------

func TestSendAndWaitReturnsValueOnSuccess(t *testing.T) {
	t.Parallel()

	el := NewEventLoop[testCmd](1)
	defer el.Close()

	reply := make(chan string, 1)

	// Simulate a loop that reads the command and sends a reply.
	go func() {
		cmd := <-el.Cmds()
		reply <- "got:" + string(rune('0'+cmd.value)) //nolint:gosec
	}()

	val, ok := SendAndWait[testCmd, string](el, testCmd{value: 3}, reply)
	if !ok {
		t.Fatal("SendAndWait should return true")
	}
	if val != "got:3" {
		t.Fatalf("expected 'got:3', got %q", val)
	}
}

func TestSendAndWaitReturnsZeroWhenLoopClosed(t *testing.T) {
	t.Parallel()

	el := NewEventLoop[testCmd](0)
	el.Close()

	reply := make(chan string, 1)
	val, ok := SendAndWait[testCmd, string](el, testCmd{value: 1}, reply)
	if ok {
		t.Fatal("SendAndWait should return false when loop is closed")
	}
	if val != "" {
		t.Fatalf("expected zero value '', got %q", val)
	}
}

// ---------------------------------------------------------------------------
// EventSub
// ---------------------------------------------------------------------------

func TestNewEventSubCreatesBufferedChannel(t *testing.T) {
	t.Parallel()

	sub := NewEventSub(8)
	if sub.Ch == nil {
		t.Fatal("Ch should be non-nil")
	}
	// Verify channel is open by sending an event.
	sub.Ch <- model.ProxyEvent{Status: model.ProxyStatusRunning}
	evt := <-sub.Ch
	if evt.Status != model.ProxyStatusRunning {
		t.Fatalf("expected ProxyStatusRunning, got %v", evt.Status)
	}
	sub.Close()
}

func TestEventSubCloseIsIdempotent(t *testing.T) {
	t.Parallel()

	sub := NewEventSub(1)

	// Close twice should not panic.
	sub.Close()
	sub.Close()

	// Channel should be closed.
	_, ok := <-sub.Ch
	if ok {
		t.Fatal("channel should be closed after Close")
	}
}

func TestEventSubChannelClosedAfterClose(t *testing.T) {
	t.Parallel()

	sub := NewEventSub(1)

	// Before close, channel should be open.
	select {
	case <-sub.Ch:
		t.Fatal("channel should block, not be closed")
	default:
		// expected: empty buffer, but not closed
	}

	sub.Close()

	// After close, reading should return zero value with ok=false.
	_, ok := <-sub.Ch
	if ok {
		t.Fatal("channel should be closed after Close")
	}
}
