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

func TestReconcile_WithLocalState_OfflineExactHostnameDeleted(t *testing.T) {
	t.Parallel()

	mock := &mockDeviceLister{
		devices: []deviceEntry{
			{Hostname: "myhost", NodeID: "exact-offline", ConnectedToControl: false},
			{Hostname: "myhost-1", NodeID: "suffix-offline", ConnectedToControl: false},
		},
	}
	r := newTestReconciler(mock)

	r.Reconcile(context.Background(), "myhost", "tag:test", nil, WithLocalState(true))

	require.Len(t, mock.deletedIDs, 2)
	assert.Contains(t, mock.deletedIDs, "exact-offline")
	assert.Contains(t, mock.deletedIDs, "suffix-offline")
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
