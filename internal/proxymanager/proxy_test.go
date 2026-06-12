// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package proxymanager

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"tailscale.com/client/local"

	"github.com/almeidapaulopt/tsdproxy/internal/core/metrics"
	"github.com/almeidapaulopt/tsdproxy/internal/dnsproviders"
	"github.com/almeidapaulopt/tsdproxy/internal/model"
	"github.com/almeidapaulopt/tsdproxy/internal/proxyproviders"
	"github.com/almeidapaulopt/tsdproxy/internal/tlsproviders"
)

type stubProviderProxy struct {
	startErr    error
	closeErr    error
	listener    net.Listener
	listenerErr error
	packetConn  net.PacketConn
	packetErr   error
	eventsCh    chan model.ProxyEvent
	whoisFunc   func(*http.Request) model.Whois
	url         string
	authURL     string
	mtx         sync.Mutex
	closed      bool
}

var _ proxyproviders.ProxyInterface = (*stubProviderProxy)(nil)

func (s *stubProviderProxy) Start(_ context.Context) error { return s.startErr }

func (s *stubProviderProxy) Close() error {
	s.mtx.Lock()
	s.closed = true
	s.mtx.Unlock()
	return s.closeErr
}

func (s *stubProviderProxy) GetListener(_ string) (net.Listener, error) {
	return s.listener, s.listenerErr
}

func (s *stubProviderProxy) GetPacketConn(_ string) (net.PacketConn, error) {
	return s.packetConn, s.packetErr
}

func (s *stubProviderProxy) GetURL() string { return s.url }

func (s *stubProviderProxy) GetAuthURL() string { return s.authURL }

func (s *stubProviderProxy) WatchEvents() chan model.ProxyEvent { return s.eventsCh }

func (s *stubProviderProxy) Whois(r *http.Request) model.Whois {
	if s.whoisFunc != nil {
		return s.whoisFunc(r)
	}
	return model.Whois{}
}

type stubRawTCPProviderProxy struct {
	rawListener    net.Listener
	rawListenerErr error
	stubProviderProxy
	rawCalled bool
}

var _ proxyproviders.RawTCPListener = (*stubRawTCPProviderProxy)(nil)

func (s *stubRawTCPProviderProxy) GetRawTCPListener(_ string) (net.Listener, error) {
	s.rawCalled = true
	return s.rawListener, s.rawListenerErr
}

type stubLocalClientProvider struct {
	localClient *local.Client
	stubProviderProxy
}

func (s *stubLocalClientProvider) GetLocalClient() *local.Client {
	return s.localClient
}

type stubLocalClientRawTCPProvider struct { //nolint:unused
	localClient *local.Client
	stubRawTCPProviderProxy
}

func (s *stubLocalClientRawTCPProvider) GetLocalClient() *local.Client { //nolint:unused
	return s.localClient
}

type stubPortHandler struct {
	closeErr            error
	startedWithListener atomic.Bool
	closed              atomic.Bool
}

var _ portHandler = (*stubPortHandler)(nil)

func (s *stubPortHandler) startWithListener(_ net.Listener) error {
	s.startedWithListener.Store(true)
	return nil
}

func (s *stubPortHandler) close() error {
	s.closed.Store(true)
	return s.closeErr
}

type stubProxyProvider struct {
	failNewProxy bool
}

var _ proxyproviders.Provider = (*stubProxyProvider)(nil)

func (s *stubProxyProvider) ResolveAuthKey(_ *model.Config) (string, error) {
	return "tskey-stub", nil
}

func (s *stubProxyProvider) NewProxy(_ *model.Config) (proxyproviders.ProxyInterface, error) {
	if s.failNewProxy {
		return nil, errors.New("stub: NewProxy failed")
	}
	return &stubProviderProxy{}, nil
}

func TestGetListenerForPort_TCP_UsesRawTCPListener(t *testing.T) {
	t.Parallel()

	expected, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = expected.Close() })

	stub := &stubRawTCPProviderProxy{
		stubProviderProxy: stubProviderProxy{},
		rawListener:       expected,
	}

	proxy := &Proxy{
		log:           zerolog.Nop(),
		Config:        &model.Config{},
		providerProxy: stub,
	}

	got, err := proxy.getListenerForPort("port-tcp", model.PortConfig{
		ProxyProtocol: model.ProtoTCP,
	})

	require.NoError(t, err)
	assert.True(t, stub.rawCalled, "TCP port should call GetRawTCPListener")
	assert.Same(t, expected, got)
}

func TestGetListenerForPort_TCP_FallsBackWhenNotRawTCP(t *testing.T) {
	t.Parallel()

	expected, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = expected.Close() })

	stub := &stubProviderProxy{
		listener: expected,
	}

	proxy := &Proxy{
		log:           zerolog.Nop(),
		Config:        &model.Config{},
		providerProxy: stub,
	}

	got, err := proxy.getListenerForPort("port-tcp", model.PortConfig{
		ProxyProtocol: model.ProtoTCP,
	})

	require.NoError(t, err)
	assert.Same(t, expected, got)
}

func TestGetListenerForPort_HTTPS_UsesGetListener(t *testing.T) {
	t.Parallel()

	expected, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = expected.Close() })

	stub := &stubRawTCPProviderProxy{
		stubProviderProxy: stubProviderProxy{},
		rawListener:       nil,
	}
	stub.listener = expected

	proxy := &Proxy{
		log:           zerolog.Nop(),
		Config:        &model.Config{},
		providerProxy: stub,
	}

	got, err := proxy.getListenerForPort("port-https", model.PortConfig{
		ProxyProtocol: model.ProtoHTTPS,
	})

	require.NoError(t, err)
	assert.False(t, stub.rawCalled, "HTTPS port must not call GetRawTCPListener")
	assert.Same(t, expected, got)
}

func TestGetListenerForPort_CustomDomainHTTPS_UsesCustomTLS(t *testing.T) {
	t.Parallel()

	rawLn, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = rawLn.Close() })

	stub := &stubRawTCPProviderProxy{
		rawListener: rawLn,
	}

	proxy := &Proxy{
		log:           zerolog.Nop(),
		Config:        &model.Config{Domain: "app.example.com"},
		providerProxy: stub,
		tlsProvider:   &mockTLSProvider{name: "acme"},
	}

	got, err := proxy.getListenerForPort("port-https", model.PortConfig{
		ProxyProtocol: model.ProtoHTTPS,
	})
	require.NoError(t, err)
	assert.NotNil(t, got)
}

func TestStartProvider_NoPortsReturnsError(t *testing.T) {
	t.Parallel()
	proxy := &Proxy{
		log:           zerolog.Nop(),
		Config:        &model.Config{},
		providerProxy: &stubProviderProxy{},
		ports:         make(map[string]portHandler),
	}
	err := proxy.startProvider()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no ports configured")
}

func TestStartProvider_ProviderStartError(t *testing.T) {
	t.Parallel()
	proxy := &Proxy{
		log:           zerolog.Nop(),
		Config:        &model.Config{},
		providerProxy: &stubProviderProxy{startErr: errors.New("start failed")},
		ports: map[string]portHandler{
			"1": &stubPortHandler{},
		},
	}
	err := proxy.startProvider()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "start failed")
}

func TestStartProvider_Success(t *testing.T) {
	t.Parallel()
	proxy := &Proxy{
		log:           zerolog.Nop(),
		Config:        &model.Config{},
		providerProxy: &stubProviderProxy{},
		ports: map[string]portHandler{
			"1": &stubPortHandler{},
		},
	}
	err := proxy.startProvider()
	require.NoError(t, err)
}

func TestStartListeners_AllFail(t *testing.T) {
	t.Parallel()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	ln.Close()

	proxy := &Proxy{
		log:           zerolog.Nop(),
		Config:        &model.Config{},
		providerProxy: &stubProviderProxy{listenerErr: errors.New("listener error")},
		ports:         make(map[string]portHandler),
	}
	portCfg := model.PortConfig{ProxyProtocol: model.ProtoTCP}
	portCfg.AddTarget(&url.URL{Scheme: "tcp", Host: "127.0.0.1:1"})
	proxy.Config.Ports = map[string]model.PortConfig{"1": portCfg}
	proxy.ports["1"] = &stubPortHandler{}

	err = proxy.startListeners()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "all 1 listeners failed")
}

func TestStartListeners_PartialFailure(t *testing.T) {
	t.Parallel()
	pcConn, err := net.ListenPacket("udp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = pcConn.Close() })

	proxy := &Proxy{
		log:    zerolog.Nop(),
		Config: &model.Config{},
		providerProxy: &stubProviderProxy{
			packetConn:  pcConn,
			listenerErr: errors.New("bad listener"),
		},
		ports: make(map[string]portHandler),
	}
	udpCfg := model.PortConfig{ProxyProtocol: model.ProtoUDP}
	udpCfg.AddTarget(&url.URL{Scheme: "udp", Host: "127.0.0.1:1"})
	tcpCfg := model.PortConfig{ProxyProtocol: model.ProtoTCP}
	tcpCfg.AddTarget(&url.URL{Scheme: "tcp", Host: "127.0.0.1:2"})
	proxy.Config.Ports = map[string]model.PortConfig{"good": udpCfg, "bad": tcpCfg}
	proxy.ports["good"] = &stubPortHandler{}
	proxy.ports["bad"] = &stubPortHandler{}

	err = proxy.startListeners()
	require.NoError(t, err)
}

func TestStartListeners_UDPGetsPacketConn(t *testing.T) {
	t.Parallel()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = pc.Close() })

	proxy := &Proxy{
		log:           zerolog.Nop(),
		Config:        &model.Config{},
		providerProxy: &stubProviderProxy{packetConn: pc},
		ports:         make(map[string]portHandler),
	}
	portCfg := model.PortConfig{ProxyProtocol: model.ProtoUDP}
	portCfg.AddTarget(&url.URL{Scheme: "udp", Host: "127.0.0.1:1"})
	proxy.Config.Ports = map[string]model.PortConfig{"udp1": portCfg}
	proxy.ports["udp1"] = &stubPortHandler{}

	err = proxy.startListeners()
	require.NoError(t, err)
}

func TestStartListeners_UDPPacketConnError(t *testing.T) {
	t.Parallel()
	proxy := &Proxy{
		log:           zerolog.Nop(),
		Config:        &model.Config{},
		providerProxy: &stubProviderProxy{packetErr: errors.New("no udp")},
		ports:         make(map[string]portHandler),
	}
	portCfg := model.PortConfig{ProxyProtocol: model.ProtoUDP}
	portCfg.AddTarget(&url.URL{Scheme: "udp", Host: "127.0.0.1:1"})
	proxy.Config.Ports = map[string]model.PortConfig{"udp1": portCfg}
	proxy.ports["udp1"] = &stubPortHandler{}

	err := proxy.startListeners()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "all 1 listeners failed")
}

func TestStartPort_StartsHandler(t *testing.T) {
	t.Parallel()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })

	sp := &stubPortHandler{}
	proxy := &Proxy{
		log:    zerolog.Nop(),
		Config: &model.Config{},
		ports:  map[string]portHandler{"p1": sp},
	}

	proxy.startPort("p1", ln)
	require.Eventually(t, func() bool { return sp.startedWithListener.Load() }, time.Second, 10*time.Millisecond)
}

func TestStartPort_MissingPort(t *testing.T) {
	t.Parallel()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	ln.Close()

	proxy := &Proxy{
		log:    zerolog.Nop(),
		Config: &model.Config{},
		ports:  make(map[string]portHandler),
	}
	proxy.startPort("nonexistent", ln)
}

func TestStartPacketPort_StartsUDP(t *testing.T) {
	t.Parallel()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = pc.Close() })

	up := newPortUDP(context.Background(), model.PortConfig{}, zerolog.Nop())
	proxy := &Proxy{
		log:    zerolog.Nop(),
		Config: &model.Config{},
		ports:  map[string]portHandler{"u1": up},
	}

	proxy.startPacketPort("u1", pc)
}

func TestStartPacketPort_MissingPort(t *testing.T) {
	t.Parallel()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	require.NoError(t, err)
	defer pc.Close()

	proxy := &Proxy{
		log:    zerolog.Nop(),
		Config: &model.Config{},
		ports:  make(map[string]portHandler),
	}
	proxy.startPacketPort("nonexistent", pc)
	_, err = pc.WriteTo([]byte("x"), nil)
	require.Error(t, err)
}

func TestStartPacketPort_WrongPortType(t *testing.T) {
	t.Parallel()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	require.NoError(t, err)
	defer pc.Close()

	tcpHandler := newPortTCP(context.Background(), model.PortConfig{}, zerolog.Nop())
	proxy := &Proxy{
		log:    zerolog.Nop(),
		Config: &model.Config{},
		ports:  map[string]portHandler{"not-udp": tcpHandler},
	}
	proxy.startPacketPort("not-udp", pc)
	_, err = pc.WriteTo([]byte("x"), nil)
	require.Error(t, err)
}

func TestNewProxy_ProviderError(t *testing.T) {
	t.Parallel()
	failingProvider := &stubProxyProvider{failNewProxy: true}

	_, err := NewProxy(ProxyParams{
		Log:            zerolog.Nop(),
		Config:         &model.Config{Hostname: "test"},
		ProxyProvider:  failingProvider,
		ProxyAuthToken: "test-token",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "error initializing proxy on proxyProvider")
}

func TestNewProxy_WithAccessLog(t *testing.T) {
	t.Parallel()
	proxy, err := NewProxy(ProxyParams{
		Log: zerolog.Nop(),
		Config: &model.Config{
			Hostname:       "test",
			ProxyAccessLog: true,
		},
		ProxyProvider:  &stubProxyProvider{},
		ProxyAuthToken: "test-token",
	})
	require.NoError(t, err)
	require.NotNil(t, proxy)
	assert.NotNil(t, proxy.logBuffer)
	assert.Equal(t, "test", proxy.Config.Hostname)
}

func TestClosePorts_ClosesAllPorts(t *testing.T) {
	t.Parallel()
	sp := &stubPortHandler{}
	proxy := &Proxy{
		log:    zerolog.Nop(),
		Config: &model.Config{Hostname: "test"},
		ports:  map[string]portHandler{"p1": sp},
	}
	proxy.closePorts()
	assert.True(t, sp.closed.Load())
	assert.Empty(t, proxy.ports)
}

func TestClose_ClosesProviderAndPorts(t *testing.T) {
	t.Parallel()
	stub := &stubProviderProxy{}
	proxy := &Proxy{
		log:           zerolog.Nop(),
		Config:        &model.Config{Hostname: "test"},
		providerProxy: stub,
		ports:         map[string]portHandler{},
		statusHistory: make([]StatusTransition, 0, 5),
	}
	proxy.close()
	assert.True(t, stub.closed)
}

func TestClose_SetsStoppedStatus(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	proxy := &Proxy{
		log:           zerolog.Nop(),
		Config:        &model.Config{Hostname: "test"},
		providerProxy: &stubProviderProxy{},
		status:        model.ProxyStatusRunning,
		ports:         make(map[string]portHandler),
		statusHistory: make([]StatusTransition, 0, 5),
		ctx:           ctx,
		cancel:        cancel,
	}
	proxy.Close()
	assert.Equal(t, model.ProxyStatusStopped, proxy.status)
}

func TestSetStatus_RecordsHistory(t *testing.T) {
	t.Parallel()
	proxy := &Proxy{
		log:           zerolog.Nop(),
		Config:        &model.Config{Hostname: "test"},
		statusHistory: make([]StatusTransition, 0, maxStatusHistory),
	}
	proxy.setStatus(model.ProxyStatusRunning)
	assert.Equal(t, model.ProxyStatusRunning, proxy.GetStatus())

	history := proxy.GetStatusHistory()
	assert.Len(t, history, 1)
	assert.Equal(t, model.ProxyStatusRunning, history[0].Status)
}

func TestSetStatus_Deduplicates(t *testing.T) {
	t.Parallel()
	proxy := &Proxy{
		log:           zerolog.Nop(),
		Config:        &model.Config{Hostname: "test"},
		status:        model.ProxyStatusRunning,
		statusHistory: make([]StatusTransition, 0, maxStatusHistory),
	}
	proxy.setStatus(model.ProxyStatusRunning)
	history := proxy.GetStatusHistory()
	assert.Empty(t, history, "no new history entry for same status")
}

func TestSetStatus_CallsOnUpdate(t *testing.T) {
	t.Parallel()
	var got model.ProxyEvent
	proxy := &Proxy{
		log:           zerolog.Nop(),
		Config:        &model.Config{Hostname: "test"},
		statusHistory: make([]StatusTransition, 0, maxStatusHistory),
		onUpdate: func(event model.ProxyEvent) {
			got = event
		},
	}
	proxy.setStatus(model.ProxyStatusRunning)
	assert.Equal(t, "test", got.ID)
	assert.Equal(t, model.ProxyStatusInitializing, got.OldStatus)
}

func TestSetStatus_BlockedWhenPaused(t *testing.T) {
	t.Parallel()
	proxy := &Proxy{
		log:           zerolog.Nop(),
		Config:        &model.Config{Hostname: "test"},
		status:        model.ProxyStatusPaused,
		paused:        true,
		statusHistory: make([]StatusTransition, 0, maxStatusHistory),
	}
	proxy.setStatus(model.ProxyStatusRunning)
	assert.Equal(t, model.ProxyStatusPaused, proxy.GetStatus())
}

func TestSetStatus_PausedTransitionAllowed(t *testing.T) {
	t.Parallel()
	proxy := &Proxy{
		log:           zerolog.Nop(),
		Config:        &model.Config{Hostname: "test"},
		paused:        true,
		statusHistory: make([]StatusTransition, 0, maxStatusHistory),
	}
	proxy.setStatus(model.ProxyStatusPaused)
	assert.Equal(t, model.ProxyStatusPaused, proxy.GetStatus())
}

func TestSetStatus_WithMetrics(t *testing.T) {
	reg := prometheus.NewRegistry()
	oldReg := prometheus.DefaultRegisterer
	oldGatherer := prometheus.DefaultGatherer
	prometheus.DefaultRegisterer = reg
	prometheus.DefaultGatherer = reg
	t.Cleanup(func() {
		prometheus.DefaultRegisterer = oldReg
		prometheus.DefaultGatherer = oldGatherer
	})

	m := metrics.New(nil)
	proxy := &Proxy{
		log:           zerolog.Nop(),
		Config:        &model.Config{Hostname: "test"},
		statusHistory: make([]StatusTransition, 0, maxStatusHistory),
		metrics:       m,
		metricsReady:  true,
	}
	proxy.setStatus(model.ProxyStatusRunning)
	assert.Equal(t, model.ProxyStatusRunning, proxy.GetStatus())
}

func TestGetURL_WithDomainAndActiveTLS(t *testing.T) {
	t.Parallel()
	proxy := &Proxy{
		log:       zerolog.Nop(),
		Config:    &model.Config{Domain: "app.example.com"},
		tlsStatus: tlsproviders.TLSStatusActive,
	}
	url := proxy.GetURL()
	assert.Equal(t, "https://app.example.com", url)
}

func TestGetURL_FallsBackToProvider(t *testing.T) {
	t.Parallel()
	proxy := &Proxy{
		log:           zerolog.Nop(),
		Config:        &model.Config{},
		providerProxy: &stubProviderProxy{url: "https://test.tailnet.ts.net"},
	}
	url := proxy.GetURL()
	assert.Equal(t, "https://test.tailnet.ts.net", url)
}

func TestGetAuthURL_Delegates(t *testing.T) {
	t.Parallel()
	proxy := &Proxy{
		log:           zerolog.Nop(),
		Config:        &model.Config{},
		providerProxy: &stubProviderProxy{authURL: "https://login.tailscale.com"},
	}
	assert.Equal(t, "https://login.tailscale.com", proxy.GetAuthURL())
}

func TestDNSAndTLSStatus_DefaultNone(t *testing.T) {
	t.Parallel()
	proxy := &Proxy{}
	assert.Equal(t, dnsproviders.DNSStatusNone, proxy.GetDNSStatus())
	assert.Equal(t, tlsproviders.TLSStatusNone, proxy.GetTLSStatus())
}

func TestSetDNSAndTLSProviders(t *testing.T) {
	t.Parallel()
	dnsMock := &mockDNSProvider{}
	tlsMock := &mockTLSProvider{}
	proxy := &Proxy{}
	proxy.SetDNSAndTLSProviders(dnsMock, tlsMock)

	proxy.mtx.RLock()
	assert.Same(t, dnsMock, proxy.dnsProvider)
	assert.Same(t, tlsMock, proxy.tlsProvider)
	proxy.mtx.RUnlock()
}

func TestSetDNSStatus(t *testing.T) {
	t.Parallel()
	proxy := &Proxy{}
	proxy.setDNSStatus(dnsproviders.DNSStatusActive)
	assert.Equal(t, dnsproviders.DNSStatusActive, proxy.GetDNSStatus())
}

func TestSetTLSStatus(t *testing.T) {
	t.Parallel()
	proxy := &Proxy{}
	proxy.setTLSStatus(tlsproviders.TLSStatusActive)
	assert.Equal(t, tlsproviders.TLSStatusActive, proxy.GetTLSStatus())
}

func TestGetHealth_NoHealthChecker(t *testing.T) {
	t.Parallel()
	proxy := &Proxy{}
	health := proxy.GetHealth()
	assert.Equal(t, HealthUnknown, health.Status)
}

func TestGetHealth_WithHealthChecker(t *testing.T) {
	t.Parallel()
	hc := newHealthChecker(zerolog.Nop(), "127.0.0.1:1", "tcp", time.Hour, 3, 0, true, nil)
	proxy := &Proxy{health: hc}
	defer hc.stop()
	health := proxy.GetHealth()
	assert.Equal(t, HealthUnknown, health.Status)
}

func TestGetStatusHistory_ReturnsCopy(t *testing.T) {
	t.Parallel()
	proxy := &Proxy{
		log:           zerolog.Nop(),
		Config:        &model.Config{},
		statusHistory: make([]StatusTransition, 0, maxStatusHistory),
	}
	proxy.setStatus(model.ProxyStatusRunning)
	proxy.setStatus(model.ProxyStatusError)

	history := proxy.GetStatusHistory()
	assert.Len(t, history, 2)
	_ = history[:0] //nolint:staticcheck // SA4006: intentional — verify returned slice is a copy
	assert.Len(t, proxy.GetStatusHistory(), 2)
}

func TestGetUptime_ZeroWhenNotStarted(t *testing.T) {
	t.Parallel()
	proxy := &Proxy{}
	assert.Equal(t, time.Duration(0), proxy.GetUptime())
}

func TestGetUptime_NonZeroAfterStart(t *testing.T) {
	t.Parallel()
	proxy := &Proxy{startedAt: time.Now().Add(-10 * time.Second)}
	uptime := proxy.GetUptime()
	assert.Greater(t, uptime, 5*time.Second)
}

func TestSubscribeLogs_Disabled(t *testing.T) {
	t.Parallel()
	proxy := &Proxy{}
	snapshot, ch := proxy.SubscribeLogs()
	assert.Nil(t, snapshot)
	assert.Nil(t, ch)
}

func TestSubscribeLogs_Enabled(t *testing.T) {
	t.Parallel()
	buf := NewLogRingBuffer(zerolog.Nop(), 10)
	_, _ = buf.Write([]byte("line1"))
	proxy := &Proxy{logBuffer: buf}

	snapshot, ch := proxy.SubscribeLogs()
	assert.Contains(t, snapshot, "line1")
	assert.NotNil(t, ch)
}

func TestUnsubscribeLogs_NilChannelNoPanic(t *testing.T) {
	t.Parallel()
	proxy := &Proxy{}
	proxy.UnsubscribeLogs(nil)
}

func TestUnsubscribeLogs_WithoutBufferNoPanic(t *testing.T) {
	t.Parallel()
	proxy := &Proxy{}
	proxy.UnsubscribeLogs(make(chan string))
}

func TestStartHealthChecker_Disabled(t *testing.T) {
	t.Parallel()
	proxy := &Proxy{
		log:    zerolog.Nop(),
		Config: &model.Config{HealthCheckEnabled: false},
	}
	proxy.startHealthChecker()
	proxy.mtx.RLock()
	assert.Nil(t, proxy.health)
	proxy.mtx.RUnlock()
}

func TestStartHealthChecker_PicksFirstNonRedirectPort(t *testing.T) {
	t.Parallel()
	proxy := &Proxy{
		log: zerolog.Nop(),
		Config: &model.Config{
			HealthCheckEnabled:  true,
			HealthCheckInterval: 30,
			HealthCheckCooldown: 10,
			HealthCheckFailures: 3,
			Ports: map[string]model.PortConfig{
				"a": {ProxyProtocol: model.ProtoHTTP, IsRedirect: true},
				"b": {ProxyProtocol: model.ProtoHTTP},
			},
		},
	}
	pc := proxy.Config.Ports["b"]
	pc.AddTarget(&url.URL{Scheme: "http", Host: "127.0.0.1:8080"})
	proxy.Config.Ports["b"] = pc

	proxy.startHealthChecker()
	defer proxy.stopHealthChecker()

	proxy.mtx.RLock()
	assert.NotNil(t, proxy.health)
	assert.Equal(t, "b", proxy.healthPortName)
	proxy.mtx.RUnlock()
}

func TestStartHealthChecker_SkipNoTarget(t *testing.T) {
	t.Parallel()
	proxy := &Proxy{
		log: zerolog.Nop(),
		Config: &model.Config{
			HealthCheckEnabled: true,
			Ports: map[string]model.PortConfig{
				"a": {ProxyProtocol: model.ProtoHTTP},
			},
		},
	}
	proxy.startHealthChecker()
	proxy.mtx.RLock()
	assert.Nil(t, proxy.health)
	proxy.mtx.RUnlock()
}

func TestStopHealthChecker_NilNoPanic(t *testing.T) {
	t.Parallel()
	proxy := &Proxy{}
	proxy.stopHealthChecker()
}

func TestReResolveHealthTarget_DisabledWhenNoAutoRestart(t *testing.T) {
	t.Parallel()
	proxy := &Proxy{Config: &model.Config{AutoRestart: false}}
	err := proxy.reResolveHealthTarget()
	require.NoError(t, err)
}

func TestReResolveHealthTarget_NilResolverNoError(t *testing.T) {
	t.Parallel()
	proxy := &Proxy{
		Config:          &model.Config{AutoRestart: true},
		reResolveConfig: nil,
	}
	err := proxy.reResolveHealthTarget()
	require.NoError(t, err)
}

func TestReResolveHealthTarget_ContextCancelled(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	proxy := &Proxy{
		Config: &model.Config{AutoRestart: true},
		ctx:    ctx,
		reResolveConfig: func() (*model.Config, error) {
			return &model.Config{}, nil
		},
	}
	err := proxy.reResolveHealthTarget()
	require.NoError(t, err)
}

func TestClampDuration_ReturnsInputWhenInRange(t *testing.T) {
	t.Parallel()
	d := clampDuration(10, time.Second, time.Minute)
	assert.Equal(t, 10*time.Second, d)
}

func TestClampDuration_ClampsToMin(t *testing.T) {
	t.Parallel()
	d := clampDuration(0, time.Second, time.Minute)
	assert.Equal(t, time.Second, d)
}

func TestClampDuration_ClampsToMax(t *testing.T) {
	t.Parallel()
	d := clampDuration(99999, time.Second, time.Minute)
	assert.Equal(t, time.Minute, d)
}

func TestClampDuration_NegativeClampsToMin(t *testing.T) {
	t.Parallel()
	d := clampDuration(-5, time.Second, time.Minute)
	assert.Equal(t, time.Second, d)
}

func TestProviderUserMiddleware_SetsWhoisContext(t *testing.T) {
	t.Parallel()
	expectedWhois := model.Whois{ID: "user123", Username: "testuser"}
	proxy := &Proxy{
		log:           zerolog.Nop(),
		providerProxy: &stubProviderProxy{whoisFunc: func(_ *http.Request) model.Whois { return expectedWhois }},
	}

	handler := proxy.ProviderUserMiddleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got, ok := model.WhoisFromContext(r.Context())
		assert.True(t, ok)
		assert.Equal(t, expectedWhois, got)
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	handler.ServeHTTP(rec, req)
}

func TestSetMetricsReady_WithMetrics(t *testing.T) {
	reg := prometheus.NewRegistry()
	oldReg := prometheus.DefaultRegisterer
	oldGatherer := prometheus.DefaultGatherer
	prometheus.DefaultRegisterer = reg
	prometheus.DefaultGatherer = reg
	t.Cleanup(func() {
		prometheus.DefaultRegisterer = oldReg
		prometheus.DefaultGatherer = oldGatherer
	})

	m := metrics.New(nil)
	proxy := &Proxy{
		log:     zerolog.Nop(),
		Config:  &model.Config{Hostname: "test-proxy"},
		metrics: m,
	}
	proxy.setMetricsReady(true)
	proxy.setMetricsReady(false)
}

func TestGetCustomTLSListener_NoRawTCPListener(t *testing.T) {
	t.Parallel()
	proxy := &Proxy{
		log:           zerolog.Nop(),
		Config:        &model.Config{Domain: "app.example.com"},
		providerProxy: &stubProviderProxy{},
		tlsProvider:   &mockTLSProvider{name: "acme"},
	}
	_, err := proxy.getCustomTLSListener("port1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "requires raw TCP listener support")
}

func TestGetCustomTLSListener_RawTCPError(t *testing.T) {
	t.Parallel()
	proxy := &Proxy{
		log:    zerolog.Nop(),
		Config: &model.Config{Domain: "app.example.com"},
		providerProxy: &stubRawTCPProviderProxy{
			rawListenerErr: errors.New("no raw listener"),
		},
		tlsProvider: &mockTLSProvider{name: "acme"},
	}
	_, err := proxy.getCustomTLSListener("port1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "get raw tcp listener")
}

func TestGetCustomTLSListener_ReturnsTLSListener(t *testing.T) {
	t.Parallel()
	rawLn, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = rawLn.Close() })

	proxy := &Proxy{
		log:    zerolog.Nop(),
		Config: &model.Config{Domain: "app.example.com"},
		providerProxy: &stubRawTCPProviderProxy{
			rawListener: rawLn,
		},
		tlsProvider: &mockTLSProvider{name: "acme"},
	}

	tlsLn, err := proxy.getCustomTLSListener("port1")
	require.NoError(t, err)
	require.NotNil(t, tlsLn)
	assert.NotNil(t, tlsLn)
}

func TestGetTailscaleCertificate_NoLocalClient(t *testing.T) {
	t.Parallel()
	proxy := &Proxy{
		log:           zerolog.Nop(),
		providerProxy: &stubProviderProxy{url: "https://test.tailnet.ts.net"},
	}
	_, err := proxy.getTailscaleCertificate(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not support GetLocalClient")
}

func TestGetTailscaleCertificate_NilLocalClient(t *testing.T) {
	t.Parallel()
	proxy := &Proxy{
		log: zerolog.Nop(),
		providerProxy: &stubLocalClientProvider{
			localClient: nil,
		},
	}
	_, err := proxy.getTailscaleCertificate(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "local client not available")
}

func TestGetTailscaleCertificate_EmptyURL(t *testing.T) {
	t.Parallel()
	proxy := &Proxy{
		log: zerolog.Nop(),
		providerProxy: &stubLocalClientProvider{
			stubProviderProxy: stubProviderProxy{url: ""},
			localClient:       &local.Client{},
		},
	}
	_, err := proxy.getTailscaleCertificate(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "hostname not yet available")
}

func TestPause_NotRunning_ReturnsError(t *testing.T) {
	t.Parallel()
	proxy := &Proxy{
		log:    zerolog.Nop(),
		Config: &model.Config{Hostname: "test"},
		status: model.ProxyStatusStopped,
	}
	err := proxy.Pause()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expected Running")
}

func TestPause_AlreadyPaused_ReturnsError(t *testing.T) {
	t.Parallel()
	proxy := &Proxy{
		log:           zerolog.Nop(),
		Config:        &model.Config{Hostname: "test", Ports: map[string]model.PortConfig{}},
		status:        model.ProxyStatusRunning,
		providerProxy: &stubProviderProxy{},
		ports:         make(map[string]portHandler),
		statusHistory: make([]StatusTransition, 0, maxStatusHistory),
	}
	err := proxy.Pause()
	require.NoError(t, err)

	err = proxy.Pause()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already paused")
}

func TestPause_Success(t *testing.T) {
	t.Parallel()
	proxy := &Proxy{
		log:           zerolog.Nop(),
		Config:        &model.Config{Hostname: "test", Ports: map[string]model.PortConfig{}},
		status:        model.ProxyStatusRunning,
		providerProxy: &stubProviderProxy{},
		ports:         make(map[string]portHandler),
		statusHistory: make([]StatusTransition, 0, maxStatusHistory),
	}

	err := proxy.Pause()
	require.NoError(t, err)
	assert.True(t, proxy.paused)
	assert.Equal(t, model.ProxyStatusPaused, proxy.GetStatus())
}

func TestResume_NotPaused_ReturnsError(t *testing.T) {
	t.Parallel()
	proxy := &Proxy{
		log:    zerolog.Nop(),
		Config: &model.Config{Hostname: "test"},
	}
	err := proxy.Resume()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "is not paused")
}

func TestResume_AllListenersFail_ReturnsError(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	proxy := &Proxy{
		log: zerolog.Nop(),
		Config: &model.Config{
			Hostname: "test",
			Ports: map[string]model.PortConfig{
				"1": {ProxyProtocol: model.ProtoTCP},
			},
		},
		providerProxy: &stubProviderProxy{listenerErr: errors.New("no listener")},
		paused:        true,
		ports:         make(map[string]portHandler),
		statusHistory: make([]StatusTransition, 0, maxStatusHistory),
		ctx:           ctx,
		cancel:        cancel,
	}
	pc := proxy.Config.Ports["1"]
	pc.AddTarget(&url.URL{Scheme: "tcp", Host: "127.0.0.1:1"})
	proxy.Config.Ports["1"] = pc
	proxy.initPorts()

	err := proxy.Resume()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "all 1 listeners errored")
}

func TestGetStatus_InitialZeroValue(t *testing.T) {
	t.Parallel()
	proxy := &Proxy{}
	assert.Equal(t, model.ProxyStatus(0), proxy.GetStatus())
}
