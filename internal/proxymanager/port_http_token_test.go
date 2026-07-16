// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package proxymanager

import (
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"testing"

	"github.com/almeidapaulopt/tsdproxy/internal/consts"
	"github.com/almeidapaulopt/tsdproxy/internal/model"
)

func TestProxyRewriteRoundRobinManagementTokenIsolation(t *testing.T) {
	t.Parallel()

	const (
		managementPort = "8080"
		authToken      = "management-token"
	)

	pconfig := model.PortConfig{
		ProxyProtocol: model.ProtoHTTP,
		LoadBalance:   model.LoadBalanceRoundRobin,
	}
	pconfig.AddTarget(urlMustParse(t, "http://127.0.0.1:"+managementPort))
	pconfig.AddTarget(urlMustParse(t, "http://backend:8080"))

	rewrite := proxyRewrite(pconfig, true, managementPort, authToken)
	tests := []struct {
		name      string
		wantHost  string
		wantToken string
	}{
		{name: "management target receives token", wantHost: "127.0.0.1:8080", wantToken: authToken},
		{name: "non-management target does not receive token", wantHost: "backend:8080"},
	}

	for _, tc := range tests {
		in := httptest.NewRequest(http.MethodGet, "http://proxy.local/path", nil)
		in = in.WithContext(model.WhoisNewContext(in.Context(), model.Whois{ID: "user-1"}))
		out := in.Clone(in.Context())
		rewrite(&httputil.ProxyRequest{In: in, Out: out})

		if out.URL.Host != tc.wantHost {
			t.Errorf("%s: got upstream host %q, want %q", tc.name, out.URL.Host, tc.wantHost)
		}
		if got := out.Header.Get(consts.HeaderAuthToken); got != tc.wantToken {
			t.Errorf("%s: got auth token %q, want %q", tc.name, got, tc.wantToken)
		}
	}
}
