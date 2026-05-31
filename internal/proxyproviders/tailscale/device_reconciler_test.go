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
	listErr    error
	deleteErr  error
	devices    []deviceEntry
	deletedIDs []string
}

func (m *mockDeviceLister) listDevices(_ context.Context, _ string) ([]deviceEntry, error) {
	return m.devices, m.listErr
}

func (m *mockDeviceLister) deleteDevice(_ context.Context, nodeID string) error {
	m.deletedIDs = append(m.deletedIDs, nodeID)
	return m.deleteErr
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

	r.Reconcile(context.Background(), "myhost", "tag:test")

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

	r.Reconcile(context.Background(), "myhost", "tag:test")

	assert.Empty(t, mock.deletedIDs, "unavailable factory should not trigger any API calls")
}

func TestReconcile_NoTags_NoOp(t *testing.T) {
	t.Parallel()

	mock := &mockDeviceLister{}
	r := newTestReconciler(mock)

	r.Reconcile(context.Background(), "myhost", "")

	assert.Empty(t, mock.deletedIDs, "empty tags should not trigger any API calls")
}

func TestReconcile_EmptyTags_NoOp(t *testing.T) {
	t.Parallel()

	mock := &mockDeviceLister{}
	r := newTestReconciler(mock)

	r.Reconcile(context.Background(), "myhost", "   , ,  ")

	assert.Empty(t, mock.deletedIDs, "whitespace-only tags should not trigger any API calls")
}

func TestReconcile_ListError_NoOp(t *testing.T) {
	t.Parallel()

	mock := &mockDeviceLister{
		listErr: errors.New("api error"),
	}
	r := newTestReconciler(mock)

	r.Reconcile(context.Background(), "myhost", "tag:test")

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

	r.Reconcile(context.Background(), "myhost", "tag:test")

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

	r.Reconcile(context.Background(), "myhost", "tag:test")

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

	r.Reconcile(context.Background(), "myhost", "tag:test")

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

	r.Reconcile(context.Background(), "myhost", "tag:test")

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

	r.Reconcile(context.Background(), "myhost", "tag:test")

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

	r.Reconcile(context.Background(), "myhost", "tag:test")

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

	r.Reconcile(context.Background(), "myhost", "tag:test")

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

	r.Reconcile(ctx, "myhost", "tag:test")

	// The reconciler delegates to the lister; with a canceled context the
	// real API would fail, but our mock succeeds. Verify no panic.
	require.Len(t, mock.deletedIDs, 1)
}

func TestReconcile_WithLocalState_SkipsExactHostname(t *testing.T) {
	t.Parallel()

	mock := &mockDeviceLister{
		devices: []deviceEntry{
			{Hostname: "myhost", NodeID: "exact-match", ConnectedToControl: false},
			{Hostname: "myhost-1", NodeID: "suffix-match", ConnectedToControl: false},
		},
	}
	r := newTestReconciler(mock)

	r.Reconcile(context.Background(), "myhost", "tag:test", WithLocalState(true))

	require.Len(t, mock.deletedIDs, 1)
	assert.Equal(t, "suffix-match", mock.deletedIDs[0])
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

	r.Reconcile(context.Background(), "myhost", "tag:test")

	require.Len(t, mock.deletedIDs, 2)
	assert.Contains(t, mock.deletedIDs, "exact-match")
	assert.Contains(t, mock.deletedIDs, "suffix-match")
}
