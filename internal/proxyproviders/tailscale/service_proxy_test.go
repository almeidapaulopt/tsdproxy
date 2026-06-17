// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package tailscale

import (
	"context"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"golang.org/x/sync/semaphore"

	"github.com/almeidapaulopt/tsdproxy/internal/model"
)

func TestServiceProxyGetAuthURLReturnsEmpty(t *testing.T) {
	t.Parallel()

	// ServiceProxy with nil services server returns empty.
	p := &ServiceProxy{}

	if url := p.GetAuthURL(); url != "" {
		t.Fatalf("GetAuthURL should return empty string with nil services, got %q", url)
	}
}

func TestServiceProxyGetAuthURLDelegatesToServer(t *testing.T) {
	ss := NewServicesServer(ServicesServerConfig{
		Hostname: "test-server",
		Log:      zerolog.Nop(),
	})
	defer ss.Close()

	// Send auth URL via command channel.
	ss.ev.SendCmd(servicesWatchUpdateCmd{authURL: "https://login.tailscale.com/a/xyz789"})

	p := &ServiceProxy{
		services: ss,
	}

	if url := p.GetAuthURL(); url != "https://login.tailscale.com/a/xyz789" {
		t.Fatalf("GetAuthURL should return auth URL from services server, got %q", url)
	}
}

func TestServiceProxyGetPacketConnReturnsError(t *testing.T) {
	t.Parallel()

	p := &ServiceProxy{}

	_, err := p.GetPacketConn("1")
	if err == nil {
		t.Fatal("expected error from GetPacketConn")
	}
	if err.Error() == "" {
		t.Fatal("error message should not be empty")
	}
}

func TestServiceProxyWhoisReturnsEmpty(t *testing.T) {
	ss := NewServicesServer(ServicesServerConfig{
		Hostname: "test-server",
		Log:      zerolog.Nop(),
	})
	defer ss.Close()

	p := &ServiceProxy{
		services: ss,
	}

	whois := p.Whois(nil)
	if whois != (model.Whois{}) {
		t.Fatalf("expected empty Whois, got %+v", whois)
	}
}

func TestServiceProxyGetURLReturnsEmptyWhenNotStarted(t *testing.T) {
	t.Parallel()

	p := &ServiceProxy{
		fqdn: "",
	}

	if url := p.GetURL(); url != "" {
		t.Fatalf("GetURL should return empty when fqdn is empty, got %q", url)
	}
}

func TestServiceProxyGetURLWithHTTPS(t *testing.T) {
	t.Parallel()

	p := &ServiceProxy{
		fqdn: "test.tailnet.ts.net",
		config: &model.Config{
			Ports: map[string]model.PortConfig{
				"1": {ProxyProtocol: model.ProtoHTTPS, ProxyPort: 443},
			},
		},
	}

	want := "https://test.tailnet.ts.net"
	if got := p.GetURL(); got != want {
		t.Fatalf("GetURL() = %q, want %q", got, want)
	}
}

func TestServiceProxyGetURLWithHTTPOnly(t *testing.T) {
	t.Parallel()

	p := &ServiceProxy{
		fqdn: "test.tailnet.ts.net",
		config: &model.Config{
			Ports: map[string]model.PortConfig{
				"1": {ProxyProtocol: model.ProtoHTTP, ProxyPort: 80},
			},
		},
	}

	want := "http://test.tailnet.ts.net"
	if got := p.GetURL(); got != want {
		t.Fatalf("GetURL() = %q, want %q", got, want)
	}
}

func TestServiceProxyPrimaryScheme(t *testing.T) {
	tests := []struct {
		name  string
		ports map[string]model.PortConfig
		want  string
	}{
		{
			name: "https and http returns https",
			ports: map[string]model.PortConfig{
				"1": {ProxyProtocol: model.ProtoHTTPS, ProxyPort: 443},
				"2": {ProxyProtocol: model.ProtoHTTP, ProxyPort: 80},
			},
			want: "https",
		},
		{
			name: "http only returns http",
			ports: map[string]model.PortConfig{
				"1": {ProxyProtocol: model.ProtoHTTP, ProxyPort: 80},
			},
			want: "http",
		},
		{
			name: "tcp and http returns http",
			ports: map[string]model.PortConfig{
				"1": {ProxyProtocol: model.ProtoTCP, ProxyPort: 22},
				"2": {ProxyProtocol: model.ProtoHTTP, ProxyPort: 80},
			},
			want: "http",
		},
		{
			name: "tcp only returns tcp",
			ports: map[string]model.PortConfig{
				"1": {ProxyProtocol: model.ProtoTCP, ProxyPort: 22},
			},
			want: "tcp",
		},
		{
			name:  "empty ports returns https default",
			ports: map[string]model.PortConfig{},
			want:  "https",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := primaryScheme(tt.ports); got != tt.want {
				t.Fatalf("primaryScheme() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestServiceProxyGetListenerNotFound(t *testing.T) {
	t.Parallel()

	p := &ServiceProxy{
		config:   &model.Config{},
		exposure: NewServicesVIPExposure(nil, "svc:test"),
	}

	_, err := p.GetListener("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent listener")
	}
}

func TestNewServiceProxyServiceNameFormat(t *testing.T) {
	c := &Client{
		log:            zerolog.Nop(),
		services:       true,
		sharedHostname: "test-services",
		datadir:        t.TempDir(),
		certSem:        semaphore.NewWeighted(1),
	}

	cfg := &model.Config{
		Hostname: "myapp",
		Tailscale: model.Tailscale{
			ResolvedAuthKey: "test-key",
		},
		Ports: map[string]model.PortConfig{
			"1": {ProxyProtocol: model.ProtoHTTPS, ProxyPort: 443},
		},
	}

	proxy, err := c.newServiceProxy(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	sp, ok := proxy.(*ServiceProxy)
	if !ok {
		t.Fatal("expected *ServiceProxy")
	}

	if sp.serviceName != "svc:myapp" {
		t.Fatalf("serviceName = %q, want %q", sp.serviceName, "svc:myapp")
	}

	// Clean up the services server.
	c.sharedMu.Lock()
	if c.servicesServer != nil {
		c.servicesServer.Close()
		c.servicesServer = nil
	}
	c.sharedMu.Unlock()
}

// blockingServiceExposure simulates a stuck exposure.Start() (tsnet auth retries).
type blockingServiceExposure struct {
	blockCh      chan struct{}
	startCalled  atomic.Bool
	fqdnToReturn string
}

func (m *blockingServiceExposure) Start(_ context.Context, _ *NodeRuntime, _ *model.Config) error {
	m.startCalled.Store(true)
	<-m.blockCh
	return nil
}

func (m *blockingServiceExposure) Close(context.Context) error { return nil }

func (m *blockingServiceExposure) firstFQDN() string { return m.fqdnToReturn }

func (m *blockingServiceExposure) GetListener(string) (net.Listener, error) {
	return nil, nil
}

// TestServiceProxy_GetURL_MustNotBlockDuringStart proves the dashboard-hang
// bug: ServiceProxy.Start() must NOT hold p.mtx (write lock) during the
// blocking exposure.Start() call. GetURL() needs RLock and is called per-proxy
// on every dashboard request, so one stuck Services-mode proxy hangs the entire
// dashboard. Confirmed in production via goroutine dump (35 readers parked at
// ServiceProxy.GetURL RLock).
func TestServiceProxy_GetURL_MustNotBlockDuringStart(t *testing.T) {
	t.Parallel()

	ss := NewServicesServer(ServicesServerConfig{
		Hostname: "test-url",
		Log:      zerolog.Nop(),
	})
	defer ss.Close()

	mock := &blockingServiceExposure{
		blockCh:      make(chan struct{}),
		fqdnToReturn: "test.tailnet.ts.net",
	}
	p := &ServiceProxy{
		log:      zerolog.Nop(),
		services: ss,
		config: &model.Config{
			Ports: map[string]model.PortConfig{
				"1": {ProxyProtocol: model.ProtoHTTPS, ProxyPort: 443},
			},
		},
		exposure: mock,
		events:   make(chan model.ProxyEvent, 1),
	}

	startDone := make(chan error, 1)
	go func() {
		startDone <- p.Start(context.Background())
	}()

	waitFor(t, func() bool { return mock.startCalled.Load() }, "Start() should call exposure.Start()")

	done := make(chan string, 1)
	go func() {
		done <- p.GetURL()
	}()

	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("GetURL() blocked for >500ms while Start() is in progress — " +
			"root cause of dashboard hangs when Services-mode proxies are stuck " +
			"during startup")
	}

	close(mock.blockCh)
	<-startDone
}

func TestServiceProxy_GetListener_MustNotBlockDuringStart(t *testing.T) {
	t.Parallel()

	ss := NewServicesServer(ServicesServerConfig{
		Hostname: "test-listener",
		Log:      zerolog.Nop(),
	})
	defer ss.Close()

	mock := &blockingServiceExposure{
		blockCh:      make(chan struct{}),
		fqdnToReturn: "test.tailnet.ts.net",
	}
	p := &ServiceProxy{
		log:      zerolog.Nop(),
		services: ss,
		config: &model.Config{
			Ports: map[string]model.PortConfig{
				"1": {ProxyProtocol: model.ProtoHTTPS, ProxyPort: 443},
			},
		},
		exposure: mock,
		events:   make(chan model.ProxyEvent, 1),
	}

	startDone := make(chan error, 1)
	go func() {
		startDone <- p.Start(context.Background())
	}()

	waitFor(t, func() bool { return mock.startCalled.Load() }, "Start() should call exposure.Start()")

	done := make(chan struct{})
	go func() {
		_, _ = p.GetListener("1") //nolint:errcheck // return value irrelevant
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("GetListener() blocked for >500ms while Start() is in progress — " +
			"same root cause as GetURL")
	}

	close(mock.blockCh)
	<-startDone
}

func waitFor(t *testing.T, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("condition not met within 2s: %s", msg)
}
