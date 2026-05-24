// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package tailscale

import (
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"golang.org/x/sync/semaphore"

	"github.com/almeidapaulopt/tsdproxy/internal/model"
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

func TestNewSharedProxy_RejectsNonHTTPSPorts(t *testing.T) {
	c := &Client{
		log:            zerolog.Nop(),
		shared:         true,
		sharedHostname: "test-shared",
		datadir:        t.TempDir(),
		certSem:        semaphore.NewWeighted(1),
	}

	tests := []struct {
		ports    map[string]model.PortConfig
		name     string
		errMatch string
		wantErr  bool
	}{
		{
			name: "https only",
			ports: map[string]model.PortConfig{
				"1": {ProxyProtocol: model.ProtoHTTPS, ProxyPort: 443},
			},
			wantErr: false,
		},
		{
			name: "tcp port rejected",
			ports: map[string]model.PortConfig{
				"1": {ProxyProtocol: model.ProtoHTTPS, ProxyPort: 443},
				"2": {ProxyProtocol: model.ProtoTCP, ProxyPort: 22},
			},
			wantErr:  true,
			errMatch: "only supports HTTPS ports",
		},
		{
			name: "http port rejected",
			ports: map[string]model.PortConfig{
				"1": {ProxyProtocol: model.ProtoHTTP, ProxyPort: 80},
			},
			wantErr:  true,
			errMatch: "only supports HTTPS ports",
		},
		{
			name: "redirect port rejected",
			ports: map[string]model.PortConfig{
				"1": {ProxyProtocol: model.ProtoHTTPS, ProxyPort: 443},
				"2": {ProxyProtocol: model.ProtoHTTP, ProxyPort: 80, IsRedirect: true},
			},
			wantErr:  true,
			errMatch: "only supports HTTPS ports",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &model.Config{
				Hostname: "test",
				Domain:   "test.example.com",
				Tailscale: model.Tailscale{
					ResolvedAuthKey: "test-key",
				},
				Ports: tt.ports,
			}

			_, err := c.NewProxy(cfg)

			c.sharedMu.Lock()
			if c.sharedServer != nil {
				c.sharedServer.Close()
				c.sharedServer = nil
			}
			c.sharedMu.Unlock()

			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tt.errMatch != "" && !strings.Contains(err.Error(), tt.errMatch) {
					t.Fatalf("error %q should contain %q", err.Error(), tt.errMatch)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			}
		})
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
