// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package proxymanager

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

func newTestBuffer(t *testing.T, size int) *LogRingBuffer {
	t.Helper()
	return NewLogRingBuffer(zerolog.Nop(), size)
}

func TestLogRingBufferBasicWrite(t *testing.T) {
	t.Parallel()
	b := newTestBuffer(t, DefaultLogBufferSize)

	_, err := b.Write([]byte("hello\n"))
	if err != nil {
		t.Fatalf("Write returned error: %v", err)
	}

	snap, _ := b.SubscribeWithSnapshot()
	if len(snap) != 1 {
		t.Fatalf("expected 1 line in snapshot, got %d", len(snap))
	}
	if snap[0] != "hello" {
		t.Fatalf("expected %q, got %q", "hello", snap[0])
	}
}

func TestLogRingBufferWraparound(t *testing.T) {
	t.Parallel()
	const smallSize = 5
	b := newTestBuffer(t, smallSize)

	for i := range 10 {
		_, _ = b.Write([]byte("line-" + string(rune('0'+i))))
	}

	snap, _ := b.SubscribeWithSnapshot()
	if len(snap) != smallSize {
		t.Fatalf("expected %d lines, got %d", smallSize, len(snap))
	}
	// After wrapping with 10 writes into size-5 buffer, we expect lines 5..9
	for i, want := range []string{"line-5", "line-6", "line-7", "line-8", "line-9"} {
		if snap[i] != want {
			t.Errorf("snap[%d] = %q, want %q", i, snap[i], want)
		}
	}
}

func TestLogRingBufferSnapshotOrdering(t *testing.T) {
	t.Parallel()
	b := newTestBuffer(t, DefaultLogBufferSize)

	for i := range 5 {
		_, _ = b.Write([]byte(strings.Repeat("x", i+1)))
	}

	snap, _ := b.SubscribeWithSnapshot()
	if len(snap) != 5 {
		t.Fatalf("expected 5 lines, got %d", len(snap))
	}
	expected := []string{"x", "xx", "xxx", "xxxx", "xxxxx"}
	for i, want := range expected {
		if snap[i] != want {
			t.Errorf("snap[%d] = %q, want %q", i, snap[i], want)
		}
	}
}

func TestLogRingBufferSubscriberReceivesLines(t *testing.T) {
	t.Parallel()
	b := newTestBuffer(t, DefaultLogBufferSize)

	snapshot, ch := b.SubscribeWithSnapshot()
	_ = snapshot

	_, _ = b.Write([]byte("alpha\n"))
	_, _ = b.Write([]byte("beta\n"))

	got := drainChannel(ch, 2, time.Second)
	if len(got) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(got))
	}
	if got[0] != "alpha" || got[1] != "beta" {
		t.Fatalf("expected [alpha beta], got %v", got)
	}
}

func TestLogRingBufferSlowConsumerDropAndMarker(t *testing.T) {
	t.Parallel()
	const smallSize = 4
	b := newTestBuffer(t, smallSize)

	_, ch := b.SubscribeWithSnapshot()

	for range smallSize {
		_, _ = b.Write([]byte("fill\n"))
	}
	// Drain the filled lines to clear the channel.
	for range smallSize {
		<-ch
	}

	// Now write more lines than channel capacity to trigger drops.
	for range smallSize + 2 {
		_, _ = b.Write([]byte("overflow\n"))
	}

	// The subscriber channel should eventually contain lines or a marker.
	// Read everything available with a timeout.
	var received []string
	timer := time.NewTimer(200 * time.Millisecond)
	defer timer.Stop()
	for {
		select {
		case line := <-ch:
			received = append(received, line)
		case <-timer.C:
			goto done
		}
	}
done:
	if len(received) == 0 {
		t.Fatal("expected at least one line or marker after overflow")
	}
	// Verify that a marker was sent (at least one line should contain "dropped").
	hasMarker := false
	for _, line := range received {
		if strings.Contains(line, "lines dropped") {
			hasMarker = true
			break
		}
	}
	if !hasMarker {
		t.Errorf("expected a dropped-lines marker among received lines, got: %v", received)
	}
}

func TestLogRingBufferSlowConsumerLineNotLost(t *testing.T) {
	t.Parallel()
	const smallSize = 3
	b := newTestBuffer(t, smallSize)

	_, ch := b.SubscribeWithSnapshot()

	// Pre-fill the subscriber channel to capacity.
	for range smallSize {
		_, _ = b.Write([]byte("padding\n"))
	}

	// Write the critical line — this is the bug fix scenario.
	// With the bug, this line would be silently discarded.
	_, _ = b.Write([]byte("critical-line\n"))

	// The critical line must be receivable from the channel.
	var found bool
	timer := time.NewTimer(500 * time.Millisecond)
	defer timer.Stop()
	for !found {
		select {
		case line := <-ch:
			if line == "critical-line" {
				found = true
			}
		case <-timer.C:
			t.Fatal("critical-line was never delivered — bug not fixed")
		}
	}
	if !found {
		t.Fatal("critical-line was never delivered")
	}
}

func TestLogRingBufferConcurrentWriters(t *testing.T) {
	t.Parallel()
	b := newTestBuffer(t, DefaultLogBufferSize)

	const writers = 10
	const linesPerWriter = 50

	var wg sync.WaitGroup
	wg.Add(writers)
	for range writers {
		go func() {
			defer wg.Done()
			for range linesPerWriter {
				_, _ = b.Write([]byte("msg\n"))
			}
		}()
	}
	wg.Wait()

	snap, _ := b.SubscribeWithSnapshot()
	if len(snap) != DefaultLogBufferSize {
		t.Errorf("expected %d lines in snapshot, got %d", DefaultLogBufferSize, len(snap))
	}
}

func TestLogRingBufferSubscribeAtomicity(t *testing.T) {
	t.Parallel()
	b := newTestBuffer(t, DefaultLogBufferSize)

	_, _ = b.Write([]byte("before\n"))

	snapshot, ch := b.SubscribeWithSnapshot()

	// Lines written after subscribe must appear on the channel, not in snapshot.
	_, _ = b.Write([]byte("after\n"))

	if len(snapshot) != 1 || snapshot[0] != "before" {
		t.Fatalf("snapshot should contain exactly [before], got %v", snapshot)
	}

	got := drainChannel(ch, 1, time.Second)
	if len(got) != 1 || got[0] != "after" {
		t.Fatalf("channel should deliver [after], got %v", got)
	}
}

func TestLogRingBufferUnsubscribeIdempotent(t *testing.T) {
	t.Parallel()
	b := newTestBuffer(t, DefaultLogBufferSize)

	_, ch := b.SubscribeWithSnapshot()

	// First unsubscribe — closes channel.
	b.Unsubscribe(ch)

	// Second unsubscribe — must be a no-op (no panic).
	b.Unsubscribe(ch)
}

func TestLogRingBufferEmptyBuffer(t *testing.T) {
	t.Parallel()
	b := newTestBuffer(t, DefaultLogBufferSize)

	snapshot, _ := b.SubscribeWithSnapshot()
	if len(snapshot) != 0 {
		t.Fatalf("expected empty snapshot, got %d lines", len(snapshot))
	}
}

func TestLogRingBufferSubscriberCount(t *testing.T) {
	t.Parallel()
	b := newTestBuffer(t, DefaultLogBufferSize)

	_, ch1 := b.SubscribeWithSnapshot()
	_, ch2 := b.SubscribeWithSnapshot()

	_, _ = b.Write([]byte("broadcast\n"))

	got1 := drainChannel(ch1, 1, time.Second)
	got2 := drainChannel(ch2, 1, time.Second)

	if len(got1) != 1 || got1[0] != "broadcast" {
		t.Errorf("subscriber 1: expected [broadcast], got %v", got1)
	}
	if len(got2) != 1 || got2[0] != "broadcast" {
		t.Errorf("subscriber 2: expected [broadcast], got %v", got2)
	}

	b.Unsubscribe(ch1)
	b.Unsubscribe(ch2)
}

// drainChannel reads up to n items from ch with a timeout.
func drainChannel(ch chan string, n int, timeout time.Duration) []string {
	var result []string
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for len(result) < n {
		select {
		case v := <-ch:
			result = append(result, v)
		case <-timer.C:
			return result
		}
	}
	return result
}
