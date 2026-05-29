// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package tailscale

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"net"
	"testing"

	"github.com/rs/zerolog"
)

func buildClientHello(sni string) []byte {
	sniBytes := []byte(sni)
	sniExt := make([]byte, 0, 2+1+2+len(sniBytes)+2+2+1+2+len(sniBytes))
	sniExt = append(sniExt, 0x00, 0x00)         // extension type: SNI
	extLen := uint16(2 + 1 + 2 + len(sniBytes)) //nolint:gosec // test data, no overflow risk
	sniExt = binary.BigEndian.AppendUint16(sniExt, extLen)
	listLen := uint16(1 + 2 + len(sniBytes)) //nolint:gosec // test data, no overflow risk
	sniExt = binary.BigEndian.AppendUint16(sniExt, listLen)
	sniExt = append(sniExt, 0x00)                                         // host name type
	sniExt = binary.BigEndian.AppendUint16(sniExt, uint16(len(sniBytes))) //nolint:gosec // test data
	sniExt = append(sniExt, sniBytes...)

	hello := make([]byte, 0, 2+32+1+4+2+len(sniExt))
	hello = append(hello, 0x03, 0x03) // version TLS 1.2
	random := make([]byte, 32)
	hello = append(hello, random...)
	hello = append(hello, 0x00)                                       // session ID len = 0
	hello = append(hello, 0x00, 0x02, 0x00, 0x2f)                     // cipher suites
	hello = append(hello, 0x01, 0x00)                                 // compression: null
	hello = binary.BigEndian.AppendUint16(hello, uint16(len(sniExt))) //nolint:gosec // test data
	hello = append(hello, sniExt...)

	handshake := make([]byte, 0, 4+len(hello))
	handshake = append(handshake, 0x01)                                      // ClientHello
	hLen := uint32(len(hello))                                               //nolint:gosec // test data, no overflow risk
	handshake = append(handshake, byte(hLen>>16), byte(hLen>>8), byte(hLen)) //nolint:gosec // test data
	handshake = append(handshake, hello...)

	record := make([]byte, 0)
	record = append(record, 0x16)                                          // handshake
	record = append(record, 0x03, 0x01)                                    // TLS 1.0
	record = binary.BigEndian.AppendUint16(record, uint16(len(handshake))) //nolint:gosec // test data
	record = append(record, handshake...)

	return record
}

func buildClientHelloNoSNI() []byte {
	ext := make([]byte, 0, 2+2+1)
	ext = binary.BigEndian.AppendUint16(ext, 0x0001) // extension type: max_fragment_length (not SNI)
	ext = binary.BigEndian.AppendUint16(ext, 1)      // ext length
	ext = append(ext, 0x02)                          // MFL value

	hello := make([]byte, 0, 2+32+1+4+2+len(ext))
	hello = append(hello, 0x03, 0x03)
	random := make([]byte, 32)
	hello = append(hello, random...)
	hello = append(hello, 0x00)
	hello = append(hello, 0x00, 0x02, 0x00, 0x2f)
	hello = append(hello, 0x01, 0x00)
	hello = binary.BigEndian.AppendUint16(hello, uint16(len(ext))) //nolint:gosec // test data
	hello = append(hello, ext...)

	handshake := make([]byte, 0, 4+len(hello))
	handshake = append(handshake, 0x01)
	hLen := uint32(len(hello))                                               //nolint:gosec // test data
	handshake = append(handshake, byte(hLen>>16), byte(hLen>>8), byte(hLen)) //nolint:gosec // test data
	handshake = append(handshake, hello...)

	record := make([]byte, 0)
	record = append(record, 0x16)
	record = append(record, 0x03, 0x01)
	record = binary.BigEndian.AppendUint16(record, uint16(len(handshake))) //nolint:gosec // test data
	record = append(record, handshake...)

	return record
}

func TestClientHelloServerName(t *testing.T) {
	t.Parallel()

	data := buildClientHello("example.com")
	br := bufio.NewReaderSize(bytes.NewReader(data), 16384)

	sni := clientHelloServerName(br)
	if sni != "example.com" {
		t.Fatalf("expected SNI %q, got %q", "example.com", sni)
	}
}

func TestClientHelloServerNameEmpty(t *testing.T) {
	t.Parallel()

	data := buildClientHelloNoSNI()
	br := bufio.NewReaderSize(bytes.NewReader(data), 16384)

	sni := clientHelloServerName(br)
	if sni != "" {
		t.Fatalf("expected empty SNI, got %q", sni)
	}
}

func TestClientHelloServerNameInvalidHandshake(t *testing.T) {
	t.Parallel()

	record := []byte{0x17, 0x03, 0x01, 0x00, 0x02, 0x00, 0x00} // non-handshake type
	br := bufio.NewReaderSize(bytes.NewReader(record), 16384)

	sni := clientHelloServerName(br)
	if sni != "" {
		t.Fatalf("expected empty SNI for non-handshake, got %q", sni)
	}
}

func TestClientHelloServerNameNotClientHello(t *testing.T) {
	t.Parallel()

	hello := make([]byte, 0, 2+32+1+4+2)
	hello = append(hello, 0x03, 0x03)
	random := make([]byte, 32)
	hello = append(hello, random...)
	hello = append(hello, 0x00)
	hello = append(hello, 0x00, 0x02, 0x00, 0x2f)
	hello = append(hello, 0x01, 0x00)

	handshake := make([]byte, 0, 4+len(hello))
	handshake = append(handshake, 0x02)                                      // ServerHello (not ClientHello)
	hLen := uint32(len(hello))                                               //nolint:gosec // test data
	handshake = append(handshake, byte(hLen>>16), byte(hLen>>8), byte(hLen)) //nolint:gosec // test data
	handshake = append(handshake, hello...)

	record := make([]byte, 0)
	record = append(record, 0x16)
	record = append(record, 0x03, 0x01)
	record = binary.BigEndian.AppendUint16(record, uint16(len(handshake))) //nolint:gosec // test data
	record = append(record, handshake...)

	br := bufio.NewReaderSize(bytes.NewReader(record), 16384)

	sni := clientHelloServerName(br)
	if sni != "" {
		t.Fatalf("expected empty SNI for non-ClientHello, got %q", sni)
	}
}

func TestPortRouterRegisterAndServe(t *testing.T) {
	router := NewPortRouter(RouteSNI, zerolog.Nop())

	vl, err := router.Register("test.example.com")
	if err != nil {
		t.Fatalf("Register returned error: %v", err)
	}

	server, client := net.Pipe()

	go func() {
		router.handleConn(server)
	}()

	hello := buildClientHello("test.example.com")
	_, err = client.Write(hello)
	if err != nil {
		t.Fatalf("failed to write ClientHello: %v", err)
	}

	conn, err := vl.Accept()
	if err != nil {
		t.Fatalf("Accept returned error: %v", err)
	}
	defer conn.Close()

	buf := make([]byte, len(hello))
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("Read from dispatched connection returned error: %v", err)
	}
	if n != len(hello) {
		t.Fatalf("expected %d bytes, got %d", len(hello), n)
	}

	client.Close()
	vl.Close()
}

func TestPortRouterUnknownDomain(t *testing.T) {
	router := NewPortRouter(RouteSNI, zerolog.Nop())

	_, err := router.Register("known.example.com")
	if err != nil {
		t.Fatalf("Register returned error: %v", err)
	}

	server, client := net.Pipe()

	done := make(chan struct{})
	go func() {
		router.handleConn(server)
		close(done)
	}()

	hello := buildClientHello("unknown.example.com")
	_, err = client.Write(hello)
	if err != nil {
		t.Fatalf("failed to write ClientHello: %v", err)
	}

	<-done

	buf := make([]byte, 1)
	_, err = client.Read(buf)
	if err == nil {
		t.Fatal("expected connection to be closed for unknown domain")
	}

	client.Close()
}

func TestPortRouterUnregister(t *testing.T) {
	router := NewPortRouter(RouteSNI, zerolog.Nop())

	vl, err := router.Register("remove.example.com")
	if err != nil {
		t.Fatalf("Register returned error: %v", err)
	}
	router.Unregister("remove.example.com")

	server, client := net.Pipe()

	done := make(chan struct{})
	go func() {
		router.handleConn(server)
		close(done)
	}()

	hello := buildClientHello("remove.example.com")
	_, err = client.Write(hello)
	if err != nil {
		t.Fatalf("failed to write ClientHello: %v", err)
	}

	<-done

	buf := make([]byte, 1)
	_, err = client.Read(buf)
	if err == nil {
		t.Fatal("expected connection to be closed after unregister")
	}

	client.Close()

	_, err = vl.Accept()
	if !errors.Is(err, net.ErrClosed) {
		t.Fatalf("expected net.ErrClosed from Accept after unregister, got: %v", err)
	}
}

func TestHTTPHostHeader_CRLFDelimiter(t *testing.T) {
	t.Parallel()

	raw := "GET / HTTP/1.1\r\nHost: example.com\r\nUser-Agent: test\r\n\r\n"
	br := bufio.NewReaderSize(bytes.NewReader([]byte(raw)), 16384)

	host, consumed := httpHostHeader(br)
	if host != "example.com" {
		t.Fatalf("expected %q, got %q", "example.com", host)
	}
	if string(consumed) != raw {
		t.Fatalf("expected consumed %q, got %q", raw, string(consumed))
	}
}

func TestHTTPHostHeader_LFDelimiter(t *testing.T) {
	t.Parallel()

	raw := "GET / HTTP/1.1\nHost: example.com\nUser-Agent: test\n\n"
	br := bufio.NewReaderSize(bytes.NewReader([]byte(raw)), 16384)

	host, consumed := httpHostHeader(br)
	if host != "example.com" {
		t.Fatalf("expected %q, got %q", "example.com", host)
	}
	if string(consumed) != raw {
		t.Fatalf("expected consumed %q, got %q", raw, string(consumed))
	}
}

func TestHTTPHostHeader_MultipleHostHeaders(t *testing.T) {
	t.Parallel()

	raw := "GET / HTTP/1.1\r\nHost: first.com\r\nHost: second.com\r\n\r\n"
	br := bufio.NewReaderSize(bytes.NewReader([]byte(raw)), 16384)

	host, consumed := httpHostHeader(br)
	if host != "" {
		t.Fatalf("expected empty host (ReadRequest rejects duplicate Host), got %q", host)
	}
	if len(consumed) != 0 {
		t.Fatalf("expected empty consumed on invalid headers, got %d bytes", len(consumed))
	}
}

func TestHTTPHostHeader_EmptyHost(t *testing.T) {
	t.Parallel()

	raw := "GET / HTTP/1.1\r\nHost:\r\n\r\n"
	br := bufio.NewReaderSize(bytes.NewReader([]byte(raw)), 16384)

	host, consumed := httpHostHeader(br)
	if host != "" {
		t.Fatalf("expected empty host, got %q", host)
	}
	if string(consumed) != raw {
		t.Fatalf("expected consumed %q, got %q", raw, string(consumed))
	}
}

func TestHTTPHostHeader_NoHeadersEnd(t *testing.T) {
	t.Parallel()

	raw := "GET / HTTP/1.1\r\nHost: example.com\r\nUser-Agent: test"
	br := bufio.NewReaderSize(bytes.NewReader([]byte(raw)), 16384)

	host, consumed := httpHostHeader(br)
	if host != "example.com" {
		t.Fatalf("expected %q (fallback), got %q", "example.com", host)
	}
	if string(consumed) != raw {
		t.Fatalf("expected consumed %q, got %q", raw, string(consumed))
	}
}

func TestPortRouterHTTPHostMode(t *testing.T) {
	router := NewPortRouter(RouteHTTPHost, zerolog.Nop())

	vl, err := router.Register("http.example.com")
	if err != nil {
		t.Fatalf("Register returned error: %v", err)
	}

	server, client := net.Pipe()

	go func() {
		router.handleConn(server)
	}()

	req := "GET / HTTP/1.1\r\nHost: http.example.com\r\n\r\n"
	_, err = client.Write([]byte(req))
	if err != nil {
		t.Fatalf("failed to write HTTP request: %v", err)
	}

	conn, err := vl.Accept()
	if err != nil {
		t.Fatalf("Accept returned error: %v", err)
	}
	defer conn.Close()

	buf := make([]byte, len(req))
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("Read from dispatched connection returned error: %v", err)
	}
	if n != len(req) {
		t.Fatalf("expected %d bytes, got %d", len(req), n)
	}
	if string(buf[:n]) != req {
		t.Fatalf("expected %q, got %q", req, string(buf[:n]))
	}

	client.Close()
	vl.Close()
}
