// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package config

import "sync/atomic"

// Provider is an interface for accessing configuration data.
// It supports both snapshot injection (for logger, leaf consumers)
// and live-reload (for ProxyManager, admin middleware that reads
// config on every request or on ConfigUpdated).
type Provider interface {
	GetConfig() *Data
}

// atomicProvider wraps atomic.Pointer[Data] to implement Provider
// with lock-free live-reload support.
type atomicProvider struct {
	ptr atomic.Pointer[Data]
}

func (p *atomicProvider) GetConfig() *Data { return p.ptr.Load() }
func (p *atomicProvider) store(c *Data)    { p.ptr.Store(c) }

// NewProvider creates a Provider initially holding the given config.
func NewProvider(c *Data) Provider {
	p := &atomicProvider{}
	p.store(c)
	return p
}

// NewTestProvider creates a Provider for use in tests with minimal defaults.
func NewTestProvider(d ...*Data) Provider {
	cfg := &Data{
		DefaultProxyProvider: TailscaleDefaultProviderName,
		Tailscale: TailscaleProxyProviderConfig{
			DataDir:   "/tmp/tsdproxy-test",
			Providers: make(map[string]*TailscaleServerConfig),
		},
		Docker: make(map[string]*DockerTargetProviderConfig),
		Lists:  make(map[string]*ListTargetProviderConfig),
	}
	if len(d) > 0 && d[0] != nil {
		cfg = d[0]
	}
	if _, ok := cfg.Tailscale.Providers[TailscaleDefaultProviderName]; !ok {
		cfg.Tailscale.Providers[TailscaleDefaultProviderName] = &TailscaleServerConfig{
			ControlURL: defaultControlURL,
		}
	}
	return NewProvider(cfg)
}
