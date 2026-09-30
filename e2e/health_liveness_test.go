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

	resp, err := http.Get(proxy.BaseURL + "/health/ready/")
	require.NoError(t, err)
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode, "ready: body=%s", string(body))

	resp2, err := http.Get(proxy.BaseURL + "/health/live/")
	require.NoError(t, err)
	resp2.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp2.StatusCode,
		"/health/live/ is not yet implemented, should return 404")
}
