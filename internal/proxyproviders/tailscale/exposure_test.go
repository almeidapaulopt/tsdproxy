// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package tailscale

import (
	"context"
	"errors"
	"net"
	"testing"

	"github.com/rs/zerolog"
	"tailscale.com/tsnet"

	"github.com/almeidapaulopt/tsdproxy/internal/model"
)

type exposureTestServer struct {
	TSNetServer
	listener       net.Listener
	listenErr      error
	lastNetwork    string
	lastAddr       string
	listenCalls    int
	listenTLSCalls int
	funnelCalls    int
}

func (s *exposureTestServer) Listen(network, addr string) (net.Listener, error) {
	s.listenCalls++
	s.lastNetwork = network
	s.lastAddr = addr
	return s.listener, s.listenErr
}

func (s *exposureTestServer) ListenTLS(network, addr string) (net.Listener, error) {
	s.listenTLSCalls++
	s.lastNetwork = network
	s.lastAddr = addr
	return s.listener, nil
}

func (s *exposureTestServer) ListenFunnel(network, addr string, _ ...tsnet.FunnelOption) (net.Listener, error) {
	s.funnelCalls++
	s.lastNetwork = network
	s.lastAddr = addr
	return s.listener, nil
}

type closeTrackingListener struct {
	closed bool
}

func (*closeTrackingListener) Accept() (net.Conn, error) { return nil, net.ErrClosed }
func (l *closeTrackingListener) Close() error {
	l.closed = true
	return nil
}
func (*closeTrackingListener) Addr() net.Addr { return &net.TCPAddr{} }

func TestExposureLookup_NotStarted(t *testing.T) {
	t.Parallel()

	m := map[string]int{"port1": 42}
	_, err := exposureLookup[int](false, m, "port1")
	if !errors.Is(err, errExposureNotStarted) {
		t.Errorf("expected errExposureNotStarted, got %v", err)
	}
}

func TestExposureLookup_NotFound(t *testing.T) {
	t.Parallel()

	m := map[string]int{"port1": 42}
	_, err := exposureLookup[int](true, m, "nonexistent")
	if !errors.Is(err, ErrProxyPortNotFound) {
		t.Errorf("expected ErrProxyPortNotFound, got %v", err)
	}
}

func TestExposureLookup_Found(t *testing.T) {
	t.Parallel()

	m := map[string]int{"port1": 42}
	v, err := exposureLookup[int](true, m, "port1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v != 42 {
		t.Errorf("got %d, want %d", v, 42)
	}
}

func TestExposureLookup_EmptyMap(t *testing.T) {
	t.Parallel()

	m := map[string]string{}
	_, err := exposureLookup[string](true, m, "anything")
	if !errors.Is(err, ErrProxyPortNotFound) {
		t.Errorf("expected ErrProxyPortNotFound, got %v", err)
	}
}

// PerProxyExposure

func TestNewPerProxyExposure(t *testing.T) {
	t.Parallel()

	e := NewPerProxyExposure(zerolog.Nop())
	if e == nil {
		t.Fatal("NewPerProxyExposure returned nil")
	}
	if e.started {
		t.Fatal("new exposure should not be started")
	}
}

func TestPerProxyExposure_GetListener_NotStarted(t *testing.T) {
	t.Parallel()

	e := NewPerProxyExposure(zerolog.Nop())
	_, err := e.GetListener("https")
	if !errors.Is(err, errExposureNotStarted) {
		t.Errorf("expected errExposureNotStarted, got %v", err)
	}
}

func TestPerProxyExposure_GetRawTCPListener_NotStarted(t *testing.T) {
	t.Parallel()

	e := NewPerProxyExposure(zerolog.Nop())
	_, err := e.GetRawTCPListener("tcp")
	if !errors.Is(err, errExposureNotStarted) {
		t.Errorf("expected errExposureNotStarted, got %v", err)
	}
}

func TestPerProxyExposure_GetPacketConn_NotStarted(t *testing.T) {
	t.Parallel()

	e := NewPerProxyExposure(zerolog.Nop())
	_, err := e.GetPacketConn("udp")
	if !errors.Is(err, errExposureNotStarted) {
		t.Errorf("expected errExposureNotStarted, got %v", err)
	}
}

func TestPerProxyExposure_Close_Idempotent(t *testing.T) {
	t.Parallel()

	e := NewPerProxyExposure(zerolog.Nop())
	if err := e.Close(context.Background()); err != nil {
		t.Fatalf("unexpected error on first close: %v", err)
	}
	if err := e.Close(context.Background()); err != nil {
		t.Fatalf("unexpected error on second close: %v", err)
	}
}

func TestPerProxyExposure_Close_Started(t *testing.T) {
	t.Parallel()

	e := NewPerProxyExposure(zerolog.Nop())
	e.mtx.Lock()
	e.started = true
	e.mtx.Unlock()

	if err := e.Close(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	e.mtx.RLock()
	started := e.started
	e.mtx.RUnlock()
	if started {
		t.Error("started should be false after Close")
	}
}

func TestPerProxyExposure_HTTPSListenerSelection(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                string
		domain              string
		resolvedTLSProvider string
		funnel              bool
		wantRaw             bool
		wantFunnel          bool
	}{
		{name: "ordinary HTTPS uses Tailscale TLS"},
		{
			name:   "custom domain without resolved TLS uses Tailscale TLS fallback",
			domain: "app.example.com",
		},
		{
			name:       "ordinary HTTPS with Funnel uses Funnel",
			funnel:     true,
			wantFunnel: true,
		},
		{
			name:                "custom domain with Tailscale TLS uses Tailscale TLS",
			domain:              "app.example.com",
			resolvedTLSProvider: model.TLSProviderTailscale,
		},
		{
			name:                "custom domain with external TLS uses raw listener",
			domain:              "app.example.com",
			resolvedTLSProvider: model.TLSProviderACME,
			wantRaw:             true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			server := &exposureTestServer{listener: noopListener{}}
			exposure := NewPerProxyExposure(zerolog.Nop())
			cfg := &model.Config{
				Domain:              tt.domain,
				ResolvedTLSProvider: tt.resolvedTLSProvider,
				Ports: model.PortConfigList{
					"443/https": {
						ProxyProtocol: model.ProtoHTTPS,
						ProxyPort:     443,
						Tailscale: model.TailscalePort{
							Funnel: tt.funnel,
						},
					},
				},
			}

			if err := exposure.Start(context.Background(), &NodeRuntime{Server: server}, cfg); err != nil {
				t.Fatalf("Start returned an error: %v", err)
			}
			t.Cleanup(func() {
				if err := exposure.Close(context.Background()); err != nil {
					t.Errorf("Close returned an error: %v", err)
				}
			})

			assertExposureListenerSelection(t, exposure, server, tt.wantRaw, tt.wantFunnel)
		})
	}
}

func assertExposureListenerSelection(
	t *testing.T,
	exposure *PerProxyExposure,
	server *exposureTestServer,
	wantRaw bool,
	wantFunnel bool,
) {
	t.Helper()

	if server.lastNetwork != "tcp" || server.lastAddr != ":443" {
		t.Errorf("listener address = %q %q, want %q %q", server.lastNetwork, server.lastAddr, "tcp", ":443")
	}

	if wantRaw {
		if server.listenCalls != 1 || server.listenTLSCalls != 0 {
			t.Errorf("Listen calls = %d, ListenTLS calls = %d, want 1 and 0", server.listenCalls, server.listenTLSCalls)
		}
		if _, err := exposure.GetRawTCPListener("443/https"); err != nil {
			t.Fatalf("GetRawTCPListener returned an error: %v", err)
		}
		if _, err := exposure.GetListener("443/https"); !errors.Is(err, ErrProxyPortNotFound) {
			t.Errorf("GetListener error = %v, want ErrProxyPortNotFound", err)
		}
		return
	}

	wantTLSCalls := 1
	wantFunnelCalls := 0
	if wantFunnel {
		wantTLSCalls = 0
		wantFunnelCalls = 1
	}
	if server.listenCalls != 0 || server.listenTLSCalls != wantTLSCalls || server.funnelCalls != wantFunnelCalls {
		t.Errorf("listener calls = (%d, %d, %d), want (0, %d, %d)",
			server.listenCalls, server.listenTLSCalls, server.funnelCalls, wantTLSCalls, wantFunnelCalls)
	}
	if _, err := exposure.GetListener("443/https"); err != nil {
		t.Fatalf("GetListener returned an error: %v", err)
	}
	if _, err := exposure.GetRawTCPListener("443/https"); !errors.Is(err, ErrProxyPortNotFound) {
		t.Errorf("GetRawTCPListener error = %v, want ErrProxyPortNotFound", err)
	}
}

func TestPerProxyExposure_CustomTLSRejectsFunnel(t *testing.T) {
	t.Parallel()

	server := &exposureTestServer{listener: noopListener{}}
	exposure := NewPerProxyExposure(zerolog.Nop())
	cfg := &model.Config{
		Domain:              "app.example.com",
		ResolvedTLSProvider: model.TLSProviderACME,
		Ports: model.PortConfigList{
			"443/https": {
				ProxyProtocol: model.ProtoHTTPS,
				ProxyPort:     443,
				Tailscale: model.TailscalePort{
					Funnel: true,
				},
			},
		},
	}

	err := exposure.Start(context.Background(), &NodeRuntime{Server: server}, cfg)
	if err == nil {
		t.Fatal("Start returned nil, want custom TLS and Funnel error")
	}
	want := "custom TLS is incompatible with Tailscale Funnel for port \"443/https\""
	if err.Error() != want {
		t.Errorf("Start error = %q, want %q", err, want)
	}
	if server.listenCalls != 0 || server.listenTLSCalls != 0 || server.funnelCalls != 0 {
		t.Errorf("listener calls = (%d, %d, %d), want all zero", server.listenCalls, server.listenTLSCalls, server.funnelCalls)
	}
}

func TestPerProxyExposure_CustomTLSListenerError(t *testing.T) {
	t.Parallel()

	listenErr := errors.New("listen failed")
	server := &exposureTestServer{listenErr: listenErr}
	exposure := NewPerProxyExposure(zerolog.Nop())
	cfg := &model.Config{
		Domain:              "app.example.com",
		ResolvedTLSProvider: model.TLSProviderACME,
		Ports: model.PortConfigList{
			"443/https": {
				ProxyProtocol: model.ProtoHTTPS,
				ProxyPort:     443,
			},
		},
	}

	err := exposure.Start(context.Background(), &NodeRuntime{Server: server}, cfg)
	if !errors.Is(err, listenErr) {
		t.Fatalf("Start error = %v, want wrapped listener error", err)
	}
	want := "create custom-domain HTTPS listener for port \"443/https\": listen failed"
	if err.Error() != want {
		t.Errorf("Start error = %q, want %q", err, want)
	}
}

func TestPerProxyExposure_CloseClosesCustomTLSListener(t *testing.T) {
	t.Parallel()

	listener := &closeTrackingListener{}
	server := &exposureTestServer{listener: listener}
	exposure := NewPerProxyExposure(zerolog.Nop())
	cfg := &model.Config{
		Domain:              "app.example.com",
		ResolvedTLSProvider: model.TLSProviderACME,
		Ports: model.PortConfigList{
			"443/https": {
				ProxyProtocol: model.ProtoHTTPS,
				ProxyPort:     443,
			},
		},
	}

	if err := exposure.Start(context.Background(), &NodeRuntime{Server: server}, cfg); err != nil {
		t.Fatalf("Start returned an error: %v", err)
	}
	if err := exposure.Close(context.Background()); err != nil {
		t.Fatalf("Close returned an error: %v", err)
	}
	if !listener.closed {
		t.Error("Close did not close the custom TLS listener")
	}
}

// SharedSNIExposure

func TestNewSharedSNIExposure(t *testing.T) {
	t.Parallel()

	e := NewSharedSNIExposure(nil, "example.ts.net")
	if e == nil {
		t.Fatal("NewSharedSNIExposure returned nil")
	}
	if e.started {
		t.Fatal("new exposure should not be started")
	}
	if e.domain != "example.ts.net" {
		t.Errorf("domain = %q, want %q", e.domain, "example.ts.net")
	}
}

func TestSharedSNIExposure_GetListener_NotStarted(t *testing.T) {
	t.Parallel()

	e := NewSharedSNIExposure(nil, "example.ts.net")
	_, err := e.GetListener("https")
	if !errors.Is(err, errExposureNotStarted) {
		t.Errorf("expected errExposureNotStarted, got %v", err)
	}
}

func TestSharedSNIExposure_Close_Idempotent(t *testing.T) {
	t.Parallel()

	e := NewSharedSNIExposure(nil, "example.ts.net")
	if err := e.Close(context.Background()); err != nil {
		t.Fatalf("unexpected error on first close: %v", err)
	}
	if err := e.Close(context.Background()); err != nil {
		t.Fatalf("unexpected error on second close: %v", err)
	}
}

func TestSharedSNIExposure_Close_Started(t *testing.T) {
	t.Parallel()

	e := NewSharedSNIExposure(nil, "example.ts.net")
	e.mtx.Lock()
	e.started = true
	e.mtx.Unlock()

	if err := e.Close(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	e.mtx.RLock()
	started := e.started
	e.mtx.RUnlock()
	if started {
		t.Error("started should be false after Close")
	}
}

func TestSharedSNIExposure_getRawTCPListener_NotStarted(t *testing.T) {
	t.Parallel()

	e := NewSharedSNIExposure(nil, "example.ts.net")
	_, err := e.getRawTCPListener("tcp")
	if !errors.Is(err, errExposureNotStarted) {
		t.Errorf("expected errExposureNotStarted, got %v", err)
	}
}

func TestSharedSNIExposure_getRawTCPListener_NotFound(t *testing.T) {
	t.Parallel()

	e := NewSharedSNIExposure(nil, "example.ts.net")
	e.mtx.Lock()
	e.started = true
	e.mtx.Unlock()

	_, err := e.getRawTCPListener("nonexistent")
	if !errors.Is(err, ErrProxyPortNotFound) {
		t.Errorf("expected ErrProxyPortNotFound, got %v", err)
	}
}

func TestSharedSNIExposure_getPacketConn_NotStarted(t *testing.T) {
	t.Parallel()

	e := NewSharedSNIExposure(nil, "example.ts.net")
	_, err := e.getPacketConn("udp")
	if !errors.Is(err, errExposureNotStarted) {
		t.Errorf("expected errExposureNotStarted, got %v", err)
	}
}

func TestSharedSNIExposure_getPacketConn_NotFound(t *testing.T) {
	t.Parallel()

	e := NewSharedSNIExposure(nil, "example.ts.net")
	e.mtx.Lock()
	e.started = true
	e.mtx.Unlock()

	_, err := e.getPacketConn("nonexistent")
	if !errors.Is(err, ErrProxyPortNotFound) {
		t.Errorf("expected ErrProxyPortNotFound, got %v", err)
	}
}

// ServicesVIPExposure

func TestNewServicesVIPExposure(t *testing.T) {
	t.Parallel()

	e := NewServicesVIPExposure(nil, "myservice")
	if e == nil {
		t.Fatal("NewServicesVIPExposure returned nil")
	}
	if e.started {
		t.Fatal("new exposure should not be started")
	}
	if e.serviceName != "myservice" {
		t.Errorf("serviceName = %q, want %q", e.serviceName, "myservice")
	}
}

func TestServicesVIPExposure_GetListener_NotStarted(t *testing.T) {
	t.Parallel()

	e := NewServicesVIPExposure(nil, "myservice")
	_, err := e.GetListener("https")
	if !errors.Is(err, errExposureNotStarted) {
		t.Errorf("expected errExposureNotStarted, got %v", err)
	}
}

func TestServicesVIPExposure_Close_Idempotent(t *testing.T) {
	t.Parallel()

	e := NewServicesVIPExposure(nil, "myservice")
	if err := e.Close(context.Background()); err != nil {
		t.Fatalf("unexpected error on first close: %v", err)
	}
	if err := e.Close(context.Background()); err != nil {
		t.Fatalf("unexpected error on second close: %v", err)
	}
}

func TestServicesVIPExposure_Close_Started(t *testing.T) {
	t.Parallel()

	e := NewServicesVIPExposure(nil, "myservice")
	e.mtx.Lock()
	e.started = true
	e.mtx.Unlock()

	if err := e.Close(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	e.mtx.RLock()
	started := e.started
	e.mtx.RUnlock()
	if started {
		t.Error("started should be false after Close")
	}
}

func TestServicesVIPExposure_RollbackAcquired_NilCfg(t *testing.T) {
	t.Parallel()

	e := NewServicesVIPExposure(nil, "myservice")
	e.rollbackAcquired()
}

func TestServicesVIPExposure_FirstFQDN_Empty(t *testing.T) {
	t.Parallel()

	e := NewServicesVIPExposure(nil, "myservice")
	if fqdn := e.firstFQDN(); fqdn != "" {
		t.Errorf("firstFQDN = %q, want empty", fqdn)
	}
}
