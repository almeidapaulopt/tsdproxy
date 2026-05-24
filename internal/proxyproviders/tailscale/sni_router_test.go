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
	sniExt := make([]byte, 0)
	sniExt = append(sniExt, 0x00, 0x00) // extension type: SNI
	extLen := uint16(2 + 1 + 2 + len(sniBytes))
	sniExt = binary.BigEndian.AppendUint16(sniExt, extLen)
	listLen := uint16(1 + 2 + len(sniBytes))
	sniExt = binary.BigEndian.AppendUint16(sniExt, listLen)
	sniExt = append(sniExt, 0x00) // host name type
	sniExt = binary.BigEndian.AppendUint16(sniExt, uint16(len(sniBytes)))
	sniExt = append(sniExt, sniBytes...)

	hello := make([]byte, 0)
	hello = append(hello, 0x03, 0x03) // version TLS 1.2
	random := make([]byte, 32)
	hello = append(hello, random...)
	hello = append(hello, 0x00)                   // session ID len = 0
	hello = append(hello, 0x00, 0x02, 0x00, 0x2f) // cipher suites
	hello = append(hello, 0x01, 0x00)             // compression: null
	hello = binary.BigEndian.AppendUint16(hello, uint16(len(sniExt)))
	hello = append(hello, sniExt...)

	handshake := make([]byte, 0)
	handshake = append(handshake, 0x01) // ClientHello
	hLen := uint32(len(hello))
	handshake = append(handshake, byte(hLen>>16), byte(hLen>>8), byte(hLen))
	handshake = append(handshake, hello...)

	record := make([]byte, 0)
	record = append(record, 0x16)       // handshake
	record = append(record, 0x03, 0x01) // TLS 1.0
	record = binary.BigEndian.AppendUint16(record, uint16(len(handshake)))
	record = append(record, handshake...)

	return record
}

func buildClientHelloNoSNI() []byte {
	ext := make([]byte, 0)
	ext = binary.BigEndian.AppendUint16(ext, 0x0001) // extension type: max_fragment_length (not SNI)
	ext = binary.BigEndian.AppendUint16(ext, 1)      // ext length
	ext = append(ext, 0x02)                          // MFL value

	hello := make([]byte, 0)
	hello = append(hello, 0x03, 0x03)
	random := make([]byte, 32)
	hello = append(hello, random...)
	hello = append(hello, 0x00)
	hello = append(hello, 0x00, 0x02, 0x00, 0x2f)
	hello = append(hello, 0x01, 0x00)
	hello = binary.BigEndian.AppendUint16(hello, uint16(len(ext)))
	hello = append(hello, ext...)

	handshake := make([]byte, 0)
	handshake = append(handshake, 0x01)
	hLen := uint32(len(hello))
	handshake = append(handshake, byte(hLen>>16), byte(hLen>>8), byte(hLen))
	handshake = append(handshake, hello...)

	record := make([]byte, 0)
	record = append(record, 0x16)
	record = append(record, 0x03, 0x01)
	record = binary.BigEndian.AppendUint16(record, uint16(len(handshake)))
	record = append(record, handshake...)

	return record
}

func TestExtractSNI(t *testing.T) {
	t.Parallel()

	data := buildClientHello("example.com")
	br := bufio.NewReaderSize(bytes.NewReader(data), 16384)

	sni, err := extractSNI(br)
	if err != nil {
		t.Fatalf("extractSNI returned error: %v", err)
	}
	if sni != "example.com" {
		t.Fatalf("expected SNI %q, got %q", "example.com", sni)
	}
}

func TestExtractSNIEmpty(t *testing.T) {
	t.Parallel()

	data := buildClientHelloNoSNI()
	br := bufio.NewReaderSize(bytes.NewReader(data), 16384)

	_, err := extractSNI(br)
	if !errors.Is(err, errNoSNI) {
		t.Fatalf("expected errNoSNI, got: %v", err)
	}
}

func TestExtractSNIInvalidHandshake(t *testing.T) {
	t.Parallel()

	record := []byte{0x17, 0x03, 0x01, 0x00, 0x02, 0x00, 0x00} // non-handshake type
	br := bufio.NewReaderSize(bytes.NewReader(record), 16384)

	_, err := extractSNI(br)
	if !errors.Is(err, errNotHandshake) {
		t.Fatalf("expected errNotHandshake, got: %v", err)
	}
}

func TestExtractSNINotClientHello(t *testing.T) {
	t.Parallel()

	hello := make([]byte, 0)
	hello = append(hello, 0x03, 0x03)
	random := make([]byte, 32)
	hello = append(hello, random...)
	hello = append(hello, 0x00)
	hello = append(hello, 0x00, 0x02, 0x00, 0x2f)
	hello = append(hello, 0x01, 0x00)

	handshake := make([]byte, 0)
	handshake = append(handshake, 0x02) // ServerHello (not ClientHello)
	hLen := uint32(len(hello))
	handshake = append(handshake, byte(hLen>>16), byte(hLen>>8), byte(hLen))
	handshake = append(handshake, hello...)

	record := make([]byte, 0)
	record = append(record, 0x16)
	record = append(record, 0x03, 0x01)
	record = binary.BigEndian.AppendUint16(record, uint16(len(handshake)))
	record = append(record, handshake...)

	br := bufio.NewReaderSize(bytes.NewReader(record), 16384)

	_, err := extractSNI(br)
	if !errors.Is(err, errNotClientHello) {
		t.Fatalf("expected errNotClientHello, got: %v", err)
	}
}

func TestSNIRouterRegisterAndServe(t *testing.T) {
	router := NewSNIRouter(zerolog.Nop())

	vl := router.Register("test.example.com")

	server, client := net.Pipe()

	go func() {
		router.handleConn(server)
	}()

	hello := buildClientHello("test.example.com")
	_, err := client.Write(hello)
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

func TestSNIRouterUnknownDomain(t *testing.T) {
	router := NewSNIRouter(zerolog.Nop())

	_ = router.Register("known.example.com")

	server, client := net.Pipe()

	done := make(chan struct{})
	go func() {
		router.handleConn(server)
		close(done)
	}()

	hello := buildClientHello("unknown.example.com")
	_, err := client.Write(hello)
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

func TestSNIRouterUnregister(t *testing.T) {
	router := NewSNIRouter(zerolog.Nop())

	vl := router.Register("remove.example.com")
	router.Unregister("remove.example.com")

	server, client := net.Pipe()

	done := make(chan struct{})
	go func() {
		router.handleConn(server)
		close(done)
	}()

	hello := buildClientHello("remove.example.com")
	_, err := client.Write(hello)
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
