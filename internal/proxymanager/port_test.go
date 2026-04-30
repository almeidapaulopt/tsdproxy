// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package proxymanager

import (
	"context"
	"io"
	"net"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/almeidapaulopt/tsdproxy/internal/model"
	"github.com/rs/zerolog"
)

func newTestTCPConfig(t *testing.T, targetAddr string) model.PortConfig {
	t.Helper()
	targetURL, err := url.Parse("tcp://" + targetAddr)
	if err != nil {
		t.Fatalf("failed to parse target URL: %v", err)
	}
	pconfig := model.PortConfig{
		ProxyProtocol: "tcp",
	}
	pconfig.AddTarget(targetURL)
	return pconfig
}

func startEchoBackend(t *testing.T) (net.Listener, *sync.WaitGroup) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				_, _ = io.Copy(c, c)
			}(conn)
		}
	}()
	return ln, &wg
}

func TestTCPPortForward(t *testing.T) {
	backendLn, backendWg := startEchoBackend(t)
	defer backendLn.Close()

	pconfig := newTestTCPConfig(t, backendLn.Addr().String())
	tp := newPortTCP(context.Background(), pconfig, zerolog.Nop())

	frontLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create frontend listener: %v", err)
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- tp.startWithListener(frontLn)
	}()

	clientConn, err := net.DialTimeout("tcp", frontLn.Addr().String(), 2*time.Second)
	if err != nil {
		t.Fatalf("failed to connect to frontend: %v", err)
	}
	defer clientConn.Close()

	message := []byte("hello tcp proxy")
	if _, err := clientConn.Write(message); err != nil {
		t.Fatalf("failed to write: %v", err)
	}

	buf := make([]byte, len(message))
	if err := clientConn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("failed to set deadline: %v", err)
	}
	n, err := clientConn.Read(buf)
	if err != nil {
		t.Fatalf("failed to read: %v", err)
	}
	if string(buf[:n]) != string(message) {
		t.Fatalf("expected %q, got %q", message, buf[:n])
	}

	if err := clientConn.Close(); err != nil {
		t.Fatalf("failed to close client: %v", err)
	}

	tp.close()
	backendLn.Close()
	backendWg.Wait()

	if err := <-errCh; err != nil {
		t.Fatalf("startWithListener returned error: %v", err)
	}
}

func TestTCPPortMultipleConnections(t *testing.T) {
	backendLn, backendWg := startEchoBackend(t)
	defer backendLn.Close()

	pconfig := newTestTCPConfig(t, backendLn.Addr().String())
	tp := newPortTCP(context.Background(), pconfig, zerolog.Nop())

	frontLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create frontend listener: %v", err)
	}

	go tp.startWithListener(frontLn)

	for i := range 5 {
		clientConn, err := net.DialTimeout("tcp", frontLn.Addr().String(), 2*time.Second)
		if err != nil {
			t.Fatalf("connection %d: failed to connect: %v", i, err)
		}

		message := []byte("message " + string(rune('A'+i)))
		if _, err := clientConn.Write(message); err != nil {
			t.Fatalf("connection %d: failed to write: %v", i, err)
		}

		buf := make([]byte, len(message))
		if err := clientConn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
			t.Fatalf("connection %d: failed to set deadline: %v", i, err)
		}
		n, err := clientConn.Read(buf)
		if err != nil {
			t.Fatalf("connection %d: failed to read: %v", i, err)
		}
		if string(buf[:n]) != string(message) {
			t.Fatalf("connection %d: expected %q, got %q", i, message, buf[:n])
		}
		clientConn.Close()
	}

	tp.close()
	backendLn.Close()
	backendWg.Wait()
}

func TestTCPPortClose(t *testing.T) {
	pconfig := model.PortConfig{ProxyProtocol: "tcp"}
	tp := newPortTCP(context.Background(), pconfig, zerolog.Nop())

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- tp.startWithListener(ln)
	}()

	time.Sleep(50 * time.Millisecond)

	if err := tp.close(); err != nil {
		t.Fatalf("close returned error: %v", err)
	}

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("startWithListener returned error after close: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("startWithListener did not return after close")
	}

	_, err = ln.Accept()
	if err == nil {
		t.Fatal("expected error from Accept on closed listener")
	}
}

func TestTCPPortEmptyTarget(t *testing.T) {
	pconfig := model.PortConfig{ProxyProtocol: "tcp"}
	tp := newPortTCP(context.Background(), pconfig, zerolog.Nop())

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}

	go tp.startWithListener(ln)

	clientConn, err := net.DialTimeout("tcp", ln.Addr().String(), 2*time.Second)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer clientConn.Close()

	if err := clientConn.SetReadDeadline(time.Now().Add(500 * time.Millisecond)); err != nil {
		t.Fatalf("failed to set deadline: %v", err)
	}
	buf := make([]byte, 1024)
	n, err := clientConn.Read(buf)

	if n != 0 || err == nil {
		t.Fatal("expected connection to be closed by server when no target configured")
	}

	tp.close()
	ln.Close()
}

func TestTCPPortCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	pconfig := model.PortConfig{ProxyProtocol: "tcp"}
	tp := newPortTCP(ctx, pconfig, zerolog.Nop())

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- tp.startWithListener(ln)
	}()

	cancel()

	select {
	case <-errCh:
	case <-time.After(2 * time.Second):
		t.Fatal("startWithListener did not return after context cancellation")
	}

	ln.Close()
}
