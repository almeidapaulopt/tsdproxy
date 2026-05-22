// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package tailscale

import (
	"net"
	"sync"
	"sync/atomic"
)

// VirtualListener implements net.Listener backed by a channel.
// The SNI router dispatches connections to it via Dispatch().
type VirtualListener struct {
	ch     chan net.Conn
	done   chan struct{}
	closed atomic.Bool
	once   sync.Once
	addr   net.Addr
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
		vl.closed.Store(true)
		close(vl.done)
	})
	return nil
}

func (vl *VirtualListener) Addr() net.Addr {
	return vl.addr
}

// Dispatch sends a connection to this virtual listener.
// Called by the SNI router. Non-blocking; drops if listener is closed.
func (vl *VirtualListener) Dispatch(conn net.Conn) bool {
	if vl.closed.Load() {
		conn.Close()
		return false
	}

	select {
	case vl.ch <- conn:
		return true
	case <-vl.done:
		conn.Close()
		return false
	default:
		conn.Close()
		return false
	}
}
