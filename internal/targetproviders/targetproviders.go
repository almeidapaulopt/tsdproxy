// SPDX-FileCopyrightText: 2024 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT
package targetproviders

import (
	"context"

	"github.com/almeidapaulopt/tsdproxy/internal/proxyconfig"
)

const (
	ActionStart ActionType = iota
	ActionStop
	ActionRestart
)

type (
	// TargetProvider interface to be implemented by all target providers
	TargetProvider interface {
		GetAllProxies() (map[string]*proxyconfig.Config, error)
		WatchEvents(ctx context.Context, eventsChan chan TargetEvent, errChan chan error)
		GetDefaultProxyProviderName() string
		Close()
		AddTarget(id string) (*proxyconfig.Config, error)
		DeleteProxy(id string) error
	}

	ActionType int

	TargetEvent struct {
		TargetProvider TargetProvider
		ID             string
		Action         ActionType
	}
)
