// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package model

import (
	"testing"

	"github.com/go-playground/validator/v10"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewConfig_DefaultsEmpty(t *testing.T) {
	cfg, err := NewConfig()
	require.NoError(t, err)
	assert.Equal(t, "", cfg.Domain)
	assert.Equal(t, "", cfg.DNSProvider)
	assert.Equal(t, "", cfg.TLSProvider)
}

func TestConfig_Validation_EmptyDomain(t *testing.T) {
	validate := validator.New()

	cfg := struct {
		Domain      string `validate:"omitempty,fqdn"`
		DNSProvider string `validate:"omitempty"`
		TLSProvider string `validate:"omitempty"`
	}{}

	err := validate.Struct(cfg)
	assert.NoError(t, err)
}

func TestConfig_Validation_ValidFQDN(t *testing.T) {
	validate := validator.New()

	cfg := struct {
		Domain      string `validate:"omitempty,fqdn"`
		DNSProvider string `validate:"omitempty"`
		TLSProvider string `validate:"omitempty"`
	}{
		Domain:      "app.example.com",
		DNSProvider: "cloudflare",
		TLSProvider: "acme",
	}

	err := validate.Struct(cfg)
	assert.NoError(t, err)
}

func TestConfig_Validation_InvalidDomain(t *testing.T) {
	validate := validator.New()

	cfg := struct {
		Domain string `validate:"omitempty,fqdn"`
	}{
		Domain: "not a domain!",
	}

	err := validate.Struct(cfg)
	assert.Error(t, err)
}

func TestValidateProxyPorts_FunnelRequiresHTTPS(t *testing.T) {
	cfg, err := NewConfig()
	require.NoError(t, err)

	cfg.Ports = PortConfigList{
		"1": {ProxyProtocol: ProtoHTTP, ProxyPort: 80, Tailscale: TailscalePort{Funnel: true}},
	}
	err = ValidateProxyPorts(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "funnel")
	assert.Contains(t, err.Error(), "HTTPS")
}

func TestValidateProxyPorts_FunnelWithTCP(t *testing.T) {
	cfg, err := NewConfig()
	require.NoError(t, err)

	cfg.Ports = PortConfigList{
		"1": {ProxyProtocol: ProtoTCP, ProxyPort: 22, Tailscale: TailscalePort{Funnel: true}},
	}
	err = ValidateProxyPorts(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "funnel")
}

func TestValidateProxyPorts_FunnelWithUDP(t *testing.T) {
	cfg, err := NewConfig()
	require.NoError(t, err)

	cfg.Ports = PortConfigList{
		"1": {ProxyProtocol: ProtoUDP, ProxyPort: 5060, Tailscale: TailscalePort{Funnel: true}},
	}
	err = ValidateProxyPorts(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "funnel")
}

func TestValidateProxyPorts_FunnelWithHTTPS(t *testing.T) {
	cfg, err := NewConfig()
	require.NoError(t, err)

	cfg.Ports = PortConfigList{
		"1": {ProxyProtocol: ProtoHTTPS, ProxyPort: 443, Tailscale: TailscalePort{Funnel: true}},
	}
	err = ValidateProxyPorts(cfg)
	assert.NoError(t, err)
}

func TestValidateProxyPorts_NoPorts(t *testing.T) {
	cfg, err := NewConfig()
	require.NoError(t, err)

	cfg.Ports = PortConfigList{}
	err = ValidateProxyPorts(cfg)
	assert.NoError(t, err)
}

func TestValidateProxyPorts_NilPorts(t *testing.T) {
	cfg, err := NewConfig()
	require.NoError(t, err)

	cfg.Ports = nil
	err = ValidateProxyPorts(cfg)
	assert.NoError(t, err)
}

func TestValidateProxyPorts_NoFunnel(t *testing.T) {
	cfg, err := NewConfig()
	require.NoError(t, err)

	cfg.Ports = PortConfigList{
		"1": {ProxyProtocol: ProtoHTTP, ProxyPort: 80, Tailscale: TailscalePort{Funnel: false}},
		"2": {ProxyProtocol: ProtoTCP, ProxyPort: 22, Tailscale: TailscalePort{Funnel: false}},
		"3": {ProxyProtocol: ProtoUDP, ProxyPort: 5060, Tailscale: TailscalePort{Funnel: false}},
	}
	err = ValidateProxyPorts(cfg)
	assert.NoError(t, err)
}

func TestValidateProxyPorts_MixedPortsOneBad(t *testing.T) {
	cfg, err := NewConfig()
	require.NoError(t, err)

	cfg.Ports = PortConfigList{
		"1": {ProxyProtocol: ProtoHTTPS, ProxyPort: 443, Tailscale: TailscalePort{Funnel: true}},
		"2": {ProxyProtocol: ProtoTCP, ProxyPort: 22, Tailscale: TailscalePort{Funnel: true}},
	}
	err = ValidateProxyPorts(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "funnel")
}

// --- ValidateProxyConfigForMode: services mode ---

func TestValidateConfigForMode_ServicesRejectsUDP(t *testing.T) {
	cfg, err := NewConfig()
	require.NoError(t, err)

	cfg.Ports = PortConfigList{
		"1": {ProxyProtocol: ProtoUDP, ProxyPort: 5060},
	}
	err = ValidateProxyConfigForMode(cfg, ProviderModeServices)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "UDP")
	assert.Contains(t, err.Error(), "services")
}

func TestValidateConfigForMode_ServicesAcceptsHTTPS(t *testing.T) {
	cfg, err := NewConfig()
	require.NoError(t, err)

	cfg.Ports = PortConfigList{
		"1": {ProxyProtocol: ProtoHTTPS, ProxyPort: 443},
	}
	err = ValidateProxyConfigForMode(cfg, ProviderModeServices)
	assert.NoError(t, err)
}

func TestValidateConfigForMode_ServicesAcceptsHTTP(t *testing.T) {
	cfg, err := NewConfig()
	require.NoError(t, err)

	cfg.Ports = PortConfigList{
		"1": {ProxyProtocol: ProtoHTTP, ProxyPort: 80},
	}
	err = ValidateProxyConfigForMode(cfg, ProviderModeServices)
	assert.NoError(t, err)
}

func TestValidateConfigForMode_ServicesAcceptsTCP(t *testing.T) {
	cfg, err := NewConfig()
	require.NoError(t, err)

	cfg.Ports = PortConfigList{
		"1": {ProxyProtocol: ProtoTCP, ProxyPort: 2222},
	}
	err = ValidateProxyConfigForMode(cfg, ProviderModeServices)
	assert.NoError(t, err)
}

func TestValidateConfigForMode_ServicesRejectsCustomDomain(t *testing.T) {
	cfg, err := NewConfig()
	require.NoError(t, err)

	cfg.Domain = "app.example.com"
	cfg.Ports = PortConfigList{
		"1": {ProxyProtocol: ProtoHTTPS, ProxyPort: 443},
	}
	err = ValidateProxyConfigForMode(cfg, ProviderModeServices)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "domain")
	assert.Contains(t, err.Error(), "services")
}

func TestValidateConfigForMode_ServicesRejectsFunnelOnHTTP(t *testing.T) {
	cfg, err := NewConfig()
	require.NoError(t, err)

	cfg.Ports = PortConfigList{
		"1": {ProxyProtocol: ProtoHTTP, ProxyPort: 80, Tailscale: TailscalePort{Funnel: true}},
	}
	err = ValidateProxyConfigForMode(cfg, ProviderModeServices)
	assert.Error(t, err)
}

func TestValidateConfigForMode_ServicesNoPortsIsValid(t *testing.T) {
	cfg, err := NewConfig()
	require.NoError(t, err)

	cfg.Ports = PortConfigList{}
	err = ValidateProxyConfigForMode(cfg, ProviderModeServices)
	assert.NoError(t, err)
}

func TestValidateConfigForMode_ServicesRejectsMultipleInvalidPorts(t *testing.T) {
	cfg, err := NewConfig()
	require.NoError(t, err)

	cfg.Ports = PortConfigList{
		"1": {ProxyProtocol: ProtoUDP, ProxyPort: 5060},
		"2": {ProxyProtocol: ProtoHTTPS, ProxyPort: 443},
	}
	cfg.Domain = "custom.example.com"
	err = ValidateProxyConfigForMode(cfg, ProviderModeServices)
	assert.Error(t, err)
}

// --- ValidateProxyConfigForMode: shared mode ---

func TestValidateConfigForMode_SharedRequiresDomain(t *testing.T) {
	cfg, err := NewConfig()
	require.NoError(t, err)

	cfg.Domain = ""
	cfg.Ports = PortConfigList{
		"1": {ProxyProtocol: ProtoHTTPS, ProxyPort: 443},
	}
	err = ValidateProxyConfigForMode(cfg, ProviderModeShared)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "domain")
	assert.Contains(t, err.Error(), "shared")
}

func TestValidateConfigForMode_SharedAcceptsDomain(t *testing.T) {
	cfg, err := NewConfig()
	require.NoError(t, err)

	cfg.Domain = "app.example.com"
	cfg.Ports = PortConfigList{
		"1": {ProxyProtocol: ProtoHTTPS, ProxyPort: 443},
	}
	err = ValidateProxyConfigForMode(cfg, ProviderModeShared)
	assert.NoError(t, err)
}

func TestValidateConfigForMode_SharedAcceptsHTTPPort(t *testing.T) {
	cfg, err := NewConfig()
	require.NoError(t, err)

	cfg.Domain = "app.example.com"
	cfg.Ports = PortConfigList{
		"1": {ProxyProtocol: ProtoHTTP, ProxyPort: 80},
	}
	err = ValidateProxyConfigForMode(cfg, ProviderModeShared)
	assert.NoError(t, err)
}

func TestValidateConfigForMode_SharedAcceptsTCPPort(t *testing.T) {
	cfg, err := NewConfig()
	require.NoError(t, err)

	cfg.Domain = "app.example.com"
	cfg.Ports = PortConfigList{
		"1": {ProxyProtocol: ProtoTCP, ProxyPort: 2222},
	}
	err = ValidateProxyConfigForMode(cfg, ProviderModeShared)
	assert.NoError(t, err)
}

// --- ValidateProxyConfigForMode: per-proxy mode ---

func TestValidateConfigForMode_PerProxyAcceptsAll(t *testing.T) {
	cfg, err := NewConfig()
	require.NoError(t, err)

	cfg.Ports = PortConfigList{
		"1": {ProxyProtocol: ProtoHTTPS, ProxyPort: 443},
		"2": {ProxyProtocol: ProtoUDP, ProxyPort: 5060},
		"3": {ProxyProtocol: ProtoTCP, ProxyPort: 22},
	}
	err = ValidateProxyConfigForMode(cfg, ProviderModePerProxy)
	assert.NoError(t, err)
}

func TestValidateConfigForMode_PerProxyDomainOptional(t *testing.T) {
	cfg, err := NewConfig()
	require.NoError(t, err)

	cfg.Domain = ""
	cfg.Ports = PortConfigList{
		"1": {ProxyProtocol: ProtoHTTPS, ProxyPort: 443},
	}
	err = ValidateProxyConfigForMode(cfg, ProviderModePerProxy)
	assert.NoError(t, err)
}

func TestValidateConfigForMode_PerProxyFunnelOnHTTPRejected(t *testing.T) {
	cfg, err := NewConfig()
	require.NoError(t, err)

	cfg.Ports = PortConfigList{
		"1": {ProxyProtocol: ProtoHTTP, ProxyPort: 80, Tailscale: TailscalePort{Funnel: true}},
	}
	err = ValidateProxyConfigForMode(cfg, ProviderModePerProxy)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "funnel")
}
