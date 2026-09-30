// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestConcurrentContainerStart(t *testing.T) {
	authKey := requireTailscaleAuth(t)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	proxy := StartTSDProxy(t, TSDProxyConfig{AuthKey: authKey})
	_ = proxy
	client := NewTSNetClient(t, authKey)

	const count = 5
	hostnames := make([]string, count)

	var wg sync.WaitGroup
	startTime := time.Now()
	for i := range count {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			hostnames[idx] = fmt.Sprintf("e2e-concurrent-%d-%d-%d", idx, startTime.UnixNano(), time.Now().UnixNano())
			StartContainer(t, ContainerConfig{Labels: httpLabels(hostnames[idx])})
		}(i)
	}
	wg.Wait()

	for i, hostname := range hostnames {
		proxyURL := client.ProxyHTTPURL(hostname)
		WaitForProxyReachable(t, ctx, client, proxyURL, 120*time.Second)

		peer := waitForPeerByDNSName(t, ctx, client, hostname, 30*time.Second)
		dnsName := strings.TrimSuffix(peer.DNSName, ".")
		dnsName = strings.TrimSuffix(dnsName, "."+client.MagicDNSSuffix)
		require.Equal(t, hostname, dnsName,
			"hostname mismatch for container %d — DeviceReconciler race may have appended -1 suffix", i)
	}
}

func TestConcurrentStartStopSameContainer(t *testing.T) {
	authKey := requireTailscaleAuth(t)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	proxy := StartTSDProxy(t, TSDProxyConfig{AuthKey: authKey})
	_ = proxy
	client := NewTSNetClient(t, authKey)

	hostname := fmt.Sprintf("e2e-cycle-%d", time.Now().UnixNano())

	for range 3 {
		ctr := StartContainer(t, ContainerConfig{Labels: httpLabels(hostname)})

		proxyURL := client.ProxyHTTPURL(hostname)
		WaitForProxyReachable(t, ctx, client, proxyURL, 90*time.Second)
		VerifyHTTPResponse(t, ctx, client, proxyURL, "Welcome to nginx!")

		stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
		require.NoError(t, ctr.Stop(stopCtx, nil))
		stopCancel()

		time.Sleep(5 * time.Second)
	}
}
