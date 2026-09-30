// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package tailscale

import (
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/almeidapaulopt/tsdproxy/internal/model"
)

func TestServicesServerCloseIsTerminal(t *testing.T) {
	ss := NewServicesServer(ServicesServerConfig{
		Hostname: "test-server",
		Log:      zerolog.Nop(),
	})

	ss.Close()

	select {
	case <-ss.ev.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("done channel should be closed after Close")
	}
}

func TestServicesServerGetAuthURLInitiallyEmpty(t *testing.T) {
	t.Parallel()

	ss := NewServicesServer(ServicesServerConfig{
		Hostname: "test-server",
		Log:      zerolog.Nop(),
	})
	defer ss.Close()

	url := ss.GetAuthURL()
	if url != "" {
		t.Fatalf("GetAuthURL should return empty string before any auth event, got %q", url)
	}
}

func TestServicesServerGetAuthURLFromWatchUpdate(t *testing.T) {
	ss := NewServicesServer(ServicesServerConfig{
		Hostname: "test-server",
		Log:      zerolog.Nop(),
	})
	defer ss.Close()

	ss.ev.SendCmd(servicesWatchUpdateCmd{authURL: "https://login.tailscale.com/a/svcauth"})

	if url := ss.GetAuthURL(); url != "https://login.tailscale.com/a/svcauth" {
		t.Fatalf("GetAuthURL should return auth URL from watchUpdate, got %q", url)
	}
}

func TestServicesServerWhoisReturnsEmpty(t *testing.T) {
	t.Parallel()

	ss := NewServicesServer(ServicesServerConfig{
		Hostname: "test-server",
		Log:      zerolog.Nop(),
	})
	defer ss.Close()

	whois := ss.Whois(nil)
	if whois != (model.Whois{}) {
		t.Fatalf("expected empty Whois, got %+v", whois)
	}
}

func TestServicesServerAcquireOnClosedServer(t *testing.T) {
	ss := NewServicesServer(ServicesServerConfig{
		Hostname: "test-server",
		Log:      zerolog.Nop(),
	})

	ss.Close()
	<-ss.ev.Done()

	_, err := ss.Acquire("svc:test", 443, true, false)
	if err == nil {
		t.Fatal("expected error from Acquire on closed server")
	}
}

func TestServicesServerReleaseOnClosedServer(t *testing.T) {
	ss := NewServicesServer(ServicesServerConfig{
		Hostname: "test-server",
		Log:      zerolog.Nop(),
	})

	ss.Close()
	<-ss.ev.Done()

	err := ss.Release("svc:test", 443)
	if err == nil {
		t.Fatal("expected error from Release on closed server")
	}
}

func TestServicesServerCloseIdempotent(t *testing.T) {
	ss := NewServicesServer(ServicesServerConfig{
		Hostname: "test-server",
		Log:      zerolog.Nop(),
	})

	// Close twice — must not panic.
	ss.Close()
	ss.Close()

	// Verify loop exited.
	select {
	case <-ss.ev.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("done channel should be closed after Close")
	}
}

func TestServicesServerAfterFuncNoLeakOnClose(t *testing.T) {
	ss := NewServicesServer(ServicesServerConfig{
		Hostname: "test-server",
		Log:      zerolog.Nop(),
	})

	// Close the server — exits the loop and closes ev.done.
	ss.Close()
	<-ss.ev.Done()

	// Simulate what the AfterFunc callback does: try to send idleTimeoutCmd
	// with a select on ev.done. After Close, ev.done is closed so the
	// <-ss.ev.Done() case fires immediately, preventing the goroutine from leaking.
	done := make(chan struct{})
	go func() {
		defer close(done)
		select {
		case ss.ev.cmds <- servicesIdleTimeoutCmd{}:
		case <-ss.ev.Done():
		}
	}()

	select {
	case <-done:
		// Expected: goroutine exited without blocking.
	case <-time.After(5 * time.Second):
		t.Fatal("AfterFunc goroutine leaked: blocked trying to send to exited loop")
	}
}

func TestServicesServerCloseCleansUp(t *testing.T) {
	ss := NewServicesServer(ServicesServerConfig{
		Hostname: "test-server",
		Log:      zerolog.Nop(),
	})

	ss.Close()

	// Verify done channel is closed.
	select {
	case <-ss.ev.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("done channel should be closed after Close")
	}

	// Verify closed flag is set.
	if !ss.ev.IsClosed() {
		t.Fatal("closed flag should be true after Close")
	}
}

func TestServiceKey(t *testing.T) {
	t.Parallel()

	got := serviceKey("svc:test", 443)
	want := "svc:test:443"
	if got != want {
		t.Fatalf("serviceKey(%q, %d) = %q, want %q", "svc:test", 443, got, want)
	}
}
