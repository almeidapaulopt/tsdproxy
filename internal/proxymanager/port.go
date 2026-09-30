// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package proxymanager

import (
	"context"
	"net"
	"sync"
	"sync/atomic"
)

// portStartLock acquires mtx and checks ctx.Err(). If ctx is already canceled,
// it marks started=true, releases mtx, and returns the error — the caller must
// close the listener/packetConn and return. On nil return, mtx is still held
// and the caller must assign fields, call wg.Add(1), store started=true,
// then unlock mtx.
//
// Shared by tcpPort.startWithListener and udpPort.startWithPacketConn to
// guarantee identical ordering: ctx check → field assignment → wg.Add →
// started.Store, all atomically under mtx. Prevents the start-vs-close race
// where close() observes a half-initialized state between the ctx check and
// the mtx.Lock.
func portStartLock(ctx context.Context, mtx *sync.Mutex, started *atomic.Bool) error {
	mtx.Lock()
	if err := ctx.Err(); err != nil {
		started.Store(true)
		mtx.Unlock()
		return err
	}
	return nil
}

// portHandler is the interface implemented by all port types (HTTP proxy, HTTP redirect, TCP forward).
type portHandler interface {
	startWithListener(net.Listener) error
	close() error
}
