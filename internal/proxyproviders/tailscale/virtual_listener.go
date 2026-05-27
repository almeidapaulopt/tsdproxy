// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package tailscale

import (
	"net"
	"sync"
)

// VirtualListener implements net.Listener backed by a channel.
// The port router dispatches connections to it via Dispatch().
type VirtualListener struct {
	addr   net.Addr
	ch     chan net.Conn
	done   chan struct{}
	once   sync.Once
	closed bool
	mu     sync.Mutex // serializes Dispatch vs Close
}

func NewVirtualListener(addr net.Addr) *VirtualListener {
	return &VirtualListener{
		ch:   make(chan net.Conn, 64), //nolint:mnd
		done: make(chan struct{}),
		addr: addr,
	}
}

func (vl *VirtualListener) Accept() (net.Conn, error) {
	select {
	case conn, ok := <-vl.ch:
		if !ok {
			return nil, net.ErrClosed
		}
		return conn, nil
	case <-vl.done:
		return nil, net.ErrClosed
	}
}

func (vl *VirtualListener) Close() error {
	vl.once.Do(func() {
		vl.mu.Lock()
		vl.closed = true
		close(vl.ch)
		vl.mu.Unlock()

		close(vl.done)

		// Drain buffered connections to prevent resource leaks.
		for conn := range vl.ch {
			conn.Close()
		}
	})
	return nil
}

func (vl *VirtualListener) Addr() net.Addr {
	return vl.addr
}

// Dispatch sends a connection to this virtual listener.
// Called by the port router. Non-blocking; drops if listener is closed.
func (vl *VirtualListener) Dispatch(conn net.Conn) bool {
	vl.mu.Lock()
	if vl.closed {
		vl.mu.Unlock()
		conn.Close()
		return false
	}
	select {
	case vl.ch <- conn:
		vl.mu.Unlock()
		return true
	default:
		vl.mu.Unlock()
		conn.Close()
		return false
	}
}
