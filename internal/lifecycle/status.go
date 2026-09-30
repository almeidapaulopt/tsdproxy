// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package lifecycle

// Status represents the current state of a provider operation.
type Status int

const (
	StatusNone Status = iota
	StatusPending
	StatusActive
	StatusError
)

func (s Status) String() string {
	switch s {
	case StatusNone:
		return "none"
	case StatusPending:
		return "pending"
	case StatusActive:
		return "active"
	case StatusError:
		return "error"
	default:
		return "unknown"
	}
}
