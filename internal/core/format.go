// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package core

import (
	"fmt"
	"math"
	"strings"
	"time"
)

const (
	hoursPerDay    = 24
	minutesPerHour = 60
)

// FormatDuration renders d as a compact human-readable string
// (e.g. "1d 2h 3m") used for proxy uptime display across the dashboard
// and REST API.
func FormatDuration(d time.Duration) string {
	if d == 0 {
		return ""
	}
	days := int(d.Hours() / hoursPerDay)
	hours := int(math.Mod(d.Hours(), hoursPerDay))
	minutes := int(math.Mod(d.Minutes(), minutesPerHour))

	var parts []string
	if days > 0 {
		parts = append(parts, fmt.Sprintf("%dd", days))
	}
	if hours > 0 {
		parts = append(parts, fmt.Sprintf("%dh", hours))
	}
	if minutes > 0 || len(parts) == 0 {
		parts = append(parts, fmt.Sprintf("%dm", minutes))
	}
	return strings.Join(parts, " ")
}
