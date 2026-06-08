// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package core

import (
	"testing"
	"time"
)

func TestFormatDuration_Zero(t *testing.T) {
	t.Parallel()

	if got := FormatDuration(0); got != "" {
		t.Errorf("FormatDuration(0) = %q, want %q", got, "")
	}
}

func TestFormatDuration_MinutesOnly(t *testing.T) {
	t.Parallel()

	want := "5m"
	if got := FormatDuration(5 * time.Minute); got != want {
		t.Errorf("FormatDuration(5m) = %q, want %q", got, want)
	}
}

func TestFormatDuration_HoursAndMinutes(t *testing.T) {
	t.Parallel()

	want := "2h 30m"
	if got := FormatDuration(2*time.Hour + 30*time.Minute); got != want {
		t.Errorf("FormatDuration(2h30m) = %q, want %q", got, want)
	}
}

func TestFormatDuration_DaysHoursMinutes(t *testing.T) {
	t.Parallel()

	want := "1d 2h 3m"
	if got := FormatDuration(24*time.Hour + 2*time.Hour + 3*time.Minute); got != want {
		t.Errorf("FormatDuration(1d2h3m) = %q, want %q", got, want)
	}
}

func TestFormatDuration_DaysOnly(t *testing.T) {
	t.Parallel()

	want := "2d"
	if got := FormatDuration(48 * time.Hour); got != want {
		t.Errorf("FormatDuration(48h) = %q, want %q", got, want)
	}
}

func TestFormatDuration_DaysAndHours(t *testing.T) {
	t.Parallel()

	want := "1d 2h"
	if got := FormatDuration(26 * time.Hour); got != want {
		t.Errorf("FormatDuration(26h) = %q, want %q", got, want)
	}
}

func TestFormatDuration_LessThanMinute(t *testing.T) {
	t.Parallel()

	want := "0m"
	if got := FormatDuration(30 * time.Second); got != want {
		t.Errorf("FormatDuration(30s) = %q, want %q", got, want)
	}
}
