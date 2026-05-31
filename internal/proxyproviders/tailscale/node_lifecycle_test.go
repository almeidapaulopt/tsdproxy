// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package tailscale

import (
	"context"
	"errors"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"tailscale.com/tsnet"

	"github.com/almeidapaulopt/tsdproxy/internal/model"
)

// --- NewNodeLifecycle ---

func TestNewNodeLifecycle(t *testing.T) {
	t.Parallel()

	cfg := NodeLifecycleConfig{
		NodeConfig: NodeConfig{
			Hostname: "test-node",
			DataDir:  "/tmp/test",
		},
		Retry: NewRetryPolicy(),
	}

	nl := NewNodeLifecycle(zerolog.Nop(), cfg)
	require.NotNil(t, nl)
	assert.Equal(t, "test-node", nl.cfg.Hostname)
	assert.Equal(t, "/tmp/test", nl.cfg.DataDir)
	assert.NotNil(t, nl.events)
}

// --- WatchEvents ---

func TestNodeLifecycle_WatchEvents_ReturnsChannel(t *testing.T) {
	t.Parallel()

	nl := NewNodeLifecycle(zerolog.Nop(), NodeLifecycleConfig{})
	ch := nl.WatchEvents()
	assert.NotNil(t, ch)
}

// --- GetRuntime before Start ---

func TestNodeLifecycle_GetRuntime_BeforeStart_Nil(t *testing.T) {
	t.Parallel()

	nl := NewNodeLifecycle(zerolog.Nop(), NodeLifecycleConfig{})
	assert.Nil(t, nl.GetRuntime())
}

// --- Close before Start ---

func TestNodeLifecycle_Close_BeforeStart_NoError(t *testing.T) {
	t.Parallel()

	nl := NewNodeLifecycle(zerolog.Nop(), NodeLifecycleConfig{})
	assert.NoError(t, nl.Close())
}

func TestNodeLifecycle_Close_ClosesEventsChannel(t *testing.T) {
	t.Parallel()

	nl := NewNodeLifecycle(zerolog.Nop(), NodeLifecycleConfig{})
	ch := nl.WatchEvents()

	assert.NoError(t, nl.Close())

	_, ok := <-ch
	assert.False(t, ok, "events channel should be closed")
}

func TestNodeLifecycle_DoubleClose_Safe(t *testing.T) {
	t.Parallel()

	nl := NewNodeLifecycle(zerolog.Nop(), NodeLifecycleConfig{})

	assert.NoError(t, nl.Close())
	assert.NoError(t, nl.Close())

	ch := nl.WatchEvents()
	_, ok := <-ch
	assert.False(t, ok, "events channel should be closed exactly once")
}

// --- RetryPolicy.IsRecoverable ---

func TestRetryPolicy_IsRecoverable_NilError(t *testing.T) {
	t.Parallel()

	p := NewRetryPolicy()
	assert.False(t, p.IsRecoverable(nil))
}

func TestRetryPolicy_IsRecoverable_TagPermission(t *testing.T) {
	t.Parallel()

	p := NewRetryPolicy()
	err := errors.New("tag:bad is invalid or not permitted for this client")
	assert.False(t, p.IsRecoverable(err))
}

func TestRetryPolicy_IsRecoverable_HardwareAttestation(t *testing.T) {
	t.Parallel()

	p := NewRetryPolicy()
	err := errors.New("hardware attestation required")
	assert.False(t, p.IsRecoverable(err))
}

func TestRetryPolicy_IsRecoverable_TransientError(t *testing.T) {
	t.Parallel()

	p := NewRetryPolicy()
	err := errors.New("connection refused")
	assert.True(t, p.IsRecoverable(err))
}

func TestRetryPolicy_IsRecoverable_GenericError(t *testing.T) {
	t.Parallel()

	p := NewRetryPolicy()
	err := errors.New("something went wrong")
	assert.True(t, p.IsRecoverable(err))
}

// --- NodeRuntime ---

func TestNewNodeRuntime_SetsFields(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	srv := &tsnet.Server{Hostname: "test"}
	rt := NewNodeRuntime(srv, nil, ctx, cancel)

	assert.Equal(t, srv, rt.Server)
	assert.Nil(t, rt.LocalClient)
	assert.Equal(t, ctx, rt.Ctx)
	assert.Equal(t, "", rt.URL)
	assert.Equal(t, "", rt.AuthURL)
}

func TestNodeRuntime_CancelStopsContext(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	srv := &tsnet.Server{Hostname: "test"}
	rt := NewNodeRuntime(srv, nil, ctx, cancel)

	assert.NoError(t, ctx.Err(), "context should not be canceled yet")

	rt.Cancel()

	assert.ErrorIs(t, ctx.Err(), context.Canceled, "context should be canceled after Cancel")
}

// --- HasHTTPSPort ---

func TestHasHTTPSPort_WithHTTPS(t *testing.T) {
	t.Parallel()

	cfg := &model.Config{
		Ports: model.PortConfigList{
			"1": {ProxyProtocol: model.ProtoHTTPS},
		},
	}
	assert.True(t, HasHTTPSPort(cfg))
}

func TestHasHTTPSPort_WithoutHTTPS(t *testing.T) {
	t.Parallel()

	cfg := &model.Config{
		Ports: model.PortConfigList{
			"1": {ProxyProtocol: model.ProtoHTTP},
			"2": {ProxyProtocol: model.ProtoTCP},
		},
	}
	assert.False(t, HasHTTPSPort(cfg))
}

func TestHasHTTPSPort_EmptyPorts(t *testing.T) {
	t.Parallel()

	cfg := &model.Config{
		Ports: model.PortConfigList{},
	}
	assert.False(t, HasHTTPSPort(cfg))
}

func TestHasHTTPSPort_NilPorts(t *testing.T) {
	t.Parallel()

	cfg := &model.Config{
		Ports: nil,
	}
	assert.False(t, HasHTTPSPort(cfg))
}

// --- NewRetryPolicy defaults ---

func TestNewRetryPolicy_Defaults(t *testing.T) {
	t.Parallel()

	p := NewRetryPolicy()
	assert.Equal(t, 3, p.MaxAttempts) //nolint:mnd
}
