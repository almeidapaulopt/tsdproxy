// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package magicdns

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNew(t *testing.T) {
	p := New()
	assert.NotNil(t, p)
}

func TestProvider_Name(t *testing.T) {
	p := New()
	assert.Equal(t, "magicdns", p.Name())
}

func TestProvider_CreateRecord(t *testing.T) {
	p := New()
	err := p.CreateRecord(context.Background(), "app.example.com", "CNAME", "app.tailnet.ts.net")
	assert.NoError(t, err)
}

func TestProvider_DeleteRecord(t *testing.T) {
	p := New()
	err := p.DeleteRecord(context.Background(), "app.example.com", "CNAME")
	assert.NoError(t, err)
}

func TestProvider_ValidateRecord(t *testing.T) {
	p := New()
	ok, err := p.ValidateRecord(context.Background(), "app.example.com", "CNAME", "app.tailnet.ts.net")
	assert.True(t, ok)
	assert.NoError(t, err)
}
