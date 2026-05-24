// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package tailscale

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

var (
	errNotHandshake        = errors.New("not a TLS handshake record")
	errNotClientHello      = errors.New("not a ClientHello message")
	errIncompleteHandshake = errors.New("incomplete handshake data")
	errNoExtensions        = errors.New("no extensions in ClientHello")
	errNoSNI               = errors.New("no SNI extension found")
)

// SNIRouter accepts raw TCP connections, peeks the TLS ClientHello to
// extract the SNI hostname, and dispatches each connection to the
// VirtualListener registered for that domain.
type SNIRouter struct {
	log       zerolog.Logger
	listeners map[string]*VirtualListener
	mu        sync.RWMutex
}

func NewSNIRouter(log zerolog.Logger) *SNIRouter {
	return &SNIRouter{
		listeners: make(map[string]*VirtualListener),
		log:       log,
	}
}

func (r *SNIRouter) Register(domain string) *VirtualListener {
	vl := NewVirtualListener(&net.TCPAddr{IP: net.ParseIP("0.0.0.0"), Port: 0})
	r.mu.Lock()
	r.listeners[domain] = vl
	r.mu.Unlock()
	return vl
}

func (r *SNIRouter) Unregister(domain string) {
	r.mu.Lock()
	if vl, ok := r.listeners[domain]; ok {
		vl.Close()
		delete(r.listeners, domain)
	}
	r.mu.Unlock()
}

// CloseAll closes all registered virtual listeners and clears the map.
func (r *SNIRouter) CloseAll() {
	r.mu.Lock()
	for _, vl := range r.listeners {
		vl.Close()
	}
	clear(r.listeners)
	r.mu.Unlock()
}

// Serve starts the accept loop on the given listener.
// Blocks until the listener is closed.
func (r *SNIRouter) Serve(l net.Listener) {
	for {
		conn, err := l.Accept()
		if err != nil {
			return
		}
		go r.handleConn(conn)
	}
}

// sniReadDeadline is the maximum time to wait for the TLS ClientHello.
const sniReadDeadline = 10 * time.Second

func (r *SNIRouter) handleConn(conn net.Conn) {
	// Set a read deadline to prevent Slowloris-style resource exhaustion.
	// A well-behaved TLS client sends the ClientHello immediately after
	// the TCP handshake; 10s is generous.
	if err := conn.SetReadDeadline(time.Now().Add(sniReadDeadline)); err != nil {
		r.log.Debug().Err(err).Msg("failed to set read deadline, closing connection")
		conn.Close()
		return
	}

	br := bufio.NewReaderSize(conn, 16384) //nolint:mnd // TLS records up to 16KB

	sni, err := extractSNI(br)
	if err != nil {
		r.log.Debug().Err(err).Msg("failed to extract SNI, closing connection")
		conn.Close()
		return
	}

	if sni == "" {
		r.log.Debug().Msg("no SNI in ClientHello, closing connection")
		conn.Close()
		return
	}

	// Clear the deadline so the downstream handler owns its own timeouts.
	_ = conn.SetReadDeadline(time.Time{})

	r.mu.RLock()
	vl, ok := r.listeners[sni]
	r.mu.RUnlock()

	if !ok {
		r.log.Debug().Str("sni", sni).Msg("unknown SNI domain, closing connection")
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

// peekConn wraps a net.Conn, replaying peeked bytes first.
type peekConn struct {
	net.Conn
	reader io.Reader
}

func (c *peekConn) Read(b []byte) (int, error) {
	return c.reader.Read(b)
}

// extractSNI reads a TLS ClientHello from br and returns the SNI hostname.
func extractSNI(br *bufio.Reader) (string, error) {
	header, err := br.Peek(5) //nolint:mnd // TLS record header: type(1) + version(2) + length(2)
	if err != nil {
		return "", err
	}

	if header[0] != 0x16 { // handshake
		return "", errNotHandshake
	}

	recordLen := int(binary.BigEndian.Uint16(header[3:5]))

	record, err := br.Peek(5 + recordLen) //nolint:mnd
	if err != nil {
		return "", err
	}

	body := record[5:]

	if len(body) < 4 || body[0] != 0x01 { // ClientHello
		return "", errNotClientHello
	}

	handshakeLen := int(body[1])<<16 | int(body[2])<<8 | int(body[3])
	if len(body) < 4+handshakeLen {
		return "", errIncompleteHandshake
	}

	hello := body[4:]

	// Client version (2 bytes)
	if len(hello) < 2 {
		return "", errIncompleteHandshake
	}
	pos := 2

	// Random (32 bytes)
	if len(hello) < pos+32 { //nolint:mnd
		return "", errIncompleteHandshake
	}
	pos += 32 //nolint:mnd

	// Session ID (1-byte length prefix)
	if len(hello) <= pos {
		return "", errIncompleteHandshake
	}
	sessionIDLen := int(hello[pos])
	pos++
	if len(hello) < pos+sessionIDLen {
		return "", errIncompleteHandshake
	}
	pos += sessionIDLen

	// Cipher suites (2-byte length prefix)
	if len(hello) < pos+2 {
		return "", errIncompleteHandshake
	}
	cipherLen := int(binary.BigEndian.Uint16(hello[pos : pos+2]))
	pos += 2
	if len(hello) < pos+cipherLen {
		return "", errIncompleteHandshake
	}
	pos += cipherLen

	// Compression methods (1-byte length prefix)
	if len(hello) <= pos {
		return "", errIncompleteHandshake
	}
	compLen := int(hello[pos])
	pos++
	if len(hello) < pos+compLen {
		return "", errIncompleteHandshake
	}
	pos += compLen

	// Extensions total length (2 bytes)
	if len(hello) < pos+2 {
		return "", errNoExtensions
	}
	extTotalLen := int(binary.BigEndian.Uint16(hello[pos : pos+2]))
	pos += 2

	extEnd := pos + extTotalLen
	for pos+4 <= extEnd {
		extType := binary.BigEndian.Uint16(hello[pos : pos+2])
		extLen := int(binary.BigEndian.Uint16(hello[pos+2 : pos+4]))
		pos += 4

		if pos+extLen > extEnd {
			break
		}

		// SNI extension (type 0x0000)
		if extType == 0x0000 {
			if extLen < 2 {
				break
			}
			listLen := int(binary.BigEndian.Uint16(hello[pos : pos+2]))
			listPos := pos + 2

			for listPos+3 <= pos+2+listLen {
				nameType := hello[listPos]
				nameLen := int(binary.BigEndian.Uint16(hello[listPos+1 : listPos+3]))
				listPos += 3

				if nameType == 0x00 && listPos+nameLen <= pos+2+listLen {
					return string(hello[listPos : listPos+nameLen]), nil
				}
				listPos += nameLen
			}
		}

		pos += extLen
	}

	return "", errNoSNI
}
