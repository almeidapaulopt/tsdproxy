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
)

// --- mock deviceLister ---

type mockDeviceLister struct {
	listErr      error
	deleteErr    error
	getErr       error
	getOverrides map[string]deviceEntry
	devices      []deviceEntry
	deletedIDs   []string
}

func (m *mockDeviceLister) listDevices(_ context.Context, _ string) ([]deviceEntry, error) {
	return m.devices, m.listErr
}

func (m *mockDeviceLister) deleteDevice(_ context.Context, nodeID string) error {
	m.deletedIDs = append(m.deletedIDs, nodeID)
	return m.deleteErr
}

func (m *mockDeviceLister) getDevice(_ context.Context, nodeID string) (deviceEntry, error) {
	if m.getErr != nil {
		return deviceEntry{}, m.getErr
	}
	if override, ok := m.getOverrides[nodeID]; ok {
		return override, nil
	}
	for _, d := range m.devices {
		if d.NodeID == nodeID {
			return d, nil
		}
	}
	return deviceEntry{}, ErrDeviceNotFound
}

// --- helpers ---

func newTestReconciler(lister *mockDeviceLister) *DeviceReconciler {
	return &DeviceReconciler{
		log:        zerolog.Nop(),
		apiFactory: NewAPIClientFactory("test-id", "test-secret"),
		lister:     lister,
	}
}

// --- tests ---

func TestReconcile_NilFactory_NoOp(t *testing.T) {
	t.Parallel()

	mock := &mockDeviceLister{}
	r := &DeviceReconciler{
		log:        zerolog.Nop(),
		apiFactory: nil,
		lister:     mock,
	}

	r.Reconcile(context.Background(), "myhost", "tag:test", nil)

	assert.Empty(t, mock.deletedIDs, "nil factory should not trigger any API calls")
}

func TestReconcile_UnavailableFactory_NoOp(t *testing.T) {
	t.Parallel()

	mock := &mockDeviceLister{}
	r := &DeviceReconciler{
		log:        zerolog.Nop(),
		apiFactory: NewAPIClientFactory("", ""),
		lister:     mock,
	}

	r.Reconcile(context.Background(), "myhost", "tag:test", nil)

	assert.Empty(t, mock.deletedIDs, "unavailable factory should not trigger any API calls")
}

func TestReconcile_NoTags_NoOp(t *testing.T) {
	t.Parallel()

	mock := &mockDeviceLister{}
	r := newTestReconciler(mock)

	r.Reconcile(context.Background(), "myhost", "", nil)

	assert.Empty(t, mock.deletedIDs, "empty tags should not trigger any API calls")
}

func TestReconcile_EmptyTags_NoOp(t *testing.T) {
	t.Parallel()

	mock := &mockDeviceLister{}
	r := newTestReconciler(mock)

	r.Reconcile(context.Background(), "myhost", "   , ,  ", nil)

	assert.Empty(t, mock.deletedIDs, "whitespace-only tags should not trigger any API calls")
}

func TestReconcile_ListError_NoOp(t *testing.T) {
	t.Parallel()

	mock := &mockDeviceLister{
		listErr: errors.New("api error"),
	}
	r := newTestReconciler(mock)

	r.Reconcile(context.Background(), "myhost", "tag:test", nil)

	assert.Empty(t, mock.deletedIDs, "list error should not trigger any deletions")
}

func TestReconcile_NoMatchingHostname_NoOp(t *testing.T) {
	t.Parallel()

	mock := &mockDeviceLister{
		devices: []deviceEntry{
			{Hostname: "other-host", NodeID: "node-1", ConnectedToControl: false},
			{Hostname: "another-host", NodeID: "node-2", ConnectedToControl: false},
		},
	}
	r := newTestReconciler(mock)

	r.Reconcile(context.Background(), "myhost", "tag:test", nil)

	assert.Empty(t, mock.deletedIDs, "no matching hostnames should not trigger any deletions")
}

func TestReconcile_OnlineDevice_Skipped(t *testing.T) {
	t.Parallel()

	mock := &mockDeviceLister{
		devices: []deviceEntry{
			{Hostname: "myhost", NodeID: "node-1", ConnectedToControl: true},
		},
	}
	r := newTestReconciler(mock)

	r.Reconcile(context.Background(), "myhost", "tag:test", nil)

	assert.Empty(t, mock.deletedIDs, "online device should not be deleted")
}

func TestReconcile_OfflineDevice_Deleted(t *testing.T) {
	t.Parallel()

	mock := &mockDeviceLister{
		devices: []deviceEntry{
			{Hostname: "myhost", NodeID: "node-1", ConnectedToControl: false},
		},
	}
	r := newTestReconciler(mock)

	r.Reconcile(context.Background(), "myhost", "tag:test", nil)

	require.Len(t, mock.deletedIDs, 1)
	assert.Equal(t, "node-1", mock.deletedIDs[0])
}

func TestReconcile_MultipleDevices_OnlyOfflineDeleted(t *testing.T) {
	t.Parallel()

	mock := &mockDeviceLister{
		devices: []deviceEntry{
			{Hostname: "myhost", NodeID: "node-online", ConnectedToControl: true},
			{Hostname: "myhost", NodeID: "node-offline", ConnectedToControl: false},
			{Hostname: "other", NodeID: "node-other", ConnectedToControl: false},
		},
	}
	r := newTestReconciler(mock)

	r.Reconcile(context.Background(), "myhost", "tag:test", nil)

	require.Len(t, mock.deletedIDs, 1)
	assert.Equal(t, "node-offline", mock.deletedIDs[0])
}

func TestReconcile_DeleteError_LoggedButContinues(t *testing.T) {
	t.Parallel()

	mock := &mockDeviceLister{
		devices: []deviceEntry{
			{Hostname: "myhost", NodeID: "node-1", ConnectedToControl: false},
			{Hostname: "myhost", NodeID: "node-2", ConnectedToControl: false},
		},
		deleteErr: errors.New("delete failed"),
	}
	r := newTestReconciler(mock)

	r.Reconcile(context.Background(), "myhost", "tag:test", nil)

	require.Len(t, mock.deletedIDs, 2, "delete error should not stop processing remaining devices")
	assert.Equal(t, []string{"node-1", "node-2"}, mock.deletedIDs)
}

func TestReconcile_ListError_Logged(t *testing.T) {
	t.Parallel()

	listErr := errors.New("network timeout")
	mock := &mockDeviceLister{
		listErr: listErr,
	}
	r := newTestReconciler(mock)

	r.Reconcile(context.Background(), "myhost", "tag:test", nil)

	assert.Empty(t, mock.deletedIDs, "list error should prevent any deletions")
}

func TestReconcile_MultipleMatches_AllOfflineDeleted(t *testing.T) {
	t.Parallel()

	mock := &mockDeviceLister{
		devices: []deviceEntry{
			{Hostname: "myhost", NodeID: "stale-1", ConnectedToControl: false},
			{Hostname: "myhost", NodeID: "stale-2", ConnectedToControl: false},
			{Hostname: "myhost", NodeID: "stale-3", ConnectedToControl: false},
		},
	}
	r := newTestReconciler(mock)

	r.Reconcile(context.Background(), "myhost", "tag:test", nil)

	require.Len(t, mock.deletedIDs, 3)
	assert.Equal(t, []string{"stale-1", "stale-2", "stale-3"}, mock.deletedIDs)
}

func TestReconcile_ContextCancelled_NoPanic(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	mock := &mockDeviceLister{
		devices: []deviceEntry{
			{Hostname: "myhost", NodeID: "node-1", ConnectedToControl: false},
		},
	}
	r := newTestReconciler(mock)

	r.Reconcile(ctx, "myhost", "tag:test", nil)

	// The reconciler delegates to the lister; with a canceled context the
	// real API would fail, but our mock succeeds. Verify no panic.
	require.Len(t, mock.deletedIDs, 1)
}

func TestReconcile_WithLocalState_OnlineDeviceSkipped(t *testing.T) {
	t.Parallel()

	mock := &mockDeviceLister{
		devices: []deviceEntry{
			{Hostname: "myhost", NodeID: "exact-online", ConnectedToControl: true},
			{Hostname: "myhost-1", NodeID: "suffix-offline", ConnectedToControl: false},
		},
	}
	r := newTestReconciler(mock)

	r.Reconcile(context.Background(), "myhost", "tag:test", nil, WithLocalState(true))

	require.Len(t, mock.deletedIDs, 1)
	assert.Equal(t, "suffix-offline", mock.deletedIDs[0])
}

// With local state but no known node ID, an offline exact-hostname device may
// be this node itself: it is offline because tsdproxy is restarting. Deleting
// it invalidates the local state, and tsnet then resumes the deleted node
// (it ignores the auth key once state exists), so the proxy never comes back.
// Leaving a stale device costs at most a "-1" suffix, so it is preserved.
func TestReconcile_WithLocalState_OfflineExactHostnamePreserved(t *testing.T) {
	t.Parallel()

	mock := &mockDeviceLister{
		devices: []deviceEntry{
			{Hostname: "myhost", NodeID: "exact-offline", ConnectedToControl: false},
			{Hostname: "myhost-1", NodeID: "suffix-offline", ConnectedToControl: false},
		},
	}
	r := newTestReconciler(mock)

	r.Reconcile(context.Background(), "myhost", "tag:test", nil, WithLocalState(true))

	assert.Equal(t, []string{"suffix-offline"}, mock.deletedIDs)
}

func TestReconcile_WithoutLocalState_DeletesExactHostname(t *testing.T) {
	t.Parallel()

	mock := &mockDeviceLister{
		devices: []deviceEntry{
			{Hostname: "myhost", NodeID: "exact-match", ConnectedToControl: false},
			{Hostname: "myhost-1", NodeID: "suffix-match", ConnectedToControl: false},
		},
	}
	r := newTestReconciler(mock)

	r.Reconcile(context.Background(), "myhost", "tag:test", nil)

	require.Len(t, mock.deletedIDs, 2)
	assert.Contains(t, mock.deletedIDs, "exact-match")
	assert.Contains(t, mock.deletedIDs, "suffix-match")
}

func TestReconcile_WithForceClean_OnlineDevice_Deleted(t *testing.T) {
	t.Parallel()

	mock := &mockDeviceLister{
		devices: []deviceEntry{
			{Hostname: "myhost", NodeID: "node-online", ConnectedToControl: true},
		},
	}
	r := newTestReconciler(mock)

	r.Reconcile(context.Background(), "myhost", "tag:test", nil, WithForceClean())

	require.Len(t, mock.deletedIDs, 1)
	assert.Equal(t, "node-online", mock.deletedIDs[0])
}

func TestReconcile_WithForceClean_MixedDevices_AllMatchingDeleted(t *testing.T) {
	t.Parallel()

	mock := &mockDeviceLister{
		devices: []deviceEntry{
			{Hostname: "myhost", NodeID: "exact-online", ConnectedToControl: true},
			{Hostname: "myhost", NodeID: "exact-offline", ConnectedToControl: false},
			{Hostname: "myhost-1", NodeID: "suffix-online", ConnectedToControl: true},
			{Hostname: "myhost-1", NodeID: "suffix-offline", ConnectedToControl: false},
			{Hostname: "other", NodeID: "unrelated", ConnectedToControl: false},
		},
	}
	r := newTestReconciler(mock)

	r.Reconcile(context.Background(), "myhost", "tag:test", nil, WithForceClean())

	require.Len(t, mock.deletedIDs, 4)
	assert.Contains(t, mock.deletedIDs, "exact-online")
	assert.Contains(t, mock.deletedIDs, "exact-offline")
	assert.Contains(t, mock.deletedIDs, "suffix-online")
	assert.Contains(t, mock.deletedIDs, "suffix-offline")
}

func TestReconcile_WithForceClean_MirrorsNodeLifecycle(t *testing.T) {
	t.Parallel()

	mock := &mockDeviceLister{
		devices: []deviceEntry{
			{Hostname: "myhost", NodeID: "exact-online", ConnectedToControl: true},
			{Hostname: "myhost-1", NodeID: "suffix-online", ConnectedToControl: true},
			{Hostname: "myhost-1", NodeID: "suffix-offline", ConnectedToControl: false},
		},
	}
	r := newTestReconciler(mock)

	r.Reconcile(context.Background(), "myhost", "tag:test", nil, WithForceClean())

	require.Len(t, mock.deletedIDs, 3)
	assert.Contains(t, mock.deletedIDs, "exact-online")
	assert.Contains(t, mock.deletedIDs, "suffix-online")
	assert.Contains(t, mock.deletedIDs, "suffix-offline")
}

func TestReconcile_WithForceClean_OnConflictNotCalled(t *testing.T) {
	t.Parallel()

	conflictCalled := false
	onConflict := func(_, _ string) { conflictCalled = true }

	mock := &mockDeviceLister{
		devices: []deviceEntry{
			{Hostname: "myhost", NodeID: "node-online", ConnectedToControl: true},
		},
	}
	r := newTestReconciler(mock)

	r.Reconcile(context.Background(), "myhost", "tag:test", onConflict, WithForceClean())

	require.Len(t, mock.deletedIDs, 1)
	assert.False(t, conflictCalled, "onConflict must not fire when force-clean deletes the device")
}

func TestReconcile_DeviceReconnectsBetweenListAndGet_NotDeleted(t *testing.T) {
	t.Parallel()

	conflictCalled := false
	onConflict := func(_, _ string) { conflictCalled = true }

	mock := &mockDeviceLister{
		devices: []deviceEntry{
			{Hostname: "myhost", NodeID: "node-1", ConnectedToControl: false},
		},
		getOverrides: map[string]deviceEntry{
			"node-1": {Hostname: "myhost", NodeID: "node-1", ConnectedToControl: true},
		},
	}
	r := newTestReconciler(mock)

	r.Reconcile(context.Background(), "myhost", "tag:test", onConflict)

	assert.Empty(t, mock.deletedIDs, "device that reconnected between List and Get must not be deleted")
	assert.True(t, conflictCalled, "onConflict must fire so the caller learns the hostname is contested")
}

// --- WithSelfNodeID ---

// The node this tsdproxy instance holds state for is offline only because the
// instance is restarting. It must survive reconciliation; other offline
// duplicates are still removed.
func TestReconcile_WithSelfNodeID_OfflineExactSelfPreserved(t *testing.T) {
	t.Parallel()

	mock := &mockDeviceLister{
		devices: []deviceEntry{
			{Hostname: "myhost", NodeID: "self", ConnectedToControl: false},
			{Hostname: "myhost-1", NodeID: "stale", ConnectedToControl: false},
		},
	}
	r := newTestReconciler(mock)

	r.Reconcile(context.Background(), "myhost", "tag:test", nil,
		WithLocalState(true), WithSelfNodeID("self"))

	assert.Equal(t, []string{"stale"}, mock.deletedIDs)
}

// When this node registered as "myhost-1" because a stale "myhost" existed,
// the stale exact-hostname device is known not to be this node and is removed,
// while this node's own suffixed device is kept.
func TestReconcile_WithSelfNodeID_SuffixedSelfPreserved_StaleExactDeleted(t *testing.T) {
	t.Parallel()

	mock := &mockDeviceLister{
		devices: []deviceEntry{
			{Hostname: "myhost", NodeID: "stale", ConnectedToControl: false},
			{Hostname: "myhost-1", NodeID: "self", ConnectedToControl: false},
		},
	}
	r := newTestReconciler(mock)

	r.Reconcile(context.Background(), "myhost", "tag:test", nil,
		WithLocalState(true), WithSelfNodeID("self"))

	assert.Equal(t, []string{"stale"}, mock.deletedIDs)
}

// An online exact-hostname device that is not this node is a real conflict,
// even with local state: report it rather than assume it is this node.
func TestReconcile_WithSelfNodeID_OnlineOtherExact_ReportsConflict(t *testing.T) {
	t.Parallel()

	var conflicts []string
	onConflict := func(_, nodeID string) { conflicts = append(conflicts, nodeID) }

	mock := &mockDeviceLister{
		devices: []deviceEntry{
			{Hostname: "myhost", NodeID: "other", ConnectedToControl: true},
		},
	}
	r := newTestReconciler(mock)

	r.Reconcile(context.Background(), "myhost", "tag:test", onConflict,
		WithLocalState(true), WithSelfNodeID("self"))

	assert.Empty(t, mock.deletedIDs)
	assert.Equal(t, []string{"other"}, conflicts)
}

// Force clean must never remove this node either.
func TestReconcile_WithSelfNodeID_ForceClean_SelfPreserved(t *testing.T) {
	t.Parallel()

	mock := &mockDeviceLister{
		devices: []deviceEntry{
			{Hostname: "myhost", NodeID: "self", ConnectedToControl: false},
			{Hostname: "myhost-1", NodeID: "other", ConnectedToControl: true},
		},
	}
	r := newTestReconciler(mock)

	r.Reconcile(context.Background(), "myhost", "tag:test", nil,
		WithForceClean(), WithSelfNodeID("self"))

	assert.Equal(t, []string{"other"}, mock.deletedIDs)
}

// --- isNumeric ---

func TestIsNumeric_Valid(t *testing.T) {
	t.Parallel()

	if !isNumeric("123") {
		t.Fatal("isNumeric('123') should return true")
	}
	if !isNumeric("0") {
		t.Fatal("isNumeric('0') should return true")
	}
	if !isNumeric("999999") {
		t.Fatal("isNumeric('999999') should return true")
	}
}

func TestIsNumeric_Empty(t *testing.T) {
	t.Parallel()

	if isNumeric("") {
		t.Fatal("isNumeric('') should return false")
	}
}

func TestIsNumeric_Invalid(t *testing.T) {
	t.Parallel()

	if isNumeric("123a") {
		t.Fatal("isNumeric('123a') should return false")
	}
	if isNumeric("-1") {
		t.Fatal("isNumeric('-1') should return false")
	}
	if isNumeric("12.3") {
		t.Fatal("isNumeric('12.3') should return false")
	}
	if isNumeric("abc") {
		t.Fatal("isNumeric('abc') should return false")
	}
}

// --- listerForReconcile ---

func TestListerForReconcile_WithInjectedLister(t *testing.T) {
	t.Parallel()

	mock := &mockDeviceLister{}
	r := newTestReconciler(mock)
	lister := r.listerForReconcile()
	if lister != mock {
		t.Error("listerForReconcile() should return the injected lister")
	}
}

// --- Reconcile ---

func TestReconcile_DeviceDisappearsBetweenListAndGet_NotDeleted(t *testing.T) {
	t.Parallel()

	mock := &mockDeviceLister{
		devices: []deviceEntry{
			{Hostname: "myhost", NodeID: "node-1", ConnectedToControl: false},
		},
		getErr: ErrDeviceNotFound,
	}
	r := newTestReconciler(mock)

	r.Reconcile(context.Background(), "myhost", "tag:test", nil)

	assert.Empty(t, mock.deletedIDs, "device that vanished between List and Get must not be deleted")
}
