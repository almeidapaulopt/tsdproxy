// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package proxymanager

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"sync"
	"time"

	"github.com/almeidapaulopt/tsdproxy/internal/consts"
	"github.com/almeidapaulopt/tsdproxy/internal/core"
	"github.com/almeidapaulopt/tsdproxy/internal/model"

	"github.com/rs/zerolog"
)

// portHandler is the interface implemented by all port types (HTTP proxy, HTTP redirect, TCP forward).
type portHandler interface {
	startWithListener(net.Listener) error
	close() error
}

type port struct {
	log        zerolog.Logger
	ctx        context.Context
	listener   net.Listener
	cancel     context.CancelFunc
	httpServer *http.Server
	transport  *http.Transport
	mtx        sync.Mutex
}

func newPortProxy(
	ctx context.Context,
	pconfig model.PortConfig,
	log zerolog.Logger,
	accessLog bool,
	whoisFunc func(next http.Handler) http.Handler,
) *port {
	//
	log = log.With().Str("port", pconfig.String()).Logger()

	ctxPort, cancel := context.WithCancel(ctx)

	// Create the reverse proxy
	//
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: !pconfig.TLSValidate}, //nolint
	}
	reverseProxy := &httputil.ReverseProxy{
		Transport:     tr,
		FlushInterval: -1, // flush immediately — required for SSE streaming
		Rewrite: func(r *httputil.ProxyRequest) {
			r.SetURL(pconfig.GetFirstTarget())
			r.Out.Host = r.In.Host

			if user, ok := model.WhoisFromContext(r.In.Context()); ok {
				r.Out.Header.Set(consts.HeaderUsername, user.Username)
				r.Out.Header.Set(consts.HeaderDisplayName, user.DisplayName)
				r.Out.Header.Set(consts.HeaderProfilePicURL, user.ProfilePicURL)
			}

			r.SetXForwarded()
		},
	}

	handler := whoisFunc(reverseProxy)
	// add logger to proxy
	if accessLog {
		handler = core.LoggerMiddleware(log, handler)
	}

	// main http Server
	httpServer := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: core.ReadHeaderTimeout,
		BaseContext:       func(net.Listener) context.Context { return ctxPort },
	}

	return &port{
		log:        log,
		ctx:        ctxPort,
		cancel:     cancel,
		httpServer: httpServer,
		transport:  tr,
	}
}

func newPortRedirect(ctx context.Context, pconfig model.PortConfig, log zerolog.Logger) *port {
	log = log.With().Str("port", pconfig.String()).Logger()

	ctxPort, cancel := context.WithCancel(ctx)

	redirectHTTPServer := &http.Server{
		ReadHeaderTimeout: core.ReadHeaderTimeout,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, pconfig.GetFirstTarget().String(), http.StatusMovedPermanently)
		}),
	}

	return &port{
		log:        log,
		ctx:        ctxPort,
		cancel:     cancel,
		httpServer: redirectHTTPServer,
	}
}

func (p *port) startWithListener(l net.Listener) error {
	p.mtx.Lock()
	p.listener = l
	p.mtx.Unlock()

	err := p.httpServer.Serve(l)
	defer p.log.Info().Msg("Terminating server")

	if err != nil && !errors.Is(err, net.ErrClosed) && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("error starting port %w", err)
	}
	return nil
}

func (p *port) close() error {
	var errs error

	if p.httpServer != nil {
		errs = errors.Join(errs, p.httpServer.Shutdown(p.ctx))
	}

	if p.listener != nil {
		errs = errors.Join(errs, p.listener.Close())
	}

	if p.transport != nil {
		p.transport.CloseIdleConnections()
	}

	p.cancel()

	return errs
}

// tcpPort forwards raw TCP connections from the Tailscale listener to the target backend.
type tcpPort struct {
	log      zerolog.Logger
	ctx      context.Context
	listener net.Listener
	cancel   context.CancelFunc
	pconfig  model.PortConfig
	mtx      sync.Mutex
}

func newPortTCP(ctx context.Context, pconfig model.PortConfig, log zerolog.Logger) *tcpPort {
	ctxPort, cancel := context.WithCancel(ctx)

	return &tcpPort{
		log:     log.With().Str("port", pconfig.String()).Logger(),
		ctx:     ctxPort,
		cancel:  cancel,
		pconfig: pconfig,
	}
}

func (p *tcpPort) startWithListener(l net.Listener) error {
	p.mtx.Lock()
	p.listener = l
	p.mtx.Unlock()

	go func() {
		<-p.ctx.Done()
		l.Close()
	}()

	for {
		conn, err := l.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			p.log.Error().Err(err).Msg("error accepting TCP connection")
			return fmt.Errorf("tcp accept: %w", err)
		}

		go p.handleConn(conn)
	}
}

func (p *tcpPort) handleConn(clientConn net.Conn) {
	defer clientConn.Close()

	target := p.pconfig.GetFirstTarget()
	if target.Host == "" {
		p.log.Error().Msg("no target configured for TCP port")
		return
	}

	dialer := net.Dialer{Timeout: 10 * time.Second}
	backendConn, err := dialer.DialContext(p.ctx, "tcp", target.Host)
	if err != nil {
		p.log.Error().Err(err).Str("target", target.Host).Msg("error dialing backend")
		return
	}
	defer backendConn.Close()

	errChan := make(chan error, 2)
	go func() {
		_, err := io.Copy(backendConn, clientConn)
		errChan <- err
	}()
	go func() {
		_, err := io.Copy(clientConn, backendConn)
		errChan <- err
	}()

	<-errChan
	<-errChan
}

func (p *tcpPort) close() error {
	p.mtx.Lock()
	var errs error
	if p.listener != nil {
		errs = errors.Join(errs, p.listener.Close())
	}
	p.mtx.Unlock()

	p.cancel()

	return errs
}
