// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package tailscale

import (
	"errors"
	"net"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestVirtualListenerAccept(t *testing.T) {
	t.Parallel()

	addr := &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 443}
	vl := NewVirtualListener(addr)

	server, client := net.Pipe()
	defer client.Close()

	if !vl.Dispatch(server) {
		t.Fatal("Dispatch should return true")
	}

	conn, err := vl.Accept()
	if err != nil {
		t.Fatalf("Accept returned error: %v", err)
	}
	defer conn.Close()

	if conn != server {
		t.Fatal("Accept should return the dispatched connection")
	}
}

func TestVirtualListenerClose(t *testing.T) {
	t.Parallel()

	vl := NewVirtualListener(&net.TCPAddr{IP: net.ParseIP("0.0.0.0"), Port: 0})

	if err := vl.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}

	_, err := vl.Accept()
	if !errors.Is(err, net.ErrClosed) {
		t.Fatalf("Accept after Close should return net.ErrClosed, got: %v", err)
	}
}

func TestVirtualListenerDispatchAfterClose(t *testing.T) {
	t.Parallel()

	vl := NewVirtualListener(&net.TCPAddr{IP: net.ParseIP("0.0.0.0"), Port: 0})

	_ = vl.Close()

	server, client := net.Pipe()
	defer client.Close()

	if vl.Dispatch(server) {
		t.Fatal("Dispatch after Close should return false")
	}
}

func TestVirtualListenerAddr(t *testing.T) {
	t.Parallel()

	addr := &net.TCPAddr{IP: net.ParseIP("192.168.1.1"), Port: 8443}
	vl := NewVirtualListener(addr)

	if vl.Addr() != addr {
		t.Fatalf("Addr should return the provided address, got %v", vl.Addr())
	}
}

func TestVirtualListenerDispatchDropWhenFull(t *testing.T) {
	t.Parallel()

	vl := NewVirtualListener(&net.TCPAddr{IP: net.ParseIP("0.0.0.0"), Port: 0})

	// Fill the channel buffer (capacity 64).
	var overflowConn *net.Conn
	for i := range 65 {
		server, client := net.Pipe()
		defer client.Close()

		dispatched := vl.Dispatch(server)
		if i < 64 {
			if !dispatched {
				t.Fatalf("Dispatch %d should succeed (buffer not full)", i)
			}
		} else {
			// 65th dispatch: channel is full, should drop.
			if dispatched {
				t.Fatal("Dispatch should return false when channel is full")
			}
			overflowConn = &server
		}
	}

	// The overflow connection should have been closed by Dispatch.
	if overflowConn != nil {
		buf := make([]byte, 1)
		_, err := (*overflowConn).Read(buf)
		if err == nil {
			t.Fatal("overflow connection should have been closed")
		}
	}
}

func TestVirtualListenerConcurrentDispatchClose(t *testing.T) {
	t.Parallel()

	const numSenders = 8
	const sendsPerSender = 100

	vl := NewVirtualListener(&net.TCPAddr{IP: net.ParseIP("0.0.0.0"), Port: 0})

	var wg sync.WaitGroup
	dispatched := atomic.Int32{}

	// Start sender goroutines that continuously dispatch connections.
	for i := range numSenders {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := range sendsPerSender {
				server, client := net.Pipe()
				// Close the client end immediately — we don't need it.
				client.Close()

				if vl.Dispatch(server) {
					dispatched.Add(1)
				}
				// Small yield to allow interleaving.
				if j%16 == 0 { //nolint:mnd
					runtime.Gosched()
				}
			}
		}(i)
	}

	// After a short delay, close the listener.
	time.Sleep(5 * time.Millisecond)
	vl.Close()

	// Wait for all senders to finish.
	wg.Wait()

	// Verify: no panics (test reaching here proves it), and the count
	// of dispatched connections is reasonable (some succeeded before Close,
	// some failed after).
	totalSends := int32(numSenders) * int32(sendsPerSender)
	d := dispatched.Load()
	if d > totalSends {
		t.Fatalf("dispatched count %d exceeds total sends %d", d, totalSends)
	}

	// Dispatch after Close should always fail.
	server, client := net.Pipe()
	defer client.Close()
	if vl.Dispatch(server) {
		t.Fatal("Dispatch after Close should return false")
	}

	// Accept should return error after Close.
	_, err := vl.Accept()
	if !errors.Is(err, net.ErrClosed) {
		t.Fatalf("Accept after Close should return net.ErrClosed, got: %v", err)
	}
}
