// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package tailscale

import (
	"testing"

	"github.com/almeidapaulopt/tsdproxy/internal/model"
	"github.com/rs/zerolog"
)

func TestSharedServerSubscribeUnsubscribe(t *testing.T) {
	t.Parallel()

	ss := NewSharedServer(SharedServerConfig{
		Hostname: "test-server",
		Log:      zerolog.Nop(),
	})

	ch := ss.SubscribeEvents()
	if ch == nil {
		t.Fatal("SubscribeEvents should return a non-nil channel")
	}

	ss.UnsubscribeEvents(ch)

	_, ok := <-ch
	if ok {
		t.Fatal("channel should be closed after UnsubscribeEvents")
	}
}

func TestSharedServerEventBroadcasting(t *testing.T) {
	ss := NewSharedServer(SharedServerConfig{
		Hostname: "test-server",
		Log:      zerolog.Nop(),
	})

	ch := ss.SubscribeEvents()

	evt := model.ProxyEvent{Status: model.ProxyStatusRunning}
	ss.sendEvent(evt)

	received := <-ch
	if received.Status != model.ProxyStatusRunning {
		t.Fatalf("expected ProxyStatusRunning, got %v", received.Status)
	}

	ss.UnsubscribeEvents(ch)
}

func TestSharedServerGetURLInitiallyEmpty(t *testing.T) {
	t.Parallel()

	ss := NewSharedServer(SharedServerConfig{
		Hostname: "test-server",
		Log:      zerolog.Nop(),
	})

	url := ss.GetURL()
	if url != "" {
		t.Fatalf("GetURL should return empty string before start, got %q", url)
	}
}

func TestSharedServerRefCountingViaShutdown(t *testing.T) {
	ss := NewSharedServer(SharedServerConfig{
		Hostname: "test-server",
		Log:      zerolog.Nop(),
	})

	ss.mu.Lock()
	ss.refCount = 2
	ss.mu.Unlock()

	ss.mu.Lock()
	ss.refCount--
	ss.mu.Unlock()

	if ss.GetURL() != "" {
		t.Fatal("URL should still be empty")
	}

	ss.mu.Lock()
	ss.refCount--
	if ss.refCount <= 0 {
		ss.shutdown()
	}
	ss.mu.Unlock()

	if ss.started {
		t.Fatal("server should not be started after shutdown")
	}

	url := ss.GetURL()
	if url != "" {
		t.Fatalf("GetURL should return empty after shutdown, got %q", url)
	}
}

func TestSharedServerMultipleSubscribers(t *testing.T) {
	ss := NewSharedServer(SharedServerConfig{
		Hostname: "test-server",
		Log:      zerolog.Nop(),
	})

	ch1 := ss.SubscribeEvents()
	ch2 := ss.SubscribeEvents()

	evt := model.ProxyEvent{Status: model.ProxyStatusStarting}
	ss.sendEvent(evt)

	r1 := <-ch1
	r2 := <-ch2

	if r1.Status != model.ProxyStatusStarting {
		t.Fatalf("subscriber 1 expected ProxyStatusStarting, got %v", r1.Status)
	}
	if r2.Status != model.ProxyStatusStarting {
		t.Fatalf("subscriber 2 expected ProxyStatusStarting, got %v", r2.Status)
	}

	ss.UnsubscribeEvents(ch1)
	ss.UnsubscribeEvents(ch2)
}

func TestSharedServerGetLocalClientNil(t *testing.T) {
	t.Parallel()

	ss := NewSharedServer(SharedServerConfig{
		Hostname: "test-server",
		Log:      zerolog.Nop(),
	})

	lc := ss.GetLocalClient()
	if lc != nil {
		t.Fatal("GetLocalClient should return nil before start")
	}
}
