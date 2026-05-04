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
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestE2ETCPForward(t *testing.T) {
	authKey := requireTailscaleAuth(t)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	echoLn, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err, "failed to start echo server")
	defer echoLn.Close()
	go serveEcho(echoLn)

	hostname := fmt.Sprintf("e2e-tcp-%d", time.Now().UnixNano())
	listPath := filepath.Join(e2eTestDataDir(t), "tcp-list.yaml")
	WriteListProviderFile(t, listPath, map[string]ListEntry{
		hostname: {
			ProxyProvider: "default",
			Tailscale: ListTailscale{
				Ephemeral: true,
			},
			Dashboard: ListDashboard{Visible: true, Label: "TCP E2E"},
			Ports: map[string]ListPort{
				"443/tcp": {
					Targets: []string{"tcp://" + echoLn.Addr().String()},
				},
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
  testlist:
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

	_ = startTSDProxyRawConfig(t, configContent, httpPort, tmpDir, dataDir)
	client := NewTSNetClient(t, authKey)

	peer := waitForPeerByDNSName(t, ctx, client, hostname, 90*time.Second)
	dnsName := strings.TrimSuffix(peer.DNSName, ".")
	t.Logf("TCP proxy running at %s", dnsName)

	dialAddr := client.ProxyTCPAddress(hostname, 443)
	conn, err := client.Dial(ctx, "tcp", dialAddr)
	require.NoError(t, err, "failed to dial TCP proxy %s", dialAddr)
	defer conn.Close()

	message := []byte("hello from e2e test over tailscale tcp")
	require.NoError(t, conn.SetDeadline(time.Now().Add(10*time.Second)), "failed to set deadline")
	_, err = conn.Write(message)
	require.NoError(t, err, "failed to write to TCP proxy")

	buf := make([]byte, len(message))
	n, err := io.ReadFull(conn, buf)
	require.NoError(t, err, "failed to read from TCP proxy")
	require.Equal(t, string(message), string(buf[:n]), "unexpected echo response")

	t.Logf("echo verified: %q", buf[:n])
}

func serveEcho(ln net.Listener) {
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
}
