// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestColdStartDiscovery(t *testing.T) {
	authKey := requireTailscaleAuth(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	hostname := fmt.Sprintf("e2e-coldstart-%d", time.Now().UnixNano())

	// Start container BEFORE tsdproxy to test the initial container scan path.
	StartContainer(t, ContainerConfig{
		Labels: map[string]string{
			"tsdproxy.enable":    "true",
			"tsdproxy.ephemeral": "true",
			"tsdproxy.name":      hostname,
			"tsdproxy.port.http": "80/http:80/http",
		},
	})

	// Now start tsdproxy — it should discover the already-running container.
	_ = StartTSDProxy(t, TSDProxyConfig{
  AuthKey: authKey,
})

	client := NewTSNetClient(t, authKey)
	proxyURL := client.ProxyHTTPURL(hostname)

	WaitForProxyReachable(t, ctx, client, proxyURL, 120*time.Second)
	VerifyHTTPResponse(t, ctx, client, proxyURL, "Welcome to nginx!")
}
