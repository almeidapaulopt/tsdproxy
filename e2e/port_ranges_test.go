// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPortRangeTCP(t *testing.T) {
	authKey := requireTailscaleAuth(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	proxy := StartTSDProxy(t, TSDProxyConfig{AuthKey: authKey})
	_ = proxy

	hostname := fmt.Sprintf("e2e-portrange-%d", time.Now().UnixNano())
	StartContainer(t, ContainerConfig{
		Labels: map[string]string{
			"tsdproxy.enable":       "true",
			"tsdproxy.name":         hostname,
			"tsdproxy.ephemeral":    "true",
			"tsdproxy.port.range":   "2222-2224/tcp:80/http",
		},
	})

	client := NewTSNetClient(t, authKey)

	for _, port := range []int{2222, 2223, 2224} {
		addr := client.ProxyTCPAddress(hostname, port)
		var lastErr error
		success := assert.Eventually(t, func() bool {
			dialCtx, dialCancel := context.WithTimeout(ctx, 5*time.Second)
			defer dialCancel()
			conn, err := client.Dial(dialCtx, "tcp", addr)
			if err != nil {
				lastErr = err
				return false
			}
			conn.Close()
			return true
		}, 90*time.Second, 2*time.Second)
		require.True(t, success, "port %d should be reachable: %v", port, lastErr)
	}
}

func TestPortRangeHTTP(t *testing.T) {
	authKey := requireTailscaleAuth(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	proxy := StartTSDProxy(t, TSDProxyConfig{AuthKey: authKey})
	_ = proxy

	hostname := fmt.Sprintf("e2e-porthttp-%d", time.Now().UnixNano())
	StartContainer(t, ContainerConfig{
		Labels: map[string]string{
			"tsdproxy.enable":    "true",
			"tsdproxy.name":      hostname,
			"tsdproxy.ephemeral": "true",
			"tsdproxy.port.1":    "8080-8081/http:80/http",
		},
	})

	client := NewTSNetClient(t, authKey)

	for _, port := range []int{8080, 8081} {
		proxyURL := fmt.Sprintf("http://%s.%s:%d/", hostname, client.MagicDNSSuffix, port)
		WaitForProxyReachable(t, ctx, client, proxyURL, 90*time.Second)
		VerifyHTTPResponse(t, ctx, client, proxyURL, "Welcome to nginx!")
	}
}
