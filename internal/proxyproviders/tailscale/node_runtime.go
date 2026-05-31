// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package tailscale

import (
	"context"

	"tailscale.com/client/local"
	"tailscale.com/tsnet"
)

// NodeRuntime holds the runtime state of a started Tailscale node.
// It is created by NodeLifecycle.Start and owned by the lifecycle layer.
// Exposure strategies may use it, but should not mutate node lifecycle state directly.
type NodeRuntime struct {
	Ctx         context.Context
	Server      *tsnet.Server
	LocalClient *local.Client
	Cancel      context.CancelFunc
	URL         string
	AuthURL     string
}

// NewNodeRuntime creates a NodeRuntime from a started tsnet.Server.
func NewNodeRuntime(server *tsnet.Server, lc *local.Client, ctx context.Context, cancel context.CancelFunc) *NodeRuntime {
	return &NodeRuntime{
		Server:      server,
		LocalClient: lc,
		Ctx:         ctx,
		Cancel:      cancel,
	}
}

// Close cancels the context and shuts down the tsnet server.
func (rt *NodeRuntime) Close() error {
	rt.Cancel()
	return rt.Server.Close()
}
