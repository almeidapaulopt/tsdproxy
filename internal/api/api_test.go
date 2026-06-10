// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package api

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/almeidapaulopt/tsdproxy/internal/config"
	"github.com/almeidapaulopt/tsdproxy/internal/core"
	"github.com/almeidapaulopt/tsdproxy/internal/model"
	"github.com/almeidapaulopt/tsdproxy/internal/proxymanager"
	"github.com/almeidapaulopt/tsdproxy/internal/proxyproviders"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

// Stub implementations for proxyproviders interfaces.
// These allow creating Proxy objects via proxymanager.NewProxy without
// a real Tailscale connection.

type stubProvider struct{}

func (s *stubProvider) ResolveAuthKey(_ *model.Config) (string, error) { return "", nil }

func (s *stubProvider) NewProxy(_ *model.Config) (proxyproviders.ProxyInterface, error) {
	return &stubProxyInterface{}, nil
}

type stubProxyInterface struct{}

func (s *stubProxyInterface) Start(_ context.Context) error { return nil }
func (s *stubProxyInterface) Close() error                  { return nil }

var errStubNotAvailable = errors.New("stub: not available")

func (s *stubProxyInterface) GetListener(_ string) (net.Listener, error) {
	return nil, errStubNotAvailable
}

func (s *stubProxyInterface) GetPacketConn(_ string) (net.PacketConn, error) {
	return nil, errStubNotAvailable
}
func (s *stubProxyInterface) GetURL() string                     { return "https://testproxy.example.com" }
func (s *stubProxyInterface) GetAuthURL() string                 { return "https://auth.example.com" }
func (s *stubProxyInterface) WatchEvents() chan model.ProxyEvent { return nil }
func (s *stubProxyInterface) Whois(_ *http.Request) model.Whois  { return model.Whois{} }

func newTestProxy(t *testing.T, name string, visible bool) *proxymanager.Proxy {
	t.Helper()
	proxy, err := proxymanager.NewProxy(
		zerolog.Nop(),
		&model.Config{
			Hostname: name,
			Dashboard: model.Dashboard{
				Label:   name,
				Visible: visible,
			},
			Tailscale: model.Tailscale{
				Tags: "tag:test",
			},
		},
		&stubProvider{},
		nil, // metrics — nil safe for handlers that only read Config
	)
	if err != nil {
		t.Fatalf("NewProxy failed: %v", err)
	}
	return proxy
}

func newTestProxyWithPorts(t *testing.T, name string, visible bool, ports model.PortConfigList) *proxymanager.Proxy {
	t.Helper()
	proxy, err := proxymanager.NewProxy(
		zerolog.Nop(),
		&model.Config{
			Hostname: name,
			Dashboard: model.Dashboard{
				Label:   name,
				Visible: visible,
			},
			Tailscale: model.Tailscale{
				Tags: "tag:test",
			},
			Ports: ports,
		},
		&stubProvider{},
		nil,
	)
	if err != nil {
		t.Fatalf("NewProxy failed: %v", err)
	}
	return proxy
}

func setupAPI(t *testing.T) (*API, *proxymanager.ProxyManager) {
	t.Helper()
	origConfig := config.Config
	t.Cleanup(func() { config.Config = origConfig })
	config.SetTestConfig(t.TempDir(), "")
	config.Config.AdminAllowLocalhost = true

	oldReg := prometheus.DefaultRegisterer
	oldGatherer := prometheus.DefaultGatherer
	reg := prometheus.NewRegistry()
	prometheus.DefaultRegisterer = reg
	prometheus.DefaultGatherer = reg
	t.Cleanup(func() {
		prometheus.DefaultRegisterer = oldReg
		prometheus.DefaultGatherer = oldGatherer
	})

	httpSrv := core.NewHTTPServer(zerolog.Nop())
	pm := proxymanager.NewProxyManager(zerolog.Nop())

	api := New(httpSrv, pm, zerolog.Nop())
	api.AddRoutes()

	return api, pm
}

func localhostRequest(method, target string) *http.Request {
	r := httptest.NewRequest(method, target, nil)
	r.RemoteAddr = "127.0.0.1:12345"
	return r
}

func TestNewAPI(t *testing.T) {
	api, _ := setupAPI(t)
	if api.HTTP == nil {
		t.Fatal("expected HTTP server to be set")
	}
}

func TestAPI_AddRoutes(t *testing.T) {
	api, _ := setupAPI(t)
	if api.HTTP.Mux == nil {
		t.Fatal("expected mux to be set")
	}
}

func TestAPI_ListProxiesHandler_Empty(t *testing.T) {
	api, _ := setupAPI(t)

	w := httptest.NewRecorder()
	r := localhostRequest("GET", "/api/v1/proxies")
	api.HTTP.Mux.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp apiProxiesResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json unmarshal failed: %v", err)
	}
	if len(resp.Proxies) != 0 {
		t.Fatalf("expected empty proxies array, got %d", len(resp.Proxies))
	}
}

func TestAPI_ListProxiesHandler_WithProxies(t *testing.T) {
	api, pm := setupAPI(t)

	pm.Proxies["testproxy"] = newTestProxy(t, "testproxy", true)

	w := httptest.NewRecorder()
	r := localhostRequest("GET", "/api/v1/proxies")
	api.HTTP.Mux.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp apiProxiesResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json unmarshal failed: %v", err)
	}
	if len(resp.Proxies) != 1 {
		t.Fatalf("expected 1 proxy, got %d", len(resp.Proxies))
	}
	if resp.Proxies[0].Name != "testproxy" {
		t.Fatalf("expected name testproxy, got %s", resp.Proxies[0].Name)
	}
}

func TestAPI_ListProxiesHandler_FiltersInvisible(t *testing.T) {
	api, pm := setupAPI(t)

	pm.Proxies["visible"] = newTestProxy(t, "visible", true)
	pm.Proxies["hidden"] = newTestProxy(t, "hidden", false)

	w := httptest.NewRecorder()
	r := localhostRequest("GET", "/api/v1/proxies")
	api.HTTP.Mux.ServeHTTP(w, r)

	var resp apiProxiesResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	if len(resp.Proxies) != 1 {
		t.Fatalf("expected 1 visible proxy, got %d", len(resp.Proxies))
	}
	if resp.Proxies[0].Name != "visible" {
		t.Fatalf("expected visible proxy, got %s", resp.Proxies[0].Name)
	}
}

func TestAPI_GetProxyHandler(t *testing.T) {
	api, pm := setupAPI(t)

	pm.Proxies["testproxy"] = newTestProxy(t, "testproxy", true)

	w := httptest.NewRecorder()
	r := localhostRequest("GET", "/api/v1/proxies/testproxy")
	api.HTTP.Mux.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var proxy apiProxy
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &proxy))
	if proxy.Name != "testproxy" {
		t.Fatalf("expected testproxy, got %s", proxy.Name)
	}
}

func TestAPI_GetProxyHandler_NotFound(t *testing.T) {
	api, _ := setupAPI(t)

	w := httptest.NewRecorder()
	r := localhostRequest("GET", "/api/v1/proxies/nonexistent")
	api.HTTP.Mux.ServeHTTP(w, r)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestAPI_GetProxyPortsHandler(t *testing.T) {
	api, pm := setupAPI(t)

	pm.Proxies["testproxy"] = newTestProxyWithPorts(t, "testproxy", true, model.PortConfigList{
		"1": {ProxyProtocol: "https", ProxyPort: 443},
		"2": {ProxyProtocol: "http", ProxyPort: 80, IsRedirect: true},
	})

	w := httptest.NewRecorder()
	r := localhostRequest("GET", "/api/v1/proxies/testproxy/ports")
	api.HTTP.Mux.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Ports []apiPort `json:"ports"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	if len(resp.Ports) != 2 {
		t.Fatalf("expected 2 ports, got %d", len(resp.Ports))
	}
}

func TestAPI_GetProxyPortsHandler_NotFound(t *testing.T) {
	api, _ := setupAPI(t)

	w := httptest.NewRecorder()
	r := localhostRequest("GET", "/api/v1/proxies/nonexistent/ports")
	api.HTTP.Mux.ServeHTTP(w, r)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestAPI_VersionHandler(t *testing.T) {
	api, _ := setupAPI(t)

	w := httptest.NewRecorder()
	r := localhostRequest("GET", "/api/version")
	api.HTTP.Mux.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var v apiVersionResponse
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
		t.Fatalf("json unmarshal failed: %v", err)
	}
	if v.Author == "" {
		t.Fatal("expected author to be set")
	}
}

func TestAPI_AggregateHealthHandler(t *testing.T) {
	api, pm := setupAPI(t)

	pm.Proxies["running"] = newTestProxy(t, "running", true)

	w := httptest.NewRecorder()
	r := localhostRequest("GET", "/api/health")
	api.HTTP.Mux.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var h apiHealthResponse
	if err := json.Unmarshal(w.Body.Bytes(), &h); err != nil {
		t.Fatalf("json unmarshal failed: %v", err)
	}
	if h.Total != 1 {
		t.Fatalf("expected total 1, got %d", h.Total)
	}
}

func TestAPI_WriteJSONError(t *testing.T) {
	api, _ := setupAPI(t)

	w := httptest.NewRecorder()
	api.writeJSONError(w, "test error", http.StatusBadRequest)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}

	var errResp apiErrorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("json unmarshal failed: %v", err)
	}
	if errResp.Message != "test error" {
		t.Fatalf("expected 'test error', got %s", errResp.Message)
	}
}
