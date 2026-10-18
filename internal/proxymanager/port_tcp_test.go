// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package proxymanager

import (
	"context"
	"io"
	"net"
	"testing"
	"time"

	"github.com/almeidapaulopt/tsdproxy/internal/model"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

func TestTCPPort_CustomIdleTimeout(t *testing.T) {
	t.Parallel()

	backendLn, backendWg := startEchoBackend(t)
	defer backendLn.Close()

	pconfig := newTestTCPConfig(t, backendLn.Addr().String())
	pconfig.TCPIdleTimeout = 150 * time.Millisecond

	tp := newPortTCP(context.Background(), pconfig, zerolog.Nop(), nil, "", "")
	frontLn, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	errCh := make(chan error, 1)
	go func() {
		errCh <- tp.startWithListener(frontLn)
	}()

	clientConn, err := net.DialTimeout("tcp", frontLn.Addr().String(), 2*time.Second)
	require.NoError(t, err)
	defer clientConn.Close()

	// Wait longer than the custom idle timeout without sending application data
	time.Sleep(300 * time.Millisecond)

	// Since idle timeout was 150ms, the proxy should have closed the connection
	buf := make([]byte, 16)
	_ = clientConn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	_, err = clientConn.Read(buf)
	require.Error(t, err, "expected read error on idle timed-out connection")

	tp.close()
	backendLn.Close()
	backendWg.Wait()
	_ = <-errCh
}

func TestTCPPort_DisabledIdleTimeout(t *testing.T) {
	t.Parallel()

	backendLn, backendWg := startEchoBackend(t)
	defer backendLn.Close()

	pconfig := newTestTCPConfig(t, backendLn.Addr().String())
	pconfig.TCPIdleTimeoutDisabled = true
	pconfig.TCPIdleTimeout = 0

	tp := newPortTCP(context.Background(), pconfig, zerolog.Nop(), nil, "", "")
	frontLn, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	errCh := make(chan error, 1)
	go func() {
		errCh <- tp.startWithListener(frontLn)
	}()

	clientConn, err := net.DialTimeout("tcp", frontLn.Addr().String(), 2*time.Second)
	require.NoError(t, err)
	defer clientConn.Close()

	// Sleep for a quiet period
	time.Sleep(200 * time.Millisecond)

	// After quiet period, connection must remain healthy and active
	msg := []byte("hello after quiet period")
	_, err = clientConn.Write(msg)
	require.NoError(t, err)

	buf := make([]byte, len(msg))
	_ = clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, err := io.ReadFull(clientConn, buf)
	require.NoError(t, err)
	require.Equal(t, string(msg), string(buf[:n]))

	tp.close()
	backendLn.Close()
	backendWg.Wait()
	_ = <-errCh
}

func TestTCPPort_KeepAliveConfigured(t *testing.T) {
	t.Parallel()

	backendLn, backendWg := startEchoBackend(t)
	defer backendLn.Close()

	pconfig := newTestTCPConfig(t, backendLn.Addr().String())
	pconfig.TCPKeepAliveEnabled = true
	pconfig.TCPKeepAlive = 50 * time.Millisecond

	tp := newPortTCP(context.Background(), pconfig, zerolog.Nop(), nil, "", "")
	frontLn, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	errCh := make(chan error, 1)
	go func() {
		errCh <- tp.startWithListener(frontLn)
	}()

	clientConn, err := net.DialTimeout("tcp", frontLn.Addr().String(), 2*time.Second)
	require.NoError(t, err)
	defer clientConn.Close()

	msg := []byte("keepalive test")
	_, err = clientConn.Write(msg)
	require.NoError(t, err)

	buf := make([]byte, len(msg))
	_ = clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, err := io.ReadFull(clientConn, buf)
	require.NoError(t, err)
	require.Equal(t, string(msg), string(buf[:n]))

	tp.close()
	backendLn.Close()
	backendWg.Wait()
	_ = <-errCh
}

func TestTCPPort_IdleTimeoutHelper(t *testing.T) {
	t.Parallel()

	t.Run("default timeout", func(t *testing.T) {
		t.Parallel()
		tp := &tcpPort{pconfig: model.PortConfig{}}
		require.Equal(t, tcpIdleTimeout, tp.idleTimeout())
	})

	t.Run("custom timeout", func(t *testing.T) {
		t.Parallel()
		tp := &tcpPort{pconfig: model.PortConfig{TCPIdleTimeout: 10 * time.Minute}}
		require.Equal(t, 10*time.Minute, tp.idleTimeout())
	})

	t.Run("disabled timeout", func(t *testing.T) {
		t.Parallel()
		tp := &tcpPort{pconfig: model.PortConfig{TCPIdleTimeoutDisabled: true}}
		require.Equal(t, time.Duration(0), tp.idleTimeout())
	})

	t.Run("negative timeout", func(t *testing.T) {
		t.Parallel()
		tp := &tcpPort{pconfig: model.PortConfig{TCPIdleTimeout: -1}}
		require.Equal(t, time.Duration(0), tp.idleTimeout())
	})
}
