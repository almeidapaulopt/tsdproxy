// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package tailscale

import (
	"net"
	"testing"

	"github.com/rs/zerolog"
)

func TestDroppedConnections_InitiallyZero(t *testing.T) {
	t.Parallel()

	vl := NewVirtualListener(&net.TCPAddr{IP: net.ParseIP("0.0.0.0"), Port: 0}, zerolog.Nop())
	if n := vl.DroppedConnections(); n != 0 {
		t.Fatalf("expected 0 dropped connections, got %d", n)
	}
}

func TestDroppedConnections_AfterOverflow(t *testing.T) {
	t.Parallel()

	vl := NewVirtualListener(&net.TCPAddr{IP: net.ParseIP("0.0.0.0"), Port: 0}, zerolog.Nop())

	// Fill the channel buffer (capacity 64).
	for range 64 {
		server, client := net.Pipe()
		client.Close()
		vl.Dispatch(server)
	}

	// 65th dispatch should be dropped.
	overflowServer, overflowClient := net.Pipe()
	overflowClient.Close()
	if vl.Dispatch(overflowServer) {
		t.Fatal("expected Dispatch to return false when buffer is full")
	}

	if n := vl.DroppedConnections(); n != 1 {
		t.Fatalf("expected 1 dropped connection, got %d", n)
	}
}

func TestDroppedConnections_MultipleOverflows(t *testing.T) {
	t.Parallel()

	vl := NewVirtualListener(&net.TCPAddr{IP: net.ParseIP("0.0.0.0"), Port: 0}, zerolog.Nop())

	// Fill the channel buffer (capacity 64).
	conns := make([]net.Conn, 0, 67)
	for range 64 {
		server, client := net.Pipe()
		client.Close()
		vl.Dispatch(server)
		conns = append(conns, server)
	}
	// Don't drain, keep them to keep buffer full

	// Dispatch 3 more — all dropped.
	for range 3 {
		server, client := net.Pipe()
		client.Close()
		vl.Dispatch(server)
		conns = append(conns, server)
	}

	if n := vl.DroppedConnections(); n != 3 {
		t.Fatalf("expected 3 dropped connections, got %d", n)
	}

	// Cleanup: close all buffered connections.
	for _, c := range conns {
		c.Close()
	}
	vl.Close()
}

func TestDroppedConnections_AfterClose(t *testing.T) {
	t.Parallel()

	vl := NewVirtualListener(&net.TCPAddr{IP: net.ParseIP("0.0.0.0"), Port: 0}, zerolog.Nop())
	vl.Close()

	server, client := net.Pipe()
	client.Close()
	if vl.Dispatch(server) {
		t.Fatal("Dispatch after Close should return false")
	}

	// Drops after Close don't increment the counter (Dispatch returns false
	// immediately due to closed flag, doesn't hit the default/overflow path).
	if n := vl.DroppedConnections(); n != 0 {
		t.Fatalf("expected 0 dropped connections after Close, got %d", n)
	}
}
