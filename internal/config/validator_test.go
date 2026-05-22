// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidateProxyConfig_NoDomain(t *testing.T) {
	err := ValidateProxyConfig("", "", "", "", "")
	assert.NoError(t, err)
}

func TestValidateProxyConfig_DomainWithProviders(t *testing.T) {
	err := ValidateProxyConfig("app.example.com", "cloudflare", "acme", "", "")
	assert.NoError(t, err)
}

func TestValidateProxyConfig_DomainWithDefaults(t *testing.T) {
	err := ValidateProxyConfig("app.example.com", "", "", "cloudflare", "acme")
	assert.NoError(t, err)
}

func TestValidateProxyConfig_DomainWithoutDNSProvider(t *testing.T) {
	err := ValidateProxyConfig("app.example.com", "", "acme", "", "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "DNS")
}

func TestValidateProxyConfig_DomainWithoutTLSProvider(t *testing.T) {
	err := ValidateProxyConfig("app.example.com", "cloudflare", "", "", "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "TLS")
}

func TestValidateProxyConfig_DomainWithoutAnyProvider(t *testing.T) {
	err := ValidateProxyConfig("app.example.com", "", "", "", "")
	assert.Error(t, err)
}

func TestDomainProviderError(t *testing.T) {
	err := &DomainProviderError{Domain: "app.example.com", FieldType: "DNS"}
	assert.Contains(t, err.Error(), "app.example.com")
	assert.Contains(t, err.Error(), "DNS")
}
