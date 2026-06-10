// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package tailscale

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"errors"
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
	RouteSNI RoutingMode = iota
	RouteHTTPHost
)

type PortRouter struct {
	log       zerolog.Logger
	listeners map[string]*VirtualListener
	mode      RoutingMode
	mu        sync.RWMutex
}

const (
	tlsRecordBufSize = 16389 // 5-byte TLS record header + 16384 max body
	httpReadBufSize  = 256
	crlfCRLFDelimLen = 4 // \r\n\r\n
	lfLFDelimLen     = 2 // \n\n
)

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

	vl := NewVirtualListener(&net.TCPAddr{IP: net.ParseIP("0.0.0.0"), Port: 0}, r.log.With().Str("domain", domain).Logger())
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

const maxHostnameLen = 253

func (r *PortRouter) Serve(l net.Listener) {
	for {
		conn, err := l.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}
			r.log.Warn().Err(err).Msg("port router: accept error, retrying")
			time.Sleep(time.Second)
			continue
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

	br := bufio.NewReaderSize(conn, tlsRecordBufSize)

	var consumed []byte // bytes consumed from br during sniffing, must be replayed
	var hostname string

	switch r.mode {
	case RouteSNI:
		hostname = clientHelloServerName(br)
		// SNI uses Peek, so nothing consumed; all bytes are still in br's buffer.
		var err error
		consumed, err = br.Peek(br.Buffered())
		if err != nil {
			r.log.Debug().Err(err).Msg("failed to peek buffered bytes for SNI replay, closing connection")
			conn.Close()
			return
		}
	case RouteHTTPHost:
		hostname, consumed = httpHostHeader(br)
		// HTTP reads from br to accumulate headers; consumed holds everything read so far.
		// Any remaining buffered bytes beyond what we Read must also be replayed.
		if remaining, _ := br.Peek(br.Buffered()); len(remaining) > 0 {
			consumed = append(consumed, remaining...)
		}
	}

	if hostname == "" || len(hostname) > maxHostnameLen {
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

	wrapped := &readerConn{
		Conn:   conn,
		reader: io.MultiReader(bytes.NewReader(consumed), conn),
	}

	if !vl.Dispatch(wrapped) {
		wrapped.Close()
	}
}

// readerConn wraps a net.Conn with a custom io.Reader for replaying peeked bytes.
// Also used by clientHelloServerName to feed TLS handshake bytes to tls.Server.
type readerConn struct {
	net.Conn
	reader io.Reader
}

func (c *readerConn) Read(b []byte) (int, error) { return c.reader.Read(b) }

// Write delegates to the underlying Conn when present. When readerConn is
// used for TLS ClientHello sniffing (no Conn), Write returns io.EOF to
// satisfy the tls.Config handshake callback without writing to the wire.
func (c *readerConn) Write(b []byte) (int, error) {
	if c.Conn != nil {
		return c.Conn.Write(b)
	}
	return 0, io.EOF
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
	_ = tls.Server(&readerConn{reader: bytes.NewReader(helloBytes)}, &tls.Config{
		GetConfigForClient: func(hello *tls.ClientHelloInfo) (*tls.Config, error) {
			sni = hello.ServerName
			return nil, nil //nolint:nilnil // required by GetConfigForClient API to stop handshake
		},
	}).Handshake() //nolint:errcheck // handshake only extracts SNI; error is irrelevant
	return
}

func httpHostHeader(br *bufio.Reader) (hostname string, consumed []byte) {
	const maxTotal = 4 << 10 // 4KB upper bound

	var buf bytes.Buffer
	tmp := make([]byte, httpReadBufSize)

	for buf.Len() < maxTotal {
		n, err := br.Read(tmp)
		if n > 0 {
			buf.Write(tmp[:n])
		}
		b := buf.Bytes()

		if len(b) > 0 && (b[0] < 'A' || b[0] > 'Z') {
			return "", nil
		}

		delimLen := crlfCRLFDelimLen
		eofHeaders := bytes.Index(b, crlfcrlf)
		if eofHeaders == -1 {
			eofHeaders = bytes.Index(b, lflf)
			delimLen = lfLFDelimLen
		}

		if eofHeaders != -1 {
			req, reqErr := http.ReadRequest(bufio.NewReader(bytes.NewReader(b[:eofHeaders+delimLen])))
			if reqErr == nil {
				return stripPort(req.Host), b
			}
			return "", nil
		}

		if err != nil {
			return stripPort(httpHostHeaderFromBytes(b)), b
		}
	}

	b := buf.Bytes()
	return stripPort(httpHostHeaderFromBytes(b)), b
}

func stripPort(hostport string) string {
	host, _, err := net.SplitHostPort(hostport)
	if err != nil {
		return hostport
	}
	return host
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
