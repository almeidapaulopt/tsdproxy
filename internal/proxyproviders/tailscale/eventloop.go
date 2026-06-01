// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package tailscale

import (
	"context"
	"sync/atomic"
	"time"
)

// EventLoop provides the channel infrastructure for a command-based event loop.
// It is parameterized by command type to provide type-safe send operations.
//
// Only the channel plumbing is extracted — state machines, dispatch, and teardown
// remain in each server implementation.
type EventLoop[Cmd any] struct {
	cmds   chan Cmd
	done   chan struct{}
	closed atomic.Bool
}

// NewEventLoop creates an EventLoop with the given channel buffer size.
func NewEventLoop[Cmd any](bufferSize int) *EventLoop[Cmd] {
	return &EventLoop[Cmd]{
		cmds: make(chan Cmd, bufferSize),
		done: make(chan struct{}),
	}
}

// Cmds returns the command channel for use in the main loop's range.
func (el *EventLoop[Cmd]) Cmds() <-chan Cmd { return el.cmds }

// Done returns the done channel for goroutine coordination.
func (el *EventLoop[Cmd]) Done() <-chan struct{} { return el.done }

// SendCmd sends a command directly on the cmds channel.
// Used by tests that need to inject commands without the public/producer guards.
func (el *EventLoop[Cmd]) SendCmd(cmd Cmd) { el.cmds <- cmd }

// SendProducer sends a command from a producer goroutine (bridge, cert provisioning).
// It aborts if the loop has exited or the producer's context was canceled,
// preventing deadlock when the loop is blocked in teardown.
func (el *EventLoop[Cmd]) SendProducer(ctx context.Context, cmd Cmd) bool {
	if ctx == nil {
		select {
		case el.cmds <- cmd:
			return true
		case <-el.done:
			return false
		}
	}
	select {
	case el.cmds <- cmd:
		return true
	case <-el.done:
		return false
	case <-ctx.Done():
		return false
	}
}

// SendPublic sends a command from a public method.
// Returns false if the loop is closed (loop has exited), preventing goroutine leaks.
func (el *EventLoop[Cmd]) SendPublic(cmd Cmd) bool {
	if el.closed.Load() {
		return false
	}
	select {
	case el.cmds <- cmd:
		return true
	case <-el.done:
		return false
	}
}

// Close marks the loop as closed and signals done.
func (el *EventLoop[Cmd]) Close() {
	el.closed.Store(true)
	close(el.done)
}

// IsClosed returns whether the loop has been closed.
func (el *EventLoop[Cmd]) IsClosed() bool {
	return el.closed.Load()
}

// ScheduleIdleTimer starts an idle-shutdown timer that sends a command after
// the given duration. The cmdFactory callback constructs the mode-specific
// idle timeout command with the captured generation counter.
func (el *EventLoop[Cmd]) ScheduleIdleTimer(
	gen int,
	timeout time.Duration,
	cmdFactory func(int) Cmd,
) *time.Timer {
	return time.AfterFunc(timeout, func() {
		select {
		case el.cmds <- cmdFactory(gen):
		case <-el.done:
		}
	})
}

// SendAndWait sends a command via the event loop and waits for a typed reply.
// Package-level function because Go does not support generic methods on generic types.
func SendAndWait[Cmd any, T any](el *EventLoop[Cmd], cmd Cmd, reply chan T) (T, bool) {
	if !el.SendPublic(cmd) {
		var zero T
		return zero, false
	}
	select {
	case v := <-reply:
		return v, true
	case <-el.Done():
		var zero T
		return zero, false
	}
}
