// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package tailscale

import (
	"context"
	"strings"
	"unicode"

	"github.com/rs/zerolog"
	tailscale "tailscale.com/client/tailscale/v2"
)

// deviceEntry is a simplified representation of a Tailscale device,
// used internally to decouple reconciliation logic from the API types.
type deviceEntry struct {
	Hostname           string
	NodeID             string
	ConnectedToControl bool
}

// deviceLister abstracts Tailscale device listing and deletion for testability.
type deviceLister interface {
	listDevices(ctx context.Context, tag string) ([]deviceEntry, error)
	deleteDevice(ctx context.Context, nodeID string) error
}

// realDeviceAPI adapts the Tailscale API client to the deviceLister interface.
type realDeviceAPI struct {
	client *tailscale.Client
}

func (a *realDeviceAPI) listDevices(ctx context.Context, tag string) ([]deviceEntry, error) {
	devices, err := a.client.Devices().List(ctx, tailscale.WithFilter("tags", []string{tag}))
	if err != nil {
		return nil, err
	}
	entries := make([]deviceEntry, len(devices))
	for i, d := range devices {
		entries[i] = deviceEntry{
			Hostname:           d.Hostname,
			NodeID:             d.NodeID,
			ConnectedToControl: d.ConnectedToControl,
		}
	}
	return entries, nil
}

func (a *realDeviceAPI) deleteDevice(ctx context.Context, nodeID string) error {
	return a.client.Devices().Delete(ctx, nodeID)
}

// DeviceReconciler handles detection and cleanup of duplicate/stale Tailscale
// devices. It centralizes the logic used by all three proxy modes (per-proxy,
// shared, services) to prevent the "-1" suffix duplication problem.
type DeviceReconciler struct {
	log        zerolog.Logger
	apiFactory *APIClientFactory
	lister     deviceLister // nil in production; set in tests for mocking
}

// NewDeviceReconciler creates a new DeviceReconciler.
func NewDeviceReconciler(log zerolog.Logger, apiFactory *APIClientFactory) *DeviceReconciler {
	return &DeviceReconciler{
		log:        log,
		apiFactory: apiFactory,
	}
}

// listerForReconcile returns the deviceLister to use for reconciliation.
// In tests, an injected lister takes priority. In production, it creates
// a realDeviceAPI from the APIClientFactory.
func (r *DeviceReconciler) listerForReconcile() deviceLister {
	if r.lister != nil {
		return r.lister
	}
	client := r.apiFactory.NewClient(ScopesPerProxy()...)
	return &realDeviceAPI{client: client}
}

type reconcileOpts struct {
	force         bool
	hasLocalState bool
}

// ReconcileOption configures the behavior of Reconcile.
type ReconcileOption func(*reconcileOpts)

// WithForceClean forces deletion of online devices (used when state was
// completely regenerated).
func WithForceClean() ReconcileOption {
	return func(o *reconcileOpts) { o.force = true }
}

// WithLocalState tells Reconcile that local tsnet state exists, so the
// exact-hostname device should be preserved (tsnet will re-authenticate
// with existing state).
func WithLocalState(v bool) ReconcileOption {
	return func(o *reconcileOpts) { o.hasLocalState = v }
}

// Reconcile checks for and removes offline Tailscale devices with the same
// hostname, preventing duplicate machines. Online devices are never deleted.
// No-op when API is unavailable or tags are empty.
// onConflict is called for each online duplicate that cannot be cleaned (may be nil).
func (r *DeviceReconciler) Reconcile(ctx context.Context, hostname string, tags string, onConflict func(hostname, nodeID string), opts ...ReconcileOption) {
	if r.apiFactory == nil || !r.apiFactory.IsAvailable() {
		return
	}

	cleanedTags := cleanTags(tags)
	if len(cleanedTags) == 0 {
		return
	}

	var cfg reconcileOpts
	for _, o := range opts {
		o(&cfg)
	}

	lister := r.listerForReconcile()

	devices, err := lister.listDevices(ctx, cleanedTags[0])
	if err != nil {
		r.log.Warn().Err(err).Msg("failed to list tailnet devices, skipping stale device cleanup")
		return
	}

	for _, d := range devices {
		if d.Hostname == hostname {
			// When local tsnet state exists, the exact-hostname device is likely
			// our own node from a previous run — tsnet will re-authenticate with
			// existing state. Deleting it forces unnecessary re-auth.
			if cfg.hasLocalState {
				continue
			}
		} else {
			// Tailscale appends "-N" to device hostnames when a duplicate exists
			// (e.g. "app", "app-1", "app-2"). Only match that pattern — not arbitrary
			// prefixed names like "app-prod" or "app-staging".
			if !strings.HasPrefix(d.Hostname, hostname+"-") {
				continue
			}
			suffix := d.Hostname[len(hostname)+1:]
			if !isNumeric(suffix) {
				continue
			}
		}
		if d.ConnectedToControl && !cfg.force {
			r.log.Warn().
				Str("hostname", d.Hostname).
				Str("node_id", d.NodeID).
				Msg("device with same hostname is currently online, skipping cleanup")
			if onConflict != nil {
				onConflict(d.Hostname, d.NodeID)
			}
			continue
		}

		r.log.Info().
			Str("hostname", d.Hostname).
			Str("node_id", d.NodeID).
			Bool("was_online", d.ConnectedToControl).
			Msg("removing device from tailnet to prevent duplicate")

		if err := lister.deleteDevice(ctx, d.NodeID); err != nil {
			r.log.Error().Err(err).
				Str("hostname", d.Hostname).
				Msg("failed to delete stale device")
		}
	}
}

// isNumeric reports whether s consists entirely of digits.
func isNumeric(s string) bool {
	for _, r := range s {
		if !unicode.IsDigit(r) {
			return false
		}
	}
	return len(s) > 0
}
