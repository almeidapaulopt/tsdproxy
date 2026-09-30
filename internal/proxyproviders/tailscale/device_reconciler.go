// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package tailscale

import (
	"context"
	"errors"
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
	getDevice(ctx context.Context, nodeID string) (deviceEntry, error)
}

// ErrDeviceNotFound is returned by getDevice when the device no longer exists.
var ErrDeviceNotFound = errors.New("device not found")

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

func (a *realDeviceAPI) getDevice(ctx context.Context, nodeID string) (deviceEntry, error) {
	d, err := a.client.Devices().Get(ctx, nodeID)
	if err != nil {
		return deviceEntry{}, err
	}
	return deviceEntry{
		Hostname:           d.Hostname,
		NodeID:             d.NodeID,
		ConnectedToControl: d.ConnectedToControl,
	}, nil
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
		r.processDevice(ctx, lister, d, hostname, cfg, onConflict)
	}
}

// processDevice handles a single device during reconciliation: checks if it
// matches the target hostname pattern, and deletes it if safe to do so.
func (r *DeviceReconciler) processDevice(
	ctx context.Context, lister deviceLister,
	d deviceEntry, hostname string, cfg reconcileOpts,
	onConflict func(hostname, nodeID string),
) {
	if !r.isDeviceMatch(d, hostname, cfg) {
		return
	}

	if d.ConnectedToControl && !cfg.force {
		r.log.Warn().
			Str("hostname", d.Hostname).
			Str("node_id", d.NodeID).
			Msg("device with same hostname is currently online, skipping cleanup")
		if onConflict != nil {
			onConflict(d.Hostname, d.NodeID)
		}
		return
	}

	// Re-fetch the device immediately before deletion to shrink the TOCTOU
	// window. A device that reconnected between the List snapshot and now
	// must not be deleted.
	current, err := lister.getDevice(ctx, d.NodeID)
	if err != nil {
		r.log.Warn().Err(err).Str("node_id", d.NodeID).
			Msg("device disappeared before delete, skipping")
		return
	}
	if current.ConnectedToControl && !cfg.force {
		r.log.Warn().
			Str("hostname", current.Hostname).
			Str("node_id", current.NodeID).
			Msg("device reconnected between list and delete, skipping cleanup")
		if onConflict != nil {
			onConflict(current.Hostname, current.NodeID)
		}
		return
	}

	r.log.Info().
		Str("hostname", current.Hostname).
		Str("node_id", current.NodeID).
		Bool("was_online", current.ConnectedToControl).
		Msg("removing device from tailnet to prevent duplicate")

	if err := lister.deleteDevice(ctx, current.NodeID); err != nil {
		r.log.Error().Err(err).
			Str("hostname", current.Hostname).
			Msg("failed to delete stale device")
	}
}

// isDeviceMatch reports whether the device is a candidate for cleanup:
// either an exact hostname match (skipped if local state exists and device is online),
// or a Tailscale "-N" suffix duplicate.
func (r *DeviceReconciler) isDeviceMatch(d deviceEntry, hostname string, cfg reconcileOpts) bool {
	if d.Hostname == hostname {
		// When local tsnet state exists AND the exact-hostname device is
		// still online, it's our own node from a previous run — tsnet will
		// re-authenticate with existing state. Deleting it would force
		// unnecessary re-auth.
		//
		// But if the device is offline, the auth state may be stale and
		// tsnet may not successfully re-authenticate, causing Tailscale
		// to append a "-1" suffix. Delete offline exact-match devices
		// so the new node can claim the hostname cleanly.
		return !cfg.hasLocalState || !d.ConnectedToControl
	}

	// Tailscale appends "-N" to device hostnames when a duplicate exists
	// (e.g. "app", "app-1", "app-2"). Only match that pattern — not arbitrary
	// prefixed names like "app-prod" or "app-staging".
	if !strings.HasPrefix(d.Hostname, hostname+"-") {
		return false
	}
	suffix := d.Hostname[len(hostname)+1:]
	return isNumeric(suffix)
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
