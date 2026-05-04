// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestTCPLargeDataTransfer(t *testing.T) {
	authKey := requireTailscaleAuth(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	echoLn, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer echoLn.Close()
	go serveEcho(echoLn)

	hostname := fmt.Sprintf("e2e-tcp-large-%d", time.Now().UnixNano())
	listPath := filepath.Join(e2eTestDataDir(t), "tcp-large-list.yaml")
	WriteListProviderFile(t, listPath, map[string]ListEntry{
		hostname: {
			ProxyProvider: "default",
			Tailscale:     ListTailscale{Ephemeral: true},
			Dashboard:     ListDashboard{Visible: true, Label: "TCP Large"},
			Ports: map[string]ListPort{
				"443/tcp": {Targets: []string{"tcp://" + echoLn.Addr().String()}},
			},
		},
	})

	httpPort := getFreePort()
	tmpDir := e2eTestDataDir(t)
	dataDir := filepath.Join(tmpDir, "data")
	require.NoError(t, os.MkdirAll(dataDir, 0o755))

	configContent := fmt.Sprintf(`defaultProxyProvider: default
docker:
  local:
    host: "unix:///var/run/docker.sock"
    targetHostname: "172.17.0.1"
lists:
  tcplist:
    filename: %q
tailscale:
  providers:
    default:
      authKey: %q
  dataDir: %q
http:
  hostname: "0.0.0.0"
  port: %d
log:
  level: debug
  json: false
proxyAccessLog: true
`, listPath, authKey, dataDir, httpPort)

	proxy := startTSDProxyRawConfig(t, configContent, httpPort, tmpDir, dataDir)
	_ = proxy

	client := NewTSNetClient(t, authKey)
	peer := waitForPeerByDNSName(t, ctx, client, hostname, 90*time.Second)
	_ = peer

	dialAddr := client.ProxyTCPAddress(hostname, 443)
	conn, err := client.Dial(ctx, "tcp", dialAddr)
	require.NoError(t, err)
	defer conn.Close()

	// Send 1MB of predictable data.
	const size = 1024 * 1024
	data := make([]byte, size)
	for i := range data {
		data[i] = byte(i % 256)
	}

	require.NoError(t, conn.SetDeadline(time.Now().Add(30*time.Second)))
	_, err = conn.Write(data)
	require.NoError(t, err, "failed to write %d bytes", size)

	received := make([]byte, size)
	_, err = io.ReadFull(conn, received)
	require.NoError(t, err, "failed to read %d bytes", size)
	require.Equal(t, data, received, "echo mismatch")
}

func TestTCPConcurrentConnections(t *testing.T) {
	authKey := requireTailscaleAuth(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	echoLn, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer echoLn.Close()
	go serveEcho(echoLn)

	hostname := fmt.Sprintf("e2e-tcp-conc-%d", time.Now().UnixNano())
	listPath := filepath.Join(e2eTestDataDir(t), "tcp-concurrent-list.yaml")
	WriteListProviderFile(t, listPath, map[string]ListEntry{
		hostname: {
			ProxyProvider: "default",
			Tailscale:     ListTailscale{Ephemeral: true},
			Dashboard:     ListDashboard{Visible: true, Label: "TCP Concurrent"},
			Ports: map[string]ListPort{
				"443/tcp": {Targets: []string{"tcp://" + echoLn.Addr().String()}},
			},
		},
	})

	httpPort := getFreePort()
	tmpDir := e2eTestDataDir(t)
	dataDir := filepath.Join(tmpDir, "data")
	require.NoError(t, os.MkdirAll(dataDir, 0o755))

	configContent := fmt.Sprintf(`defaultProxyProvider: default
docker:
  local:
    host: "unix:///var/run/docker.sock"
    targetHostname: "172.17.0.1"
lists:
  tcplist:
    filename: %q
tailscale:
  providers:
    default:
      authKey: %q
  dataDir: %q
http:
  hostname: "0.0.0.0"
  port: %d
log:
  level: debug
  json: false
proxyAccessLog: true
`, listPath, authKey, dataDir, httpPort)

	proxy := startTSDProxyRawConfig(t, configContent, httpPort, tmpDir, dataDir)
	_ = proxy

	client := NewTSNetClient(t, authKey)
	peer := waitForPeerByDNSName(t, ctx, client, hostname, 90*time.Second)
	_ = peer

	dialAddr := client.ProxyTCPAddress(hostname, 443)
	const numConns = 5
	var wg sync.WaitGroup
	var errors atomic.Int64

	for i := range numConns {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			conn, err := client.Dial(ctx, "tcp", dialAddr)
			if err != nil {
				errors.Add(1)
				t.Logf("conn %d: dial failed: %v", idx, err)
				return
			}
			defer conn.Close()

			msg := []byte(fmt.Sprintf("hello-from-conn-%d", idx))
			conn.SetDeadline(time.Now().Add(10 * time.Second))

			if _, err := conn.Write(msg); err != nil {
				errors.Add(1)
				t.Logf("conn %d: write failed: %v", idx, err)
				return
			}

			buf := make([]byte, len(msg))
			if _, err := io.ReadFull(conn, buf); err != nil {
				errors.Add(1)
				t.Logf("conn %d: read failed: %v", idx, err)
				return
			}

			if string(buf) != string(msg) {
				errors.Add(1)
				t.Logf("conn %d: mismatch: got %q, want %q", idx, string(buf), string(msg))
			}
		}(i)
	}

	wg.Wait()
	require.Equal(t, int64(0), errors.Load(),
		"%d/%d concurrent TCP connections failed", errors.Load(), numConns)
}
