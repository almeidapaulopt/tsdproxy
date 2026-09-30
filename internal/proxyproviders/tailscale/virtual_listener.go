// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package tailscale

import (
	"net"
	"sync"
	"sync/atomic"

	"github.com/rs/zerolog"
)

const connBufferSize = 64

// VirtualListener implements net.Listener backed by a channel.
// The port router dispatches connections to it via Dispatch().
type VirtualListener struct {
	log          zerolog.Logger
	addr         net.Addr
	ch           chan net.Conn
	done         chan struct{}
	once         sync.Once
	mu           sync.Mutex
	closed       bool
	droppedCount atomic.Int64
}

func NewVirtualListener(addr net.Addr, log zerolog.Logger) *VirtualListener {
	return &VirtualListener{
		ch:   make(chan net.Conn, connBufferSize),
		done: make(chan struct{}),
		addr: addr,
		log:  log,
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

// DroppedConnections returns the total number of connections dropped
// because the listener buffer was full.
func (vl *VirtualListener) DroppedConnections() int64 {
	return vl.droppedCount.Load()
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
		vl.log.Warn().Msg("virtual listener buffer full, dropping connection")
		vl.droppedCount.Add(1)
		conn.Close()
		return false
	}
}
