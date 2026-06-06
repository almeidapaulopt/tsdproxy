// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func startUDPEcho(t *testing.T) *net.UDPConn {
	t.Helper()
	addr, err := net.ResolveUDPAddr("udp", "127.0.0.1:0")
	require.NoError(t, err)
	ln, err := net.ListenUDP("udp", addr)
	require.NoError(t, err)
	t.Cleanup(func() { ln.Close() })
	go func() {
		buf := make([]byte, 4096)
		for {
			n, client, err := ln.ReadFromUDP(buf)
			if err != nil {
				return
			}
			ln.WriteToUDP(buf[:n], client)
		}
	}()
	return ln
}

func TestUDPForward(t *testing.T) {
	authKey := requireTailscaleAuth(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	echoLn := startUDPEcho(t)
	echoAddr := echoLn.LocalAddr().String()

	hostname := fmt.Sprintf("e2e-udp-%d", time.Now().UnixNano())
	listFilePath := GenerateListProviderFile(t, map[string]ListEntry{
		hostname: {
			ProxyProvider: "default",
			Tailscale:     ListTailscale{Ephemeral: true},
			Dashboard:     ListDashboard{Visible: true, Label: "UDP E2E"},
			Ports: map[string]ListPort{
				"5060/udp": {Targets: []string{"udp://" + echoAddr}},
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
    targetHostname: "172.17.0.1"
lists:
  udplist:
    filename: %q
tailscale:
  providers:
    default:
      authKey: %q
      tags: %q
  dataDir: %q
http:
  hostname: "0.0.0.0"
  port: %d
log:
  level: debug
  json: false
proxyAccessLog: true
`, listFilePath, authKey, tsTags, dataDir, httpPort)

	startTSDProxyRawConfig(t, configContent, httpPort, tmpDir, dataDir)

	client := NewTSNetClient(t, authKey)
	waitForPeerByDNSName(t, ctx, client, hostname, 90*time.Second)

	dialAddr := client.ProxyTCPAddress(hostname, 5060)
	conn, err := client.Dial(ctx, "udp", dialAddr)
	if err != nil {
		t.Skipf("tsnet does not support UDP dial: %v", err)
	}
	defer conn.Close()

	message := []byte("hello udp e2e")
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(10*time.Second)))
	_, err = conn.Write(message)
	require.NoError(t, err)

	buf := make([]byte, 1024)
	n, err := conn.Read(buf)
	require.NoError(t, err)
	require.Equal(t, string(message), string(buf[:n]))
}

func TestUDPThroughDockerLabel(t *testing.T) {
	authKey := requireTailscaleAuth(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	echoLn := startUDPEcho(t)
	echoPort := echoLn.LocalAddr().(*net.UDPAddr).Port

	hostname := fmt.Sprintf("e2e-udp-docker-%d", time.Now().UnixNano())
	StartContainer(t, ContainerConfig{
		Labels: map[string]string{
			"tsdproxy.enable":    "true",
			"tsdproxy.name":      hostname,
			"tsdproxy.ephemeral": "true",
			"tsdproxy.port.udp":  fmt.Sprintf("%d/udp:%d/udp", echoPort, echoPort),
		},
	})

	proxy := StartTSDProxy(t, TSDProxyConfig{
		AuthKey:        authKey,
		TargetHostname: "127.0.0.1",
	})
	_ = proxy

	client := NewTSNetClient(t, authKey)
	waitForPeerByDNSName(t, ctx, client, hostname, 90*time.Second)

	dialAddr := client.ProxyTCPAddress(hostname, echoPort)
	conn, err := client.Dial(ctx, "udp", dialAddr)
	if err != nil {
		t.Skipf("tsnet does not support UDP dial: %v", err)
	}
	defer conn.Close()

	message := []byte("hello udp docker e2e")
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(10*time.Second)))
	_, err = conn.Write(message)
	require.NoError(t, err)

	buf := make([]byte, 1024)
	n, err := conn.Read(buf)
	require.NoError(t, err)
	require.Equal(t, string(message), string(buf[:n]))
}
