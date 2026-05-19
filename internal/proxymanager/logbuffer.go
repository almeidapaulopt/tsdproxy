// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package proxymanager

import (
	"fmt"
	"strings"
	"sync"

	"github.com/rs/zerolog"
)

// DefaultLogBufferSize is the default number of log lines retained per proxy.
const DefaultLogBufferSize = 100

type LogRingBuffer struct {
	log         zerolog.Logger
	subscribers map[chan string]*subscriberState
	lines       []string
	pos         int
	size        int
	mu          sync.Mutex
	full        bool
}

type subscriberState struct {
	pendingDropped int
}

func NewLogRingBuffer(log zerolog.Logger, size int) *LogRingBuffer {
	return &LogRingBuffer{
		lines:       make([]string, size),
		subscribers: make(map[chan string]*subscriberState),
		log:         log.With().Str("component", "logbuffer").Logger(),
		size:        size,
	}
}

// Write appends one log line to the ring buffer and fans it out to all
// subscribers. Each call must contain exactly one logical line (the
// trailing newline is stripped). Callers must not pass multi-line input.
func (b *LogRingBuffer) Write(p []byte) (int, error) {
	line := strings.TrimRight(string(p), "\n")
	b.mu.Lock()
	b.appendLocked(line)
	b.fanOutLocked(line)
	b.mu.Unlock()
	return len(p), nil
}

// appendLocked stores a line into the ring buffer. Caller must hold b.mu.
func (b *LogRingBuffer) appendLocked(line string) {
	b.lines[b.pos] = line
	b.pos = (b.pos + 1) % b.size
	if b.pos == 0 {
		b.full = true
	}
}

// fanOutLocked sends the line to all subscribers. Caller must hold b.mu.
func (b *LogRingBuffer) fanOutLocked(line string) {
	for ch, st := range b.subscribers {
		if st.pendingDropped > 0 {
			d := st.pendingDropped
			if d > b.size {
				d = b.size
			}
			marker := fmt.Sprintf("--- %d lines dropped (slow consumer) ---", d)
			st.pendingDropped = 0
			select {
			case ch <- marker:
			default:
				st.pendingDropped = d
			}
		}

		select {
		case ch <- line:
		default:
			dropped := 1
		drain:
			for {
				select {
				case <-ch:
					dropped++
				default:
					break drain
				}
			}
			// Re-attempt to send the current line after draining.
			select {
			case ch <- line:
			default:
				dropped++
			}
			b.log.Debug().Int("dropped", dropped).Msg("slow consumer: lines dropped")
			st.pendingDropped += dropped
			if st.pendingDropped > b.size*2 {
				st.pendingDropped = b.size
			}
		}
	}
}

func (b *LogRingBuffer) snapshotLocked() []string {
	if !b.full {
		result := make([]string, 0, b.pos)
		result = append(result, b.lines[:b.pos]...)
		return result
	}

	result := make([]string, 0, b.size)
	result = append(result, b.lines[b.pos:]...)
	result = append(result, b.lines[:b.pos]...)
	return result
}

// SubscribeWithSnapshot atomically captures existing lines and subscribes
// to future ones, eliminating the race between snapshot and subscribe.
func (b *LogRingBuffer) SubscribeWithSnapshot() (snapshot []string, ch chan string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	ch = make(chan string, b.size)
	b.subscribers[ch] = &subscriberState{}

	return b.snapshotLocked(), ch
}

// Unsubscribe removes the channel from subscribers and closes it.
// Safe to call multiple times for the same channel (no-op on subsequent calls).
func (b *LogRingBuffer) Unsubscribe(ch chan string) {
	b.mu.Lock()
	if _, ok := b.subscribers[ch]; ok {
		delete(b.subscribers, ch)
		close(ch)
	}
	b.mu.Unlock()
}

// Close removes and closes all subscriber channels. After this call, any
// goroutine blocked on receive from a subscriber channel will unblock with
// the zero value / ok=false.
func (b *LogRingBuffer) Close() {
	b.mu.Lock()
	for ch := range b.subscribers {
		close(ch)
		delete(b.subscribers, ch)
	}
	b.mu.Unlock()
}
