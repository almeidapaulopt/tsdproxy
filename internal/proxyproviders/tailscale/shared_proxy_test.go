// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package tailscale

import (
	"testing"

	"github.com/almeidapaulopt/tsdproxy/internal/model"
	"github.com/rs/zerolog"
)

func TestSharedProxyNeedsSNI(t *testing.T) {
	t.Parallel()

	p := &SharedProxy{}

	tests := []struct {
		protocol string
		want     bool
	}{
		{model.ProtoHTTPS, true},
		{model.ProtoHTTP, false},
		{model.ProtoTCP, false},
		{model.ProtoUDP, false},
	}

	for _, tt := range tests {
		portCfg := &model.PortConfig{ProxyProtocol: tt.protocol}
		got := p.needsSNI(portCfg)
		if got != tt.want {
			t.Errorf("needsSNI(%q) = %v, want %v", tt.protocol, got, tt.want)
		}
	}
}

func TestSharedProxyGetAuthURL(t *testing.T) {
	t.Parallel()

	p := &SharedProxy{}

	if url := p.GetAuthURL(); url != "" {
		t.Fatalf("GetAuthURL should return empty string, got %q", url)
	}
}

func TestSharedProxyGetURLReturnsEmptyWhenServerNotReady(t *testing.T) {
	t.Parallel()

	ss := NewSharedServer(SharedServerConfig{
		Hostname: "test-server",
		Log:      zerolog.Nop(),
	})

	p := &SharedProxy{
		shared: ss,
		config: &model.Config{
			Ports: map[string]model.PortConfig{
				"port1": {ProxyProtocol: model.ProtoHTTPS, ProxyPort: 443},
			},
		},
		log:    zerolog.Nop(),
		events: make(chan model.ProxyEvent, 1),
	}

	if url := p.GetURL(); url != "" {
		t.Fatalf("GetURL should return empty when server URL is empty, got %q", url)
	}
}
