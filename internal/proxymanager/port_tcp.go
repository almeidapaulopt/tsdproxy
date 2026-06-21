// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package proxymanager

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/almeidapaulopt/tsdproxy/internal/core/metrics"
	"github.com/almeidapaulopt/tsdproxy/internal/model"

	"github.com/rs/zerolog"
)

const (
	tcpDialTimeout      = 10 * time.Second
	tcpErrChanBuf       = 2
	maxTCPAcceptRetries = 5
	defaultMaxTCPConns  = 1024
	// tcpIdleTimeout is the maximum idle time allowed on a proxied TCP
	// connection before it is forcibly closed. Prevents slowloris-style
	// resource exhaustion where a client opens many connections and sends
	// minimal data to hold semaphore slots indefinitely.
	tcpIdleTimeout = 5 * time.Minute
)

// tcpPort forwards raw TCP connections from the Tailscale listener to the target backend.
type tcpPort struct {
	log         zerolog.Logger
	listener    net.Listener
	ctx         context.Context
	metrics     *metrics.Metrics
	cancel      context.CancelFunc
	sem         chan struct{}
	portName    string
	proxyName   string
	pconfig     model.PortConfig
	wg          sync.WaitGroup
	acceptWg    sync.WaitGroup
	activeConns atomic.Int64
	mtx         sync.Mutex
	started     atomic.Bool
}

// idleTimeoutConn wraps a net.Conn, resetting read/write deadlines before
// each operation so idle connections are forcibly closed after the timeout.
type idleTimeoutConn struct {
	net.Conn
	timeout time.Duration
}

func (c *idleTimeoutConn) Read(b []byte) (int, error) {
	_ = c.SetReadDeadline(time.Now().Add(c.timeout))
	return c.Conn.Read(b)
}

func (c *idleTimeoutConn) Write(b []byte) (int, error) {
	_ = c.SetWriteDeadline(time.Now().Add(c.timeout))
	return c.Conn.Write(b)
}

func wrapIdleTimeout(c net.Conn, timeout time.Duration) net.Conn {
	return &idleTimeoutConn{Conn: c, timeout: timeout}
}

func newPortTCP(ctx context.Context, pconfig model.PortConfig, log zerolog.Logger, metrics *metrics.Metrics, proxyName, portName string) *tcpPort {
	return newPortTCPWithLimit(ctx, pconfig, log, metrics, proxyName, portName, defaultMaxTCPConns)
}

// newPortTCPWithLimit is the testable constructor: callers can pass a small
// maxConns to verify the semaphore behavior without opening 1000+ connections.
func newPortTCPWithLimit(
	ctx context.Context,
	pconfig model.PortConfig,
	log zerolog.Logger,
	metrics *metrics.Metrics,
	proxyName, portName string,
	maxConns int,
) *tcpPort {
	if maxConns < 1 {
		maxConns = defaultMaxTCPConns
	}
	ctxPort, cancel := context.WithCancel(ctx)

	tp := &tcpPort{
		log:       log.With().Str("port", pconfig.String()).Logger(),
		ctx:       ctxPort,
		cancel:    cancel,
		pconfig:   pconfig,
		metrics:   metrics,
		proxyName: proxyName,
		portName:  portName,
		sem:       make(chan struct{}, maxConns),
	}

	return tp
}

func (p *tcpPort) startWithListener(l net.Listener) error {
	if err := portStartLock(p.ctx, &p.mtx, &p.started); err != nil {
		l.Close()
		return fmt.Errorf("tcp port closed before start: %w", err)
	}
	p.listener = l
	p.acceptWg.Add(1)
	p.started.Store(true)
	p.mtx.Unlock()
	defer p.acceptWg.Done()
	defer p.wg.Wait()

	go func() {
		<-p.ctx.Done()
		l.Close()
	}()

	for retries := 0; ; retries++ {
		conn, err := l.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			if retries < maxTCPAcceptRetries {
				p.log.Warn().Err(err).Int("retry", retries+1).Msg("transient TCP accept error, retrying")
				select {
				case <-time.After(time.Duration(retries+1) * time.Second):
				case <-p.ctx.Done():
					return nil
				}
				continue
			}
			p.log.Error().Err(err).Msg("error accepting TCP connection after retries")
			l.Close()
			return fmt.Errorf("tcp accept: %w", err)
		}
		retries = 0

		// Bound concurrent connections so a single tailnet member cannot
		// exhaust goroutines by opening unlimited idle TCP sessions.
		// Non-blocking acquire: if the semaphore is full, reject immediately.
		select {
		case p.sem <- struct{}{}:
		default:
			p.log.Warn().Int("active", int(p.activeConns.Load())).Msg("TCP connection limit reached, rejecting")
			conn.Close()
			continue
		}

		p.activeConns.Add(1)
		if p.metrics != nil {
			p.metrics.SetConnectionsActive(p.proxyName, p.portName, int(p.activeConns.Load()))
		}

		p.wg.Add(1)
		go func(c net.Conn) {
			defer func() {
				<-p.sem
				p.activeConns.Add(-1)
				if p.metrics != nil {
					p.metrics.SetConnectionsActive(p.proxyName, p.portName, int(p.activeConns.Load()))
				}
				p.wg.Done()
			}()
			p.handleConn(c)
		}(conn)
	}
}

func (p *tcpPort) handleConn(clientConn net.Conn) {
	var backendConn net.Conn
	var clientCloseOnce, backendCloseOnce sync.Once

	closeClient := func() { clientCloseOnce.Do(func() { clientConn.Close() }) }
	closeBackend := func() { backendCloseOnce.Do(func() { backendConn.Close() }) }

	defer closeClient()

	target := p.pconfig.GetFirstTarget()
	if target == nil || target.Host == "" {
		p.log.Error().Msg("no target configured for TCP port")
		return
	}

	dialer := net.Dialer{Timeout: tcpDialTimeout}
	var err error
	backendConn, err = dialer.DialContext(p.ctx, "tcp", target.Host)
	if err != nil {
		p.log.Error().Err(err).Str("target", target.Host).Msg("error dialing backend")
		return
	}
	defer closeBackend()

	clientConn = wrapIdleTimeout(clientConn, tcpIdleTimeout)
	backendConn = wrapIdleTimeout(backendConn, tcpIdleTimeout)

	stop := make(chan struct{})
	defer close(stop)
	go func() {
		select {
		case <-p.ctx.Done():
			closeClient()
			closeBackend()
		case <-stop:
		}
	}()

	errChan := make(chan error, tcpErrChanBuf)
	go func() {
		_, err := io.Copy(backendConn, clientConn)
		errChan <- err
	}()
	go func() {
		_, err := io.Copy(clientConn, backendConn)
		errChan <- err
	}()

	firstErr := <-errChan
	closeClient()
	closeBackend()
	secondErr := <-errChan

	for _, copyErr := range []error{firstErr, secondErr} {
		if copyErr != nil && !errors.Is(copyErr, net.ErrClosed) && !errors.Is(copyErr, context.Canceled) {
			p.log.Debug().Err(copyErr).Msg("tcp connection closed with error")
			break
		}
	}
}

func (p *tcpPort) close() error {
	p.mtx.Lock()
	var errs error
	if p.listener != nil {
		errs = errors.Join(errs, p.listener.Close())
	}
	p.mtx.Unlock()

	p.cancel()

	if p.started.Load() {
		p.acceptWg.Wait()
	}

	return errs
}
