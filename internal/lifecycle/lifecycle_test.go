// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package lifecycle

import (
	"sync"
	"testing"
)

func TestStatus_String(t *testing.T) {
	tests := []struct {
		want   string
		status Status
	}{
		{want: "none", status: StatusNone},
		{want: "pending", status: StatusPending},
		{want: "active", status: StatusActive},
		{want: "error", status: StatusError},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.status.String(); got != tt.want {
				t.Errorf("Status(%d).String() = %q, want %q", tt.status, got, tt.want)
			}
		})
	}
}

func TestStatus_String_Unknown(t *testing.T) {
	if got := Status(99).String(); got != "unknown" {
		t.Errorf("Status(99).String() = %q, want %q", got, "unknown")
	}
}

func TestNewStateTracker(t *testing.T) {
	st := NewStateTracker()
	if st == nil {
		t.Fatal("NewStateTracker() returned nil")
	}
	if st.states == nil {
		t.Fatal("StateTracker.states map is nil")
	}
}

func TestStateTracker_SetAndGet(t *testing.T) {
	st := NewStateTracker()

	// Initial state for unknown domain
	if got := st.Get("foo"); got != StatusNone {
		t.Errorf("Get for unknown domain = %v, want StatusNone", got)
	}

	// Set and get
	st.Set("foo", StatusActive)
	if got := st.Get("foo"); got != StatusActive {
		t.Errorf("Get after Set = %v, want StatusActive", got)
	}

	// Different domain unaffected
	if got := st.Get("bar"); got != StatusNone {
		t.Errorf("Get for different domain = %v, want StatusNone", got)
	}
}

func TestStateTracker_Overwrite(t *testing.T) {
	st := NewStateTracker()

	st.Set("foo", StatusPending)
	st.Set("foo", StatusActive)
	st.Set("foo", StatusError)

	if got := st.Get("foo"); got != StatusError {
		t.Errorf("Get after multiple Sets = %v, want StatusError", got)
	}
}

func TestStateTracker_Delete(t *testing.T) {
	st := NewStateTracker()

	st.Set("foo", StatusActive)
	st.Delete("foo")

	if got := st.Get("foo"); got != StatusNone {
		t.Errorf("Get after Delete = %v, want StatusNone", got)
	}

	// Double delete should not panic
	st.Delete("foo")
}

func TestStateTracker_Delete_NonExistent(_ *testing.T) {
	st := NewStateTracker()
	// Deleting a key that was never set should not panic
	st.Delete("nonexistent")
}

func TestStateTracker_ConcurrentAccess(_ *testing.T) {
	st := NewStateTracker()
	var wg sync.WaitGroup

	// Concurrent writers
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			domain := "domain-" + string(rune('A'+n)) //nolint:gosec // n is always 0-9
			st.Set(domain, StatusActive)
		}(i)
	}

	// Concurrent readers
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			domain := "domain-" + string(rune('A'+n)) //nolint:gosec // n is always 0-9
			_ = st.Get(domain)
		}(i)
	}

	// Concurrent deleters
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			domain := "domain-" + string(rune('A'+n)) //nolint:gosec // n is always 0-9
			st.Delete(domain)
		}(i)
	}

	wg.Wait()
}

func TestStateTracker_MultipleDomains(t *testing.T) {
	st := NewStateTracker()

	domains := map[string]Status{
		"one":   StatusPending,
		"two":   StatusActive,
		"three": StatusError,
		"four":  StatusNone,
	}

	for d, s := range domains {
		st.Set(d, s)
	}

	for d, want := range domains {
		got := st.Get(d)
		if got != want {
			t.Errorf("domain %q: got %v, want %v", d, got, want)
		}
	}
}

func TestStateTracker_DeletePreventsMemoryLeak(t *testing.T) {
	st := NewStateTracker()

	// Simulate many transient domains being created and deleted
	for i := 0; i < 1000; i++ {
		domain := "leaky-" + string(rune('0'+i%10))
		st.Set(domain, StatusActive)
		st.Delete(domain)
	}

	// After deletes, map should be empty
	if len(st.states) != 0 {
		t.Errorf("expected empty map after all deletes, got %d entries", len(st.states))
	}
}
