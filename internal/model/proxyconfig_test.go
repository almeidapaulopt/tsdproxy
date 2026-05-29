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
