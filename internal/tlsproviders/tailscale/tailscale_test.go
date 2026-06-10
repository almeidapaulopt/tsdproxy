// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package tailscale

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNew(t *testing.T) {
	p := New(nil, 0)
	assert.NotNil(t, p)
}

func TestProvider_Name(t *testing.T) {
	p := New(nil, 0)
	assert.Equal(t, "tailscale", p.Name())
}

func TestProvider_Provision_NilClient(t *testing.T) {
	p := New(nil, 0)
	err := p.Provision(context.Background(), "myapp.tailnet.ts.net")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "local client is nil")
}

func TestProvider_Provision_NotMagicDNSDomain(t *testing.T) {
	p := New(nil, 0)
	err := p.Provision(context.Background(), "app.example.com")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not a MagicDNS domain")
}

func TestProvider_Cleanup(t *testing.T) {
	p := New(nil, 0)
	err := p.Cleanup(context.Background(), "myapp.tailnet.ts.net")
	assert.NoError(t, err)
}

func TestIsMagicDNSDomain(t *testing.T) {
	assert.True(t, isMagicDNSDomain("myapp.tailnet.ts.net"))
	assert.True(t, isMagicDNSDomain("MYAPP.TAILNET.TS.NET"))
	assert.False(t, isMagicDNSDomain("app.example.com"))
	assert.False(t, isMagicDNSDomain("ts.net.evil.com"))
}

func TestProvider_New_ZeroConcurrency(t *testing.T) {
	p := New(nil, 0)
	assert.NotNil(t, p)
	assert.NotNil(t, p.certSem)
}

func TestProvider_New_NegativeConcurrency(t *testing.T) {
	p := New(nil, -5)
	assert.NotNil(t, p)
	assert.NotNil(t, p.certSem)
}

func TestProvider_SetLocalClient(t *testing.T) {
	p := New(nil, 0)
	p.SetLocalClient(nil)
	err := p.Provision(context.Background(), "myapp.tailnet.ts.net")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "local client is nil")
}

func TestProvider_SetMaxConcurrency(t *testing.T) {
	p := New(nil, 0)
	p.SetMaxConcurrency(5)
	assert.NotNil(t, p.certSem)
}

func TestProvider_SetMaxConcurrency_Zero(t *testing.T) {
	p := New(nil, 0)
	p.SetMaxConcurrency(0)
	assert.NotNil(t, p.certSem)
}

func TestProvider_GetCertificate_NilClient(t *testing.T) {
	p := New(nil, 0)
	_, err := p.GetCertificate(context.Background(), "myapp.tailnet.ts.net")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "local client is nil")
}
