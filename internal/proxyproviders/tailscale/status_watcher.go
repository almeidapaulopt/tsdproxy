// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package tailscale

import (
	"context"
	"errors"
	"net"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"tailscale.com/client/local"

	"github.com/almeidapaulopt/tsdproxy/internal/model"
)

const (
	pollInterval = 2 * time.Second

	// firstBootAuthGraceCycles is the number of consecutive
	// NeedsLogin-with-empty-authURL polls tolerated before the watcher forwards
	// a terminal AuthFailed event. On first boot Tailscale briefly reports
	// NeedsLogin with no auth URL while the auth key is still being processed,
	// which otherwise surfaces a misleading "auth failed / stale state" status
	// even though the node reaches Running moments later. The grace window keeps
	// genuinely-bad auth keys visible (they persist past the threshold) while
	// suppressing the transient first-boot false alarm.
	firstBootAuthGraceCycles = 3

	// Tailscale backend state strings.
	backendStateNeedsLogin       = "NeedsLogin"
	backendStateNoState          = "NoState"
	backendStateStopped          = "Stopped"
	backendStateStarting         = "Starting"
	backendStateNeedsMachineAuth = "NeedsMachineAuth"
	backendStateRunning          = "Running"
)

// NodeEvent represents a classified Tailscale node status change.
type NodeEvent struct {
	URL          string
	AuthURL      string
	ErrorCode    string
	ErrorMessage string
	Status       model.ProxyStatus
}

// StatusWatcherConfig holds the configuration for creating a StatusWatcher.
type StatusWatcherConfig struct {
	Log          zerolog.Logger
	OnEvent      func(NodeEvent)
	OnDone       func()
	PollInterval time.Duration // zero defaults to pollInterval (2s)
}

type statusSource interface {
	getStatus(ctx context.Context) (backendState string, authURL string, dnsName string, selfOK bool, err error)
}

type realStatusSource struct {
	lc *local.Client
}

func (s *realStatusSource) getStatus(ctx context.Context) (string, string, string, bool, error) {
	status, err := s.lc.Status(ctx)
	if err != nil {
		return "", "", "", false, err
	}
	dnsName := ""
	selfOK := status.Self != nil
	if selfOK {
		dnsName = strings.TrimRight(status.Self.DNSName, ".")
	}
	return status.BackendState, status.AuthURL, dnsName, selfOK, nil
}

// StatusWatcher monitors the Tailscale backend state by polling lc.Status()
// and classifies changes into NodeEvents.
type StatusWatcher struct {
	log          zerolog.Logger
	onEvent      func(NodeEvent)
	onDone       func()
	source       statusSource // nil means use realStatusSource
	pollInterval time.Duration
}

// NewStatusWatcher creates a new StatusWatcher.
func NewStatusWatcher(cfg StatusWatcherConfig) *StatusWatcher {
	onDone := cfg.OnDone
	if onDone == nil {
		onDone = func() {}
	}
	onEvent := cfg.OnEvent
	if onEvent == nil {
		onEvent = func(NodeEvent) {}
	}
	return &StatusWatcher{
		log:          cfg.Log,
		onEvent:      onEvent,
		onDone:       onDone,
		pollInterval: cfg.PollInterval,
	}
}

// Watch polls the Tailscale backend status until ctx is canceled or a fatal
// error occurs. Always calls onDone when finished.
// The first check happens immediately; subsequent checks follow pollInterval.
func (w *StatusWatcher) Watch(ctx context.Context, lc *local.Client) {
	defer w.onDone()

	source := w.source
	if source == nil {
		if lc == nil {
			w.log.Error().Msg("status watcher: local client is nil")
			return
		}
		source = &realStatusSource{lc: lc}
	}

	interval := w.pollInterval
	if interval <= 0 {
		interval = pollInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// needsLoginNoAuthStreak counts consecutive NeedsLogin-with-empty-authURL
	// polls so the misleading first-boot AuthFailed can be debounced.
	needsLoginNoAuthStreak := 0

	for {
		backendState, authURL, dnsName, selfOK, err := source.getStatus(ctx)
		if err != nil {
			if w.handleStatusError(ctx, err, ticker) {
				return
			}
			continue
		}

		evt := classifyState(backendState, authURL, dnsName)

		// Debounce the brief first-boot NeedsLogin window: while the auth key is
		// still being processed Tailscale reports NeedsLogin with no auth URL,
		// which classifyState maps to AuthFailed. Suppress that terminal status
		// until it has persisted past the grace threshold, emitting a benign
		// Starting status instead. Any other observed state resets the streak.
		if backendState == backendStateNeedsLogin && authURL == "" {
			needsLoginNoAuthStreak++
			if needsLoginNoAuthStreak <= firstBootAuthGraceCycles {
				evt = NodeEvent{Status: model.ProxyStatusStarting}
			}
		} else {
			needsLoginNoAuthStreak = 0
		}

		if evt.Status == model.ProxyStatusRunning && !selfOK {
			w.log.Warn().Msg("status watcher: Self is nil, skipping")
		} else {
			w.onEvent(evt)
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (w *StatusWatcher) handleStatusError(ctx context.Context, err error, ticker *time.Ticker) bool {
	if errors.Is(err, context.Canceled) {
		return true
	}
	if errors.Is(err, net.ErrClosed) {
		w.log.Debug().Msg("status watcher: status connection closed, retrying")
	} else {
		w.log.Warn().Err(err).Msg("status watcher: transient error, retrying")
	}
	return waitForRetry(ctx, ticker, errors.Is(err, net.ErrClosed))
}

func waitForRetry(ctx context.Context, ticker *time.Ticker, useTicker bool) bool {
	var delay <-chan time.Time
	if useTicker {
		delay = ticker.C
	} else {
		delay = time.After(1 * time.Second)
	}
	select {
	case <-delay:
		return false
	case <-ctx.Done():
		return true
	}
}

// classifyState maps a Tailscale backend state to a NodeEvent.
// authURL and dnsName may be empty.
func classifyState(backendState string, authURL string, dnsName string) NodeEvent {
	switch backendState {
	case backendStateNeedsLogin:
		if authURL == "" {
			return NodeEvent{Status: model.ProxyStatusAuthFailed, ErrorMessage: "needs reauthentication: no auth URL available, the auth key may be invalid or expired"}
		}
		return NodeEvent{Status: model.ProxyStatusAuthenticating, AuthURL: authURL}
	case backendStateNoState:
		return NodeEvent{Status: model.ProxyStatusStarting}
	case backendStateStopped:
		return NodeEvent{Status: model.ProxyStatusStopped}
	case backendStateStarting:
		return NodeEvent{Status: model.ProxyStatusStarting}
	case backendStateNeedsMachineAuth:
		return NodeEvent{Status: model.ProxyStatusAwaitingApproval, AuthURL: authURL}
	case backendStateRunning:
		return NodeEvent{Status: model.ProxyStatusRunning, URL: dnsName}
	default:
		return NodeEvent{Status: model.ProxyStatusError, ErrorMessage: "unknown state: " + backendState}
	}
}
