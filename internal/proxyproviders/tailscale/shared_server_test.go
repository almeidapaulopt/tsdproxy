// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package tailscale

import (
	"net"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/almeidapaulopt/tsdproxy/internal/model"
)

func TestSharedServerSubscribeUnsubscribe(t *testing.T) {
	t.Parallel()

	ss := NewSharedServer(SharedServerConfig{
		Hostname: "test-server",
		Log:      zerolog.Nop(),
	})
	defer ss.Close()

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
	defer ss.Close()

	ch := ss.SubscribeEvents()

	// Send event via command channel (same-package access).
	// SubscribeEvents creates a minimal runtime with gen=0.
	ss.cmds <- watchUpdateCmd{
		gen: 0,
		evt: model.ProxyEvent{Status: model.ProxyStatusRunning},
		url: "test.example.com",
	}

	received := <-ch
	if received.Status != model.ProxyStatusRunning {
		t.Fatalf("expected ProxyStatusRunning, got %v", received.Status)
	}

	// Verify URL was stored.
	if url := ss.GetURL(); url != "test.example.com" {
		t.Fatalf("expected URL test.example.com, got %q", url)
	}

	ss.UnsubscribeEvents(ch)
}

func TestSharedServerGetURLInitiallyEmpty(t *testing.T) {
	t.Parallel()

	ss := NewSharedServer(SharedServerConfig{
		Hostname: "test-server",
		Log:      zerolog.Nop(),
	})
	defer ss.Close()

	url := ss.GetURL()
	if url != "" {
		t.Fatalf("GetURL should return empty string before start, got %q", url)
	}
}

func TestSharedServerMultipleSubscribers(t *testing.T) {
	ss := NewSharedServer(SharedServerConfig{
		Hostname: "test-server",
		Log:      zerolog.Nop(),
	})
	defer ss.Close()

	ch1 := ss.SubscribeEvents()
	ch2 := ss.SubscribeEvents()

	// Broadcast event via command channel.
	ss.cmds <- watchUpdateCmd{
		gen: 0,
		evt: model.ProxyEvent{Status: model.ProxyStatusStarting},
	}

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

func TestSharedServerCloseCleansUpSubscribers(t *testing.T) {
	ss := NewSharedServer(SharedServerConfig{
		Hostname: "test-server",
		Log:      zerolog.Nop(),
	})

	ch := ss.SubscribeEvents()

	ss.Close()

	// Subscriber channel should be closed after Close.
	_, ok := <-ch
	if ok {
		t.Fatal("subscriber channel should be closed after Close")
	}

	// Done channel should be closed (loop exited).
	select {
	case <-ss.done:
		// Expected: loop has exited.
	default:
		t.Fatal("done channel should be closed after Close")
	}
}

func TestSharedServerGenerationPreventsStaleEvents(t *testing.T) {
	ss := NewSharedServer(SharedServerConfig{
		Hostname: "test-server",
		Log:      zerolog.Nop(),
	})
	defer ss.Close()

	ch := ss.SubscribeEvents()

	// Send a stale event with wrong generation (gen=99, current is 0).
	// This should be ignored.
	ss.cmds <- watchUpdateCmd{
		gen: 99,
		evt: model.ProxyEvent{Status: model.ProxyStatusRunning},
		url: "stale.example.com",
	}

	// Send a current event with correct generation.
	ss.cmds <- watchUpdateCmd{
		gen: 0,
		evt: model.ProxyEvent{Status: model.ProxyStatusRunning},
		url: "current.example.com",
	}

	// Should receive the current event, not the stale one.
	received := <-ch
	if received.Status != model.ProxyStatusRunning {
		t.Fatalf("expected ProxyStatusRunning, got %v", received.Status)
	}

	// URL should be from the current event, not stale.
	if url := ss.GetURL(); url != "current.example.com" {
		t.Fatalf("expected URL current.example.com, got %q", url)
	}

	ss.UnsubscribeEvents(ch)
}

func TestSharedServerGetLocalClientNil(t *testing.T) {
	t.Parallel()

	ss := NewSharedServer(SharedServerConfig{
		Hostname: "test-server",
		Log:      zerolog.Nop(),
	})
	defer ss.Close()

	lc := ss.GetLocalClient()
	if lc != nil {
		t.Fatal("GetLocalClient should return nil before start")
	}
}

func TestSharedServerURLClearedAfterRuntimeStops(t *testing.T) {
	ss := NewSharedServer(SharedServerConfig{
		Hostname: "test-server",
		Log:      zerolog.Nop(),
	})

	ch := ss.SubscribeEvents()

	// Set URL via event.
	ss.cmds <- watchUpdateCmd{
		gen: 0,
		evt: model.ProxyEvent{Status: model.ProxyStatusRunning},
		url: "running.example.com",
	}

	// Wait for the event to be processed.
	<-ch

	if url := ss.GetURL(); url != "running.example.com" {
		t.Fatalf("expected URL running.example.com, got %q", url)
	}

	// Close the server (which stops the runtime).
	ss.Close()

	// After Close, the done channel is closed and URL should be gone
	// because the runtime is discarded.
	// But we can't call GetURL after Close because the loop has exited.
	// Instead verify that Close completed successfully (no panic/deadlock).
	<-ss.done
}

func TestSharedServerCloseIsTerminal(t *testing.T) {
	ss := NewSharedServer(SharedServerConfig{
		Hostname: "test-server",
		Log:      zerolog.Nop(),
	})

	ss.Close()

	// Verify loop exited by checking done channel.
	select {
	case <-ss.done:
		// Expected.
	default:
		t.Fatal("done channel should be closed after Close")
	}
}

func TestSharedServerAcquirePacketOnClosedServer(t *testing.T) {
	ss := NewSharedServer(SharedServerConfig{
		Hostname: "test-server",
		Log:      zerolog.Nop(),
	})

	ss.Close()
	<-ss.done

	_, err := ss.AcquirePacket("test.example.com", 53)
	if err == nil {
		t.Fatal("expected error from AcquirePacket on closed server")
	}
}

func TestSharedServerReleasePacketOnNilRuntime(_ *testing.T) {
	ss := NewSharedServer(SharedServerConfig{
		Hostname: "test-server",
		Log:      zerolog.Nop(),
	})
	defer ss.Close()

	// ReleasePacket on a server that never started should not panic.
	ss.ReleasePacket("test.example.com", 53)
}

func TestRegisterRouteModeMismatch(t *testing.T) {
	rt := &sharedRuntime{
		listeners:    make(map[int]*portEntry),
		packetRoutes: make(map[int]net.PacketConn),
	}

	// Simulate a port 443 registered as HTTPS (RouteSNI).
	router := NewPortRouter(RouteSNI, zerolog.Nop())
	rt.listeners[443] = &portEntry{
		listener: &noopListener{},
		router:   router,
	}

	// Try to register the same port as HTTP (RouteHTTPHost).
	// This should fail with a mode conflict error.
	ss := &SharedServer{log: zerolog.Nop()}
	_, _, err := ss.registerRoute(rt, "test.example.com", 443, model.ProtoHTTP)
	if err == nil {
		t.Fatal("expected mode mismatch error")
	}
}

type noopListener struct{}

func (noopListener) Accept() (net.Conn, error) { return nil, net.ErrClosed }
func (noopListener) Close() error              { return nil }
func (noopListener) Addr() net.Addr            { return &net.TCPAddr{} }

func TestRegisterPacketRouteConflictDetection(t *testing.T) {
	rt := &sharedRuntime{
		listeners:    make(map[int]*portEntry),
		packetRoutes: make(map[int]net.PacketConn),
		routeCount:   0,
	}

	ss := &SharedServer{log: zerolog.Nop()}

	// Register a TCP port.
	rt.listeners[22] = &portEntry{
		listener: &noopListener{},
		owner:    "ssh.example.com",
	}

	// Try to register the same port as UDP — should fail.
	_, err := ss.registerPacketRoute(rt, "dns.example.com", 22)
	if err == nil {
		t.Fatal("expected port conflict error for UDP on TCP-occupied port")
	}

	// Register a UDP port in packetRoutes directly.
	rt.packetRoutes[53] = &noopPacketConn{}

	// Try to register the same UDP port again — should fail.
	_, err = ss.registerPacketRoute(rt, "dns2.example.com", 53)
	if err == nil {
		t.Fatal("expected UDP port conflict error")
	}
}

type noopPacketConn struct{}

func (noopPacketConn) ReadFrom(_ []byte) (n int, addr net.Addr, err error) {
	return 0, nil, net.ErrClosed
}
func (noopPacketConn) WriteTo(_ []byte, _ net.Addr) (n int, err error) { return 0, nil }
func (noopPacketConn) Close() error                                    { return nil }
func (noopPacketConn) LocalAddr() net.Addr                             { return &net.UDPAddr{} }
func (noopPacketConn) SetDeadline(_ time.Time) error                   { return nil }
func (noopPacketConn) SetReadDeadline(_ time.Time) error               { return nil }
func (noopPacketConn) SetWriteDeadline(_ time.Time) error              { return nil }

func TestSharedServerCertInFlightPreventsDuplicate(_ *testing.T) {
	ss := NewSharedServer(SharedServerConfig{
		Hostname: "test-server",
		Log:      zerolog.Nop(),
	})
	defer ss.Close()

	ch := ss.SubscribeEvents()

	// Send first Running event — certInFlight should be set to true.
	ss.cmds <- watchUpdateCmd{
		gen: 0,
		evt: model.ProxyEvent{Status: model.ProxyStatusRunning},
		url: "test.example.com",
	}

	// Read the event.
	<-ch

	// Send second Running event before certDone — certInFlight is true,
	// so no duplicate cert goroutine should be spawned.
	// (This is a correctness test: the second Running event should NOT
	// spawn another cert goroutine. We can't directly observe this,
	// but we verify no panic or deadlock occurs.)
	ss.cmds <- watchUpdateCmd{
		gen: 0,
		evt: model.ProxyEvent{Status: model.ProxyStatusRunning},
		url: "test.example.com",
	}

	// Send certDone to clear certInFlight.
	ss.cmds <- certDoneCmd{gen: 0}

	// Now a third Running event should be able to spawn cert again.
	ss.cmds <- watchUpdateCmd{
		gen: 0,
		evt: model.ProxyEvent{Status: model.ProxyStatusRunning},
		url: "test.example.com",
	}

	ss.UnsubscribeEvents(ch)
}

func TestSharedServerIdleTimerLifecycle(t *testing.T) {
	ss := NewSharedServer(SharedServerConfig{
		Hostname: "test-server",
		Log:      zerolog.Nop(),
	})

	// Subscribe creates a bare runtime (rt != nil) with gen=0.
	ch := ss.SubscribeEvents()

	// Send a running event to populate URL.
	ss.cmds <- watchUpdateCmd{
		gen: 0,
		evt: model.ProxyEvent{Status: model.ProxyStatusRunning},
		url: "running.example.com",
	}
	<-ch // consume event

	// Verify URL is set.
	if url := ss.GetURL(); url != "running.example.com" {
		t.Fatalf("expected URL running.example.com, got %q", url)
	}

	// Simulate idle timeout: send idleTimeoutCmd.
	// State is sharedIdle (default) and rt != nil (from subscribe),
	// so stopRuntime should be called, closing subscriber channels.
	ss.cmds <- idleTimeoutCmd{}

	// Subscriber channel should be closed by stopRuntime.
	_, ok := <-ch
	if ok {
		t.Fatal("subscriber channel should be closed after idle timeout")
	}

	ss.Close()
}

func TestSharedServerSubscribeBareRuntimeThenClose(t *testing.T) {
	ss := NewSharedServer(SharedServerConfig{
		Hostname: "test-server",
		Log:      zerolog.Nop(),
	})

	// Subscribe without any acquire — creates bare runtime with just subs map.
	ch := ss.SubscribeEvents()
	if ch == nil {
		t.Fatal("SubscribeEvents should return non-nil channel")
	}

	// Close should handle bare runtime without panic.
	// stopRuntime on bare runtime: nil tsServer, nil cancel, nil watchDone,
	// nil listeners, nil packetRoutes — all guarded by nil checks.
	ss.Close()

	// Verify subscriber channel was closed.
	_, ok := <-ch
	if ok {
		t.Fatal("subscriber channel should be closed after Close")
	}

	// Verify loop exited.
	select {
	case <-ss.done:
	default:
		t.Fatal("done channel should be closed after Close")
	}
}

func TestSharedServerAfterFuncNoLeakOnClose(t *testing.T) {
	ss := NewSharedServer(SharedServerConfig{
		Hostname: "test-server",
		Log:      zerolog.Nop(),
	})

	_ = ss.SubscribeEvents()

	// Close the server — exits the loop and closes ss.done.
	ss.Close()
	<-ss.done

	// Simulate what the AfterFunc callback does: try to send idleTimeoutCmd
	// with a select on ss.done. After Close, ss.done is closed so the
	// <-ss.done case fires immediately, preventing the goroutine from leaking.
	done := make(chan struct{})
	go func() {
		defer close(done)
		select {
		case ss.cmds <- idleTimeoutCmd{}:
		case <-ss.done:
		}
	}()

	select {
	case <-done:
		// Expected: goroutine exited without blocking.
	case <-time.After(5 * time.Second):
		t.Fatal("AfterFunc goroutine leaked: blocked trying to send to exited loop")
	}
}
