// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package tailscale

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

type RoutingMode int

const (
	RouteSNI      RoutingMode = iota
	RouteHTTPHost
)

type PortRouter struct {
	log       zerolog.Logger
	mode      RoutingMode
	mu        sync.RWMutex
	listeners map[string]*VirtualListener
}

func NewPortRouter(mode RoutingMode, log zerolog.Logger) *PortRouter {
	return &PortRouter{
		mode:      mode,
		listeners: make(map[string]*VirtualListener),
		log:       log,
	}
}

func (r *PortRouter) Register(domain string) (*VirtualListener, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.listeners[domain]; exists {
		return nil, fmt.Errorf("duplicate registration for domain %q", domain)
	}

	vl := NewVirtualListener(&net.TCPAddr{IP: net.ParseIP("0.0.0.0"), Port: 0})
	r.listeners[domain] = vl
	return vl, nil
}

func (r *PortRouter) Unregister(domain string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	vl, ok := r.listeners[domain]
	if !ok {
		return false
	}
	vl.Close()
	delete(r.listeners, domain)
	return true
}

func (r *PortRouter) IsEmpty() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.listeners) == 0
}

func (r *PortRouter) CloseAll() {
	r.mu.Lock()
	for _, vl := range r.listeners {
		vl.Close()
	}
	clear(r.listeners)
	r.mu.Unlock()
}

const portRouterReadDeadline = 10 * time.Second

func (r *PortRouter) Serve(l net.Listener) {
	for {
		conn, err := l.Accept()
		if err != nil {
			return
		}
		go r.handleConn(conn)
	}
}

func (r *PortRouter) handleConn(conn net.Conn) {
	if err := conn.SetReadDeadline(time.Now().Add(portRouterReadDeadline)); err != nil {
		r.log.Debug().Err(err).Msg("failed to set read deadline, closing connection")
		conn.Close()
		return
	}

	br := bufio.NewReaderSize(conn, 16389) //nolint:mnd // 5-byte header + 16384 body = max TLS record

	var hostname string
	switch r.mode {
	case RouteSNI:
		hostname = clientHelloServerName(br)
	case RouteHTTPHost:
		hostname = httpHostHeader(br)
	}

	if hostname == "" {
		conn.Close()
		return
	}

	_ = conn.SetReadDeadline(time.Time{})

	r.mu.RLock()
	vl, ok := r.listeners[hostname]
	r.mu.RUnlock()

	if !ok {
		r.log.Debug().Str("hostname", hostname).Msg("unknown hostname, closing connection")
		conn.Close()
		return
	}

	peeked, _ := br.Peek(br.Buffered())
	wrapped := &peekConn{
		Conn:   conn,
		reader: io.MultiReader(bytes.NewReader(peeked), conn),
	}

	if !vl.Dispatch(wrapped) {
		wrapped.Close()
	}
}

type peekConn struct {
	net.Conn
	reader io.Reader
}

func (c *peekConn) Read(b []byte) (int, error) {
	return c.reader.Read(b)
}

// SNI and HTTP Host sniffing logic adapted from github.com/inetaf/tcpproxy
// Copyright 2017 Google Inc., Apache 2.0 License

func clientHelloServerName(br *bufio.Reader) (sni string) {
	const recordHeaderLen = 5
	hdr, err := br.Peek(recordHeaderLen)
	if err != nil {
		return ""
	}
	const recordTypeHandshake = 0x16
	if hdr[0] != recordTypeHandshake {
		return ""
	}
	recLen := int(hdr[3])<<8 | int(hdr[4])
	helloBytes, err := br.Peek(recordHeaderLen + recLen)
	if err != nil {
		return ""
	}
	tls.Server(sniSniffConn{r: bytes.NewReader(helloBytes)}, &tls.Config{
		GetConfigForClient: func(hello *tls.ClientHelloInfo) (*tls.Config, error) {
			sni = hello.ServerName
			return nil, nil
		},
	}).Handshake()
	return
}

type sniSniffConn struct {
	r        io.Reader
	net.Conn
}

func (c sniSniffConn) Read(p []byte) (int, error) { return c.r.Read(p) }
func (sniSniffConn) Write(p []byte) (int, error)  { return 0, io.EOF }

func httpHostHeader(br *bufio.Reader) string {
	const maxPeek = 4 << 10 // 4KB

	// Peek up to 4KB of data to find the Host header.
	// br.Peek returns what's available without blocking if it's less than maxPeek.
	b, err := br.Peek(maxPeek)
	if err != nil && len(b) == 0 {
		return ""
	}

	if len(b) > 0 {
		// HTTP methods are always uppercase A-Z.
		if b[0] < 'A' || b[0] > 'Z' {
			return ""
		}
	}

	// Look for the end of headers (\r\n\r\n or \n\n).
	// If we don't find it within the first 4KB, we stop.
	eofHeaders := bytes.Index(b, crlfcrlf)
	if eofHeaders == -1 {
		eofHeaders = bytes.Index(b, lflf)
	}

	if eofHeaders != -1 {
		// Found end of headers. Use http.ReadRequest for robust parsing.
		// We only pass the headers part to ReadRequest.
		req, err := http.ReadRequest(bufio.NewReader(bytes.NewReader(b[:eofHeaders+4])))
		if err != nil {
			return ""
		}
		return req.Host
	}

	// End of headers not found in the peeked buffer.
	// Fallback to a simpler search if the buffer is full but we didn't find the end.
	return httpHostHeaderFromBytes(b)
}

var (
	lfHostColon = []byte("\nHost:")
	lfhostColon = []byte("\nhost:")
	crlfcrlf    = []byte("\r\n\r\n")
	lflf        = []byte("\n\n")
)

func httpHostHeaderFromBytes(b []byte) string {
	if i := bytes.Index(b, lfHostColon); i != -1 {
		return string(bytes.TrimSpace(untilEOL(b[i+len(lfHostColon):])))
	}
	if i := bytes.Index(b, lfhostColon); i != -1 {
		return string(bytes.TrimSpace(untilEOL(b[i+len(lfhostColon):])))
	}
	return ""
}

func untilEOL(v []byte) []byte {
	for i, b := range v {
		if b == '\r' || b == '\n' || b == 0 {
			return v[:i]
		}
	}
	return v
}
