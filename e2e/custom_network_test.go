// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/network"
)

func TestCustomDockerNetwork(t *testing.T) {
	authKey := requireTailscaleAuth(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	dockNet, err := network.New(ctx, network.WithAttachable())
	require.NoError(t, err, "failed to create custom Docker network")
	testcontainers.CleanupNetwork(t, dockNet)

	_ = StartTSDProxy(t, TSDProxyConfig{
  AuthKey: authKey,
})

	hostname := fmt.Sprintf("e2e-custom-net-%d", time.Now().UnixNano())

	StartContainer(t, ContainerConfig{
		Labels: map[string]string{
			"tsdproxy.enable":     "true",
			"tsdproxy.ephemeral":  "true",
			"tsdproxy.name":       hostname,
			"tsdproxy.autodetect": "true",
			"tsdproxy.port.http":  "80/http:80/http",
		},
		Networks: []string{dockNet.Name},
	})

	client := NewTSNetClient(t, authKey)
	proxyURL := client.ProxyHTTPURL(hostname)

	WaitForProxyReachable(t, ctx, client, proxyURL, 120*time.Second)
	VerifyHTTPResponse(t, ctx, client, proxyURL, "Welcome to nginx!")
}
