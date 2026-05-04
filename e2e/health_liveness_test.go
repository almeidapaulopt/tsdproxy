// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

//go:build e2e

package e2e

import (
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHealthLivenessEndpoint(t *testing.T) {
	proxy := StartTSDProxy(t, TSDProxyConfig{
		AuthKey: requireTailscaleAuth(t),
	})

	// Verify /health/ready/ returns 200.
	resp, err := http.Get(proxy.BaseURL + "/health/ready/")
	require.NoError(t, err)
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode, "ready: body=%s", string(body))

	t.Skip("not yet implemented: /health/live/ endpoint")
}
