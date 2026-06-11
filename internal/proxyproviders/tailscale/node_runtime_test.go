// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package tailscale

import (
	"context"
	"testing"

	"tailscale.com/client/local"
	"tailscale.com/tsnet"
)

func TestNewNodeRuntime(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	server := &tsnet.Server{Hostname: "test"}
	var lc *local.Client

	rt := NewNodeRuntime(ctx, server, lc, cancel)

	if rt == nil {
		t.Fatal("NewNodeRuntime returned nil")
	}
	if rt.Server != server {
		t.Error("Server field not set correctly")
	}
	if rt.Ctx != ctx {
		t.Error("Ctx field not set correctly")
	}
	if rt.Cancel == nil {
		t.Error("Cancel field is nil")
	}
}

func TestNewNodeRuntimeClose(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	var lc *local.Client

	rt := NewNodeRuntime(ctx, nil, lc, cancel)

	rt.Cancel()

	if ctx.Err() == nil {
		t.Error("expected context to be cancelled after calling Cancel()")
	}
}
