// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package proxymanager

import (
	"context"
	"net"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/almeidapaulopt/tsdproxy/internal/model"
	"github.com/almeidapaulopt/tsdproxy/internal/proxyproviders"
	"github.com/rs/zerolog"
)

// fakeProviderProxy is a minimal ProxyInterface for testing the restart logic.
type fakeProviderProxy struct {
	events chan model.ProxyEvent
}

var _ proxyproviders.ProxyInterface = (*fakeProviderProxy)(nil)

func (f *fakeProviderProxy) Start(context.Context) error              { return nil }
func (f *fakeProviderProxy) Close() error                             { return nil }
func (f *fakeProviderProxy) GetListener(string) (net.Listener, error) { return nil, nil }
func (f *fakeProviderProxy) GetURL() string                           { return "" }
func (f *fakeProviderProxy) GetAuthURL() string                       { return "" }
func (f *fakeProviderProxy) WatchEvents() chan model.ProxyEvent       { return f.events }
func (f *fakeProviderProxy) Whois(*http.Request) model.Whois          { return model.Whois{} }

func newTestProxy(restartable bool) (*Proxy, *fakeProviderProxy) {
	events := make(chan model.ProxyEvent, 1)
	fp := &fakeProviderProxy{events: events}
	ctx, cancel := context.WithCancel(context.Background())

	p := &Proxy{
		log:           zerolog.Nop(),
		Config:        &model.Config{Hostname: "test"},
		ctx:           ctx,
		cancel:        cancel,
		providerProxy: fp,
		ports:         make(map[string]portHandler),
		restartable:   restartable,
	}
	return p, fp
}

func TestRestart_TriggersOnUnexpectedClose(t *testing.T) {
	p, fp := newTestProxy(true)

	var restartCount atomic.Int32
	p.onRestart = func() {
		restartCount.Add(1)
	}

	// Simulate the provider proxy sending an error then closing the channel,
	// matching what watchStatus does on "invalid key" detection.
	fp.events <- model.ProxyEvent{Status: model.ProxyStatusError}
	close(fp.events)

	// Start reads from the events channel in a goroutine.
	p.Start()

	// Wait for the restart goroutine to fire.
	deadline := time.After(2 * time.Second)
	for {
		if restartCount.Load() > 0 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("onRestart was not called within timeout")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}

	if restartCount.Load() != 1 {
		t.Errorf("expected exactly 1 restart, got %d", restartCount.Load())
	}
}

func TestRestart_DoesNotTriggerWhenNotRestartable(t *testing.T) {
	p, fp := newTestProxy(false)

	var restartCalled atomic.Bool
	p.onRestart = func() {
		restartCalled.Store(true)
	}

	fp.events <- model.ProxyEvent{Status: model.ProxyStatusError}
	close(fp.events)

	p.Start()

	time.Sleep(200 * time.Millisecond)

	if restartCalled.Load() {
		t.Error("onRestart should not be called when restartable is false")
	}
}

func TestRestart_DoesNotTriggerOnNormalShutdown(t *testing.T) {
	p, fp := newTestProxy(true)

	var restartCalled atomic.Bool
	p.onRestart = func() {
		restartCalled.Store(true)
	}

	// Simulate normal shutdown: status is set to Stopping before channel closes.
	fp.events <- model.ProxyEvent{Status: model.ProxyStatusStopping}
	close(fp.events)

	p.Start()

	time.Sleep(200 * time.Millisecond)

	if restartCalled.Load() {
		t.Error("onRestart should not be called during normal shutdown")
	}
}
