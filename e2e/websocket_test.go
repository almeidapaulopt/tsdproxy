// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

//go:build e2e

package e2e

import (
	"bufio"
	"context"
	"crypto/sha1"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestWebSocketForwarding(t *testing.T) {
	authKey := requireTailscaleAuth(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// Start a raw WebSocket echo server.
	echoLn, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err, "failed to start WS echo server")
	defer echoLn.Close()
	go serveWSEcho(echoLn)

	echoPort := echoLn.Addr().(*net.TCPAddr).Port

	hostname := fmt.Sprintf("e2e-ws-%d", time.Now().UnixNano())
	listPath := filepath.Join(e2eTestDataDir(t), "ws-list.yaml")
	WriteListProviderFile(t, listPath, map[string]ListEntry{
		hostname: {
			ProxyProvider: "default",
			Tailscale:     ListTailscale{Ephemeral: true},
			Dashboard:     ListDashboard{Visible: true, Label: "WS E2E"},
			Ports: map[string]ListPort{
				"80/http": {Targets: []string{fmt.Sprintf("http://127.0.0.1:%d", echoPort)}},
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
  wslist:
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
	proxyURL := client.ProxyHTTPURL(hostname)
	WaitForProxyReachable(t, ctx, client, proxyURL, 90*time.Second)

	// Perform a raw WebSocket handshake through the proxy.
	wsHost := fmt.Sprintf("%s.%s:80", hostname, client.MagicDNSSuffix)
	conn, err := client.Dial(ctx, "tcp", wsHost)
	require.NoError(t, err, "failed to dial proxy for WebSocket")
	defer conn.Close()
	require.NoError(t, conn.SetDeadline(time.Now().Add(10*time.Second)))

	// Send WebSocket upgrade request.
	const wsKey = "dGhlIHNhbXBsZSBub25jZQ=="
	upgradeReq := fmt.Sprintf("GET /ws HTTP/1.1\r\n"+
		"Host: %s\r\n"+
		"Upgrade: websocket\r\n"+
		"Connection: Upgrade\r\n"+
		"Sec-WebSocket-Key: %s\r\n"+
		"Sec-WebSocket-Version: 13\r\n\r\n", wsHost, wsKey)

	_, err = conn.Write([]byte(upgradeReq))
	require.NoError(t, err, "failed to write WebSocket upgrade request")

	reader := bufio.NewReader(conn)
	resp, err := http.ReadResponse(reader, nil)
	require.NoError(t, err, "failed to read WebSocket upgrade response")
	defer resp.Body.Close()

	require.Equal(t, http.StatusSwitchingProtocols, resp.StatusCode,
		"expected 101 Switching Protocols, got %d", resp.StatusCode)
	require.Equal(t, "websocket", strings.ToLower(resp.Header.Get("Upgrade")))

	expectedAccept := wsAcceptKey(wsKey)
	require.Equal(t, expectedAccept, resp.Header.Get("Sec-WebSocket-Accept"),
		"Sec-WebSocket-Accept mismatch")

	// Send a masked WebSocket text frame with payload "hello".
	frame := []byte{
		0x81,                   // FIN + text opcode
		0x85,                   // Masked, length 5
		0x00, 0x00, 0x00, 0x00, // Masking key (zeros for simplicity)
		'h', 'e', 'l', 'l', 'o',
	}
	_, err = conn.Write(frame)
	require.NoError(t, err, "failed to write WebSocket frame")

	// Read echoed frame (unmasked from server).
	header := make([]byte, 2)
	_, err = io.ReadFull(reader, header)
	require.NoError(t, err, "failed to read echo frame header")
	require.Equal(t, byte(0x81), header[0], "expected FIN+text frame")

	payloadLen := int(header[1] & 0x7f)
	payload := make([]byte, payloadLen)
	_, err = io.ReadFull(reader, payload)
	require.NoError(t, err, "failed to read echo frame payload")
	require.Equal(t, "hello", string(payload), "echo mismatch")
}

// serveWSEcho is a minimal WebSocket echo server.
func serveWSEcho(ln net.Listener) {
	srv := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := r.Header.Get("Sec-WebSocket-Key")
			if key == "" {
				http.Error(w, "missing key", http.StatusBadRequest)
				return
			}
			hijacker, ok := w.(http.Hijacker)
			if !ok {
				http.Error(w, "cannot hijack", http.StatusInternalServerError)
				return
			}
			conn, buf, err := hijacker.Hijack()
			if err != nil {
				return
			}
			defer conn.Close()

			accept := wsAcceptKey(key)
			resp := "HTTP/1.1 101 Switching Protocols\r\n" +
				"Upgrade: websocket\r\nConnection: Upgrade\r\n" +
				"Sec-WebSocket-Accept: " + accept + "\r\n\r\n"
			conn.Write([]byte(resp))

			for {
				hdr := make([]byte, 2)
				if _, err := io.ReadFull(buf, hdr); err != nil {
					return
				}
				masked := (hdr[1] & 0x80) != 0
				plen := int(hdr[1] & 0x7f)
				var mask [4]byte
				if masked {
					if _, err := io.ReadFull(buf, mask[:]); err != nil {
						return
					}
				}
				payload := make([]byte, plen)
				if _, err := io.ReadFull(buf, payload); err != nil {
					return
				}
				if masked {
					for i, b := range payload {
						payload[i] = b ^ mask[i%4]
					}
				}
				// Echo unmasked.
				echo := append([]byte{hdr[0], byte(plen)}, payload...)
				conn.Write(echo)
			}
		}),
	}
	srv.Serve(ln)
}

// wsAcceptKey computes the Sec-WebSocket-Accept value per RFC 6455.
func wsAcceptKey(key string) string {
	const guid = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"
	h := sha1.New()
	h.Write([]byte(key + guid))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}
