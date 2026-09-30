// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package targetproviders

import (
	"context"
	"errors"

	"github.com/almeidapaulopt/tsdproxy/internal/model"
)

// ErrTargetNotFound is returned by AddTarget, DeleteProxy, or ReResolve when
// the requested target ID does not exist in the provider. Callers can use
// errors.Is to distinguish a missing target from other failures.
var ErrTargetNotFound = errors.New("target not found")

// ErrStreamDisconnected is sent to the error channel when the backing event
// stream (Docker daemon, etc.) closes unexpectedly. Consumers should use
// errors.Is to trigger a reconnect loop.
var ErrStreamDisconnected = errors.New("event stream disconnected")

type (
	// TargetProvider interface to be implemented by all target providers
	TargetProvider interface {
		WatchEvents(ctx context.Context, eventsChan chan TargetEvent, errChan chan error)
		GetDefaultProxyProviderName() string
		Close()
		AddTarget(id string) (*model.Config, error)
		DeleteProxy(id string) error
		ReResolve(id string) (*model.Config, error)
	}
)

const (
	ActionStartProxy ActionType = iota + 1
	ActionStopProxy
	ActionRestartProxy
	ActionStartPort
	ActionStopPort
	ActionRestartPort
)

type (
	ActionType int

	TargetEvent struct {
		TargetProvider TargetProvider
		ID             string
		Action         ActionType
	}
)
