// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package tailscale

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"golang.org/x/sync/semaphore"
	"tailscale.com/tsnet"

	"github.com/almeidapaulopt/tsdproxy/internal/model"
)

// NodeLifecycleConfig holds the configuration for creating a NodeLifecycle.
type NodeLifecycleConfig struct {
	CertSem          *semaphore.Weighted
	AuthManager      *AuthManager
	StateManager     *StateManager
	DeviceReconciler *DeviceReconciler
	AuthConfig       AuthConfig
	NodeConfig       NodeConfig
	Retry            RetryPolicy
}

// NodeLifecycleProvider creates a NodeLifecycle, starts it, and returns the
// lifecycle, runtime, and an optional serviceListenerFactory (non-nil for
// services mode). It is the seam through which tests inject stubs.
type NodeLifecycleProvider func(ctx context.Context, log zerolog.Logger, cfg NodeLifecycleConfig) (
	lifecycle *NodeLifecycle, runtime *NodeRuntime, factory serviceListenerFactory, err error,
)

// DefaultNodeLifecycleProvider is the production NodeLifecycleProvider.
// It creates a NodeLifecycle, starts the tsnet.Server, and returns the runtime
// with a tsnetServerFactory wrapping the server.
func DefaultNodeLifecycleProvider(ctx context.Context, log zerolog.Logger, cfg NodeLifecycleConfig) (
	*NodeLifecycle, *NodeRuntime, serviceListenerFactory, error,
) {
	lc := NewNodeLifecycle(log, cfg)
	rt, err := lc.Start(ctx)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("node lifecycle: %w", err)
	}
	return lc, rt, tsnetServerFactory{rt.Server}, nil
}

// NodeLifecycle manages the full lifecycle of a Tailscale node:
// startup, status watching, shutdown, and cleanup.
type NodeLifecycle struct {
	log        zerolog.Logger
	devices    *DeviceReconciler
	certSem    *semaphore.Weighted
	auth       *AuthManager
	state      *StateManager
	events     chan NodeEvent
	runtime    *NodeRuntime
	watchDone  chan struct{}
	authCfg    AuthConfig
	cfg        NodeConfig
	retry      RetryPolicy
	mtx        sync.RWMutex
	eventsOnce sync.Once
}

// NewNodeLifecycle creates a new NodeLifecycle.
func NewNodeLifecycle(log zerolog.Logger, cfg NodeLifecycleConfig) *NodeLifecycle {
	return &NodeLifecycle{
		log:     log,
		cfg:     cfg.NodeConfig,
		authCfg: cfg.AuthConfig,
		certSem: cfg.CertSem,
		auth:    cfg.AuthManager,
		state:   cfg.StateManager,
		devices: cfg.DeviceReconciler,
		retry:   cfg.Retry,
		events:  make(chan NodeEvent, 64), //nolint:mnd
	}
}

// Start prepares state, reconciles devices, creates and starts tsnet.Server,
// gets LocalClient, starts StatusWatcher, and returns the NodeRuntime.
func (nl *NodeLifecycle) Start(ctx context.Context) (*NodeRuntime, error) {
	datadir := nl.cfg.DataDir

	// Resolve auth key early to determine if OAuth is in use.
	authKey, err := nl.auth.ResolveKey(ctx, nl.authCfg, nl.cfg.Tags)
	if err != nil {
		return nil, fmt.Errorf("node lifecycle: %w", err)
	}

	stateExists := nl.state.StateExists(datadir)
	if nl.state.CleanStale(&nl.cfg, datadir) {
		nl.log.Info().Msg("stale state cleaned, will create new node")
		stateExists = false
	}

	nl.state.Save(&nl.cfg, datadir)

	if stateExists {
		nl.log.Info().Msg("Reusing existing tsnet node")
	} else {
		nl.log.Info().Msg("Creating new tsnet node")
	}

	// Reconcile stale/offline device duplicates. Online devices are never
	// deleted automatically — only offline duplicates matching the hostname
	// pattern are cleaned up, regardless of whether state was regenerated.
	if nl.devices != nil {
		nl.sendEvent(NodeEvent{Status: model.ProxyStatusReconciling})
		reconcileCtx, cancel := context.WithTimeout(ctx, apiTimeout)
		nl.devices.Reconcile(reconcileCtx, nl.cfg.Hostname, nl.cfg.Tags,
			func(hostname, nodeID string) {
				nl.sendEvent(NodeEvent{
					Status:       model.ProxyStatusDeviceConflict,
					ErrorMessage: "online device with hostname " + hostname + " already exists (nodeID: " + nodeID + ")",
				})
			},
			WithLocalState(stateExists))
		cancel()
	}

	tsServer := &tsnet.Server{
		Hostname:      nl.cfg.Hostname,
		AuthKey:       authKey,
		Dir:           datadir,
		Ephemeral:     nl.cfg.Ephemeral,
		RunWebClient:  nl.cfg.RunWebClient,
		AdvertiseTags: nl.cfg.AdvertiseTags,
		ControlURL:    nl.cfg.ControlURL,
		UserLogf:      func(format string, args ...any) { nl.log.Info().Msgf(format, args...) },
		Logf:          func(format string, args ...any) { nl.log.Trace().Msgf(format, args...) },
	}

	if nl.cfg.Verbose {
		tsServer.Logf = func(format string, args ...any) { nl.log.Info().Msgf(format, args...) }
	}

	if startErr := nl.startWithRetry(ctx, tsServer); startErr != nil {
		tsServer.Close()
		return nil, fmt.Errorf("node lifecycle: start tsnet: %w", startErr)
	}

	lc, err := tsServer.LocalClient()
	if err != nil {
		tsServer.Close()
		return nil, fmt.Errorf("node lifecycle: get local client: %w", err)
	}

	// Runtime context must be independent of the startup context so the
	// StatusWatcher and bridge goroutines stay alive after startup completes.
	rtCtx, cancel := context.WithCancel(context.Background())

	rt := NewNodeRuntime(rtCtx, tsServer, lc, cancel)

	watchDone := make(chan struct{})
	watcher := NewStatusWatcher(StatusWatcherConfig{
		Log:     nl.log,
		OnEvent: nl.sendEvent,
		OnDone:  func() {},
	})

	nl.mtx.Lock()
	nl.runtime = rt
	nl.watchDone = watchDone
	nl.mtx.Unlock()

	go func() {
		defer close(watchDone)
		watcher.Watch(rtCtx, lc)
	}()

	return rt, nil
}

func (nl *NodeLifecycle) startWithRetry(ctx context.Context, tsServer *tsnet.Server) error {
	if nl.retry.MaxAttempts <= 0 {
		return tsServer.Start()
	}

	var lastErr error
	for attempt := 0; attempt < nl.retry.MaxAttempts; attempt++ {
		lastErr = tsServer.Start()
		if lastErr == nil {
			return nil
		}
		if !nl.retry.IsRecoverable(lastErr) {
			return lastErr
		}
		nl.log.Warn().Err(lastErr).Int("attempt", attempt+1).Int("max_attempts", nl.retry.MaxAttempts).
			Msg("tsnet start failed, retrying")

		// Exponential backoff: attempt 0 → initialBackoff, attempt 1 → initialBackoff*2, etc.
		backoff := nl.retry.InitialBackoff
		for i := 0; i < attempt; i++ {
			backoff *= 2
		}
		if backoff > nl.retry.MaxBackoff {
			backoff = nl.retry.MaxBackoff
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}
	}
	return fmt.Errorf("tsnet start failed after %d attempts: %w", nl.retry.MaxAttempts, lastErr)
}

// Close shuts down the node: stops watcher, closes tsnet, cleans ephemeral state.
func (nl *NodeLifecycle) Close() error {
	nl.mtx.Lock()
	rt := nl.runtime
	watchDone := nl.watchDone
	nl.mtx.Unlock()

	if rt == nil {
		nl.eventsOnce.Do(func() { close(nl.events) })
		return nil
	}

	rt.Cancel()

	if watchDone != nil {
		<-watchDone
	}

	err := rt.Close()

	if nl.cfg.Ephemeral && nl.cfg.DataDir != "" {
		if removeErr := os.RemoveAll(nl.cfg.DataDir); removeErr != nil {
			nl.log.Error().Err(removeErr).Msg("failed to clean up ephemeral node state")
		}
	}

	nl.eventsOnce.Do(func() { close(nl.events) })

	return err
}

// WatchEvents returns a channel of lifecycle events.
func (nl *NodeLifecycle) WatchEvents() chan NodeEvent {
	return nl.events
}

// GetRuntime returns the current NodeRuntime, or nil if not started.
func (nl *NodeLifecycle) GetRuntime() *NodeRuntime {
	nl.mtx.RLock()
	defer nl.mtx.RUnlock()
	return nl.runtime
}

func (nl *NodeLifecycle) sendEvent(evt NodeEvent) {
	select {
	case nl.events <- evt:
	default:
		nl.log.Warn().Msg("dropping lifecycle event: channel full")
	}
}
