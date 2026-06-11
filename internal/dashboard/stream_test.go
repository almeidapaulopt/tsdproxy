// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package dashboard

import (
	"net/http"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/almeidapaulopt/tsdproxy/internal/model"
)

// -- sseClient.send -----------------------------------------------------------

func TestSSEClient_SendSuccess(t *testing.T) {
	t.Parallel()

	client := &sseClient{
		log:     zerolog.Nop(),
		channel: make(chan SSEMessage, chanSizeSSEQueue),
		done:    make(chan struct{}),
		userID:  "user1",
		connID:  "conn1",
	}

	msg := SSEMessage{Type: EventNotify, Message: "hello"}
	if !client.send(msg) {
		t.Fatal("expected send to succeed")
	}

	got := <-client.channel
	if got.Type != EventNotify || got.Message != "hello" {
		t.Fatalf("unexpected message: %+v", got)
	}
}

func TestSSEClient_SendBufferFull(t *testing.T) {
	t.Parallel()

	// Create a channel with capacity 1 so it fills immediately.
	client := &sseClient{
		log:     zerolog.Nop(),
		channel: make(chan SSEMessage, 1),
		done:    make(chan struct{}),
		userID:  "user1",
		connID:  "conn1",
	}

	// Fill the buffer.
	client.send(SSEMessage{Type: EventNotify, Message: "first"})

	// Next send should be dropped (returns false).
	if client.send(SSEMessage{Type: EventNotify, Message: "second"}) {
		t.Fatal("expected send to fail when buffer full")
	}
}

func TestSSEClient_SendAfterDone_FullBuffer(t *testing.T) {
	t.Parallel()

	ch := make(chan SSEMessage, 1)
	client := &sseClient{
		log:     zerolog.Nop(),
		channel: ch,
		done:    make(chan struct{}),
		userID:  "user1",
		connID:  "conn1",
	}

	close(client.done)

	// Fill the buffer so the only ready case in send() is <-c.done.
	ch <- SSEMessage{Type: EventNotify, Message: "filler"}

	if client.send(SSEMessage{Type: EventNotify, Message: "hello"}) {
		t.Fatal("expected send to fail after done closed with full buffer")
	}
}

// -- Dashboard.removeSSEClient -----------------------------------------------

func newTestDashboard(clients map[string]*sseClient) *Dashboard {
	if clients == nil {
		clients = make(map[string]*sseClient)
	}
	return &Dashboard{
		sseClients:      clients,
		lastHealthState: make(map[string]string),
		stopCh:          make(chan struct{}),
		Log:             zerolog.Nop(),
	}
}

func TestRemoveSSEClient_Existing(t *testing.T) {
	t.Parallel()

	done := make(chan struct{})
	clients := map[string]*sseClient{
		"conn1": {
			log:     zerolog.Nop(),
			channel: make(chan SSEMessage, 1),
			done:    done,
			connID:  "conn1",
		},
	}
	dash := newTestDashboard(clients)

	dash.removeSSEClient("conn1")

	if _, ok := dash.sseClients["conn1"]; ok {
		t.Fatal("expected client to be removed")
	}

	// The done channel should be closed.
	select {
	case <-done:
		// ok
	default:
		t.Fatal("expected done channel to be closed")
	}
}

func TestRemoveSSEClient_NonExistent(t *testing.T) {
	t.Parallel()

	dash := newTestDashboard(nil)

	// Should not panic.
	dash.removeSSEClient("nonexistent")

	if len(dash.sseClients) != 0 {
		t.Fatal("expected no clients")
	}
}

// -- Dashboard.snapshotClients ------------------------------------------------

func TestSnapshotClients_Empty(t *testing.T) {
	t.Parallel()

	dash := newTestDashboard(nil)

	snapshot := dash.snapshotClients()
	if len(snapshot) != 0 {
		t.Fatalf("expected 0 clients, got %d", len(snapshot))
	}
}

func TestSnapshotClients_Multiple(t *testing.T) {
	t.Parallel()

	clients := map[string]*sseClient{
		"conn1": {
			log:     zerolog.Nop(),
			channel: make(chan SSEMessage, 1),
			done:    make(chan struct{}),
			userID:  "user1",
			connID:  "conn1",
			isAdmin: false,
		},
		"conn2": {
			log:     zerolog.Nop(),
			channel: make(chan SSEMessage, 1),
			done:    make(chan struct{}),
			userID:  "user2",
			connID:  "conn2",
			isAdmin: true,
		},
	}
	dash := newTestDashboard(clients)

	snapshot := dash.snapshotClients()
	if len(snapshot) != 2 {
		t.Fatalf("expected 2 clients, got %d", len(snapshot))
	}

	// Verify fields are captured.
	found := map[string]bool{}
	for _, ci := range snapshot {
		found[ci.userID] = true
		if ci.userID == "user2" && !ci.isAdmin {
			t.Fatal("expected user2 to be admin")
		}
	}
	if !found["user1"] || !found["user2"] {
		t.Fatal("expected both users in snapshot")
	}
}

// -- Dashboard.updateClientSearch ---------------------------------------------

func TestUpdateClientSearch_AllConnectionsForUser(t *testing.T) {
	t.Parallel()

	clients := map[string]*sseClient{
		"conn1": {
			log:     zerolog.Nop(),
			channel: make(chan SSEMessage, 1),
			done:    make(chan struct{}),
			userID:  "user1",
			connID:  "conn1",
		},
		"conn2": {
			log:     zerolog.Nop(),
			channel: make(chan SSEMessage, 1),
			done:    make(chan struct{}),
			userID:  "user1",
			connID:  "conn2",
		},
		"conn3": {
			log:     zerolog.Nop(),
			channel: make(chan SSEMessage, 1),
			done:    make(chan struct{}),
			userID:  "user2",
			connID:  "conn3",
		},
	}
	dash := newTestDashboard(clients)

	// Empty connID: update all connections for user1.
	dash.updateClientSearch("user1", "", "nginx")

	for id, c := range dash.sseClients {
		c.mtx.Lock()
		search := c.search
		c.mtx.Unlock()

		if c.userID == "user1" {
			if search != "nginx" {
				t.Fatalf("conn %s: expected search 'nginx', got %q", id, search)
			}
		} else if search != "" {
			t.Fatalf("conn %s: expected empty search, got %q", id, search)
		}
	}
}

func TestUpdateClientSearch_SpecificConnection(t *testing.T) {
	t.Parallel()

	clients := map[string]*sseClient{
		"conn1": {
			log:     zerolog.Nop(),
			channel: make(chan SSEMessage, 1),
			done:    make(chan struct{}),
			userID:  "user1",
			connID:  "conn1",
		},
		"conn2": {
			log:     zerolog.Nop(),
			channel: make(chan SSEMessage, 1),
			done:    make(chan struct{}),
			userID:  "user1",
			connID:  "conn2",
		},
	}
	dash := newTestDashboard(clients)

	// Update only conn1 for user1.
	dash.updateClientSearch("user1", "conn1", "redis")

	for id, c := range dash.sseClients {
		c.mtx.Lock()
		search := c.search
		c.mtx.Unlock()

		if id == "conn1" {
			if search != "redis" {
				t.Fatalf("conn1: expected search 'redis', got %q", search)
			}
		} else if search != "" {
			t.Fatalf("conn %s: expected empty search, got %q", id, search)
		}
	}
}

func TestUpdateClientSearch_NoMatch(t *testing.T) {
	t.Parallel()

	clients := map[string]*sseClient{
		"conn1": {
			log:     zerolog.Nop(),
			channel: make(chan SSEMessage, 1),
			done:    make(chan struct{}),
			userID:  "user1",
			connID:  "conn1",
		},
	}
	dash := newTestDashboard(clients)

	// Different user: no update.
	dash.updateClientSearch("other", "conn1", "test")

	c := dash.sseClients["conn1"]
	c.mtx.Lock()
	search := c.search
	c.mtx.Unlock()

	if search != "" {
		t.Fatalf("expected no update, got %q", search)
	}
}

// -- logStreamer.drainBatch ---------------------------------------------------

func TestDrainBatch_EmptyAfterFirst(t *testing.T) {
	t.Parallel()

	ch := make(chan string, 1)
	streamer := &logStreamer{}

	lines := streamer.drainBatch(ch, "first")
	if len(lines) != 1 || lines[0] != "first" {
		t.Fatalf("expected [first], got %v", lines)
	}
}

func TestDrainBatch_MultipleLines(t *testing.T) {
	t.Parallel()

	ch := make(chan string, 10)
	streamer := &logStreamer{}

	// Pre-fill channel with multiple lines.
	for i := 0; i < 5; i++ { //nolint:mnd
		ch <- "line"
	}

	lines := streamer.drainBatch(ch, "first")
	// first + whatever was available in the channel.
	if len(lines) < 1 {
		t.Fatal("expected at least 1 line")
	}
	if lines[0] != "first" {
		t.Fatalf("expected first line 'first', got %q", lines[0])
	}
}

func TestDrainBatch_MaxBatchSize(t *testing.T) {
	t.Parallel()

	// Fill channel with more than maxBatchSize (50) messages.
	const totalLines = 100
	ch := make(chan string, totalLines)
	streamer := &logStreamer{}

	for i := 0; i < totalLines; i++ {
		ch <- "line"
	}

	lines := streamer.drainBatch(ch, "first")

	// Should be capped at maxBatchSize (50).
	if len(lines) > 50 {
		t.Fatalf("expected max 50 lines, got %d", len(lines))
	}
}

func TestDrainBatch_ClosedChannel(t *testing.T) {
	t.Parallel()

	ch := make(chan string, 1)
	streamer := &logStreamer{}

	ch <- "line1"
	close(ch)

	lines := streamer.drainBatch(ch, "first")
	if len(lines) < 1 {
		t.Fatal("expected at least 1 line")
	}
}

// -- setupSSEHeaders ----------------------------------------------------------

func TestSetupSSEHeaders_WithFlusher(t *testing.T) {
	t.Parallel()

	w := newMockResponseWriter()
	if !setupSSEHeaders(w) {
		t.Fatal("expected true when ResponseWriter implements Flusher")
	}

	ct := w.Header().Get("Content-Type")
	if ct != "text/event-stream" {
		t.Fatalf("expected text/event-stream, got %s", ct)
	}
	cc := w.Header().Get("Cache-Control")
	if cc != "no-cache" {
		t.Fatalf("expected no-cache, got %s", cc)
	}
	conn := w.Header().Get("Connection")
	if conn != "keep-alive" {
		t.Fatalf("expected keep-alive, got %s", conn)
	}
}

// noFlusher is a ResponseWriter that does NOT implement http.Flusher.
type noFlusher struct {
	header http.Header
}

func (n *noFlusher) Header() http.Header         { return n.header }
func (n *noFlusher) Write(p []byte) (int, error) { return len(p), nil }
func (n *noFlusher) WriteHeader(_ int)           {}

func TestSetupSSEHeaders_NoFlusher(t *testing.T) {
	t.Parallel()

	w := &noFlusher{header: make(http.Header)}
	if setupSSEHeaders(w) {
		t.Fatal("expected false when ResponseWriter does not implement Flusher")
	}
}

// -- newLogStreamer -----------------------------------------------------------

func TestNewLogStreamer(t *testing.T) {
	t.Parallel()

	s := newLogStreamer(nil, "my-proxy")

	if s.safeID == "" {
		t.Fatal("expected non-empty safeID")
	}
	if s.selector != "#log-lines-"+s.safeID {
		t.Fatalf("expected selector #log-lines-%s, got %s", s.safeID, s.selector)
	}
	if s.maxLines == "" {
		t.Fatal("expected non-empty maxLines")
	}
}

// -- Dashboard.sendStatusNotification -----------------------------------------

func TestSendStatusNotification_Stopped(t *testing.T) {
	t.Parallel()

	dash := newTestDashboard(nil)
	client := &sseClient{
		log:     zerolog.Nop(),
		channel: make(chan SSEMessage, 1),
		done:    make(chan struct{}),
		connID:  "c1",
	}

	event := model.ProxyEvent{ID: "proxy1", Status: model.ProxyStatusStopped}
	dash.sendStatusNotification(client, event)

	msg := <-client.channel
	if msg.Type != EventNotify {
		t.Fatalf("expected EventNotify, got %v", msg.Type)
	}
	if msg.Message != "proxy1\x00Stopped" {
		t.Fatalf("unexpected message: %q", msg.Message)
	}
}

func TestSendStatusNotification_Error(t *testing.T) {
	t.Parallel()

	dash := newTestDashboard(nil)
	client := &sseClient{
		log:     zerolog.Nop(),
		channel: make(chan SSEMessage, 1),
		done:    make(chan struct{}),
		connID:  "c1",
	}

	event := model.ProxyEvent{ID: "proxy2", Status: model.ProxyStatusError}
	dash.sendStatusNotification(client, event)

	msg := <-client.channel
	if msg.Type != EventNotify {
		t.Fatalf("expected EventNotify, got %v", msg.Type)
	}
	if msg.Message != "proxy2\x00Error" {
		t.Fatalf("unexpected message: %q", msg.Message)
	}
}

func TestSendStatusNotification_RunningNoNotification(t *testing.T) {
	t.Parallel()

	dash := newTestDashboard(nil)
	client := &sseClient{
		log:     zerolog.Nop(),
		channel: make(chan SSEMessage, 1),
		done:    make(chan struct{}),
		connID:  "c1",
	}

	// Running status should not send a notification.
	event := model.ProxyEvent{ID: "proxy1", Status: model.ProxyStatusRunning}
	dash.sendStatusNotification(client, event)

	select {
	case msg := <-client.channel:
		t.Fatalf("expected no notification for Running, got %+v", msg)
	default:
		// ok — no message sent.
	}
}

// -- Dashboard resyncAllClients / refreshClientCards coverage -----------------
// These methods depend on proxymanager.ProxyManager and are tested through
// the integration path. The client management helpers above cover the
// core logic.

// -- clientInfo snapshot includes search ---------------------------------------

func TestSnapshotClients_CapturesSearch(t *testing.T) {
	t.Parallel()

	clients := map[string]*sseClient{
		"conn1": {
			log:     zerolog.Nop(),
			channel: make(chan SSEMessage, 1),
			done:    make(chan struct{}),
			userID:  "user1",
			connID:  "conn1",
			search:  "myapp",
		},
	}
	dash := newTestDashboard(clients)

	snapshot := dash.snapshotClients()
	if len(snapshot) != 1 {
		t.Fatalf("expected 1 client, got %d", len(snapshot))
	}
	if snapshot[0].search != "myapp" {
		t.Fatalf("expected search 'myapp', got %q", snapshot[0].search)
	}
}

// -- concurrent access safety --------------------------------------------------

func TestRemoveSSEClient_ConcurrentWithSnapshot(t *testing.T) {
	t.Parallel()

	done1 := make(chan struct{})
	clients := map[string]*sseClient{
		"conn1": {
			log:     zerolog.Nop(),
			channel: make(chan SSEMessage, 1),
			done:    done1,
			userID:  "user1",
			connID:  "conn1",
		},
	}
	dash := newTestDashboard(clients)

	// Snapshot and remove concurrently — should not race.
	go dash.snapshotClients()
	go dash.removeSSEClient("conn1")

	// Wait for done channel to be closed (removeSSEClient ran).
	select {
	case <-done1:
		// ok
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for client removal")
	}
}
