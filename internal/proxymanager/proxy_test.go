// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package proxymanager

import (
	"context"
	"errors"
	"net"
	"net/http"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/almeidapaulopt/tsdproxy/internal/model"
	"github.com/almeidapaulopt/tsdproxy/internal/proxyproviders"
)

var errStubNotImplemented = errors.New("stub: not implemented")

type stubProviderProxy struct {
	listener    net.Listener
	listenerErr error
}

var _ proxyproviders.ProxyInterface = (*stubProviderProxy)(nil)

func (s *stubProviderProxy) Start(_ context.Context) error { return nil }
func (s *stubProviderProxy) Close() error                  { return nil }
func (s *stubProviderProxy) GetListener(_ string) (net.Listener, error) {
	return s.listener, s.listenerErr
}

func (s *stubProviderProxy) GetPacketConn(_ string) (net.PacketConn, error) {
	return nil, errStubNotImplemented
}
func (s *stubProviderProxy) GetURL() string                     { return "" }
func (s *stubProviderProxy) GetAuthURL() string                 { return "" }
func (s *stubProviderProxy) WatchEvents() chan model.ProxyEvent { return nil }
func (s *stubProviderProxy) Whois(_ *http.Request) model.Whois  { return model.Whois{} }

type stubRawTCPProviderProxy struct {
	stubProviderProxy
	rawListener    net.Listener
	rawListenerErr error
	rawCalled      bool
}

var _ proxyproviders.RawTCPListener = (*stubRawTCPProviderProxy)(nil)

func (s *stubRawTCPProviderProxy) GetRawTCPListener(_ string) (net.Listener, error) {
	s.rawCalled = true
	return s.rawListener, s.rawListenerErr
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
