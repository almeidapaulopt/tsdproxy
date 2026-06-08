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
	err := p.Provision(context.TODO(), "myapp.tailnet.ts.net")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "local client is nil")
}

func TestProvider_Provision_NotMagicDNSDomain(t *testing.T) {
	p := New(nil, 0)
	err := p.Provision(context.TODO(), "app.example.com")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not a MagicDNS domain")
}

func TestProvider_Cleanup(t *testing.T) {
	p := New(nil, 0)
	err := p.Cleanup(context.TODO(), "myapp.tailnet.ts.net")
	assert.NoError(t, err)
}

func TestIsMagicDNSDomain(t *testing.T) {
	assert.True(t, isMagicDNSDomain("myapp.tailnet.ts.net"))
	assert.True(t, isMagicDNSDomain("MYAPP.TAILNET.TS.NET"))
	assert.False(t, isMagicDNSDomain("app.example.com"))
	assert.False(t, isMagicDNSDomain("ts.net.evil.com"))
}
