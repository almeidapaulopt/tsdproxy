// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package proxmox

import (
	"errors"
)

var (
	// ErrGuestNotEnabled is returned when AddTarget or ReResolve is called
	// for a guest whose notes do not set tsdproxy.enable=true.
	ErrGuestNotEnabled = errors.New("guest not enabled for tsdproxy")

	// ErrGuestNotRunning is returned when a proxy config is requested for a
	// guest that is not running.
	ErrGuestNotRunning = errors.New("guest not running")

	// ErrGuestNotFound is returned when a guest ID does not exist on the
	// cluster (never seen by the poll loop and not returned by the
	// resources endpoint).
	ErrGuestNotFound = errors.New("guest not found")

	// ErrNoAddressFound is returned when the guest has no usable network
	// address and no targetHostname fallback is set.
	ErrNoAddressFound = errors.New("no usable address found for guest")

	// ErrInvalidAPIToken is returned when the configured API token does not
	// have the required "<tokenid>=<secret>" format.
	ErrInvalidAPIToken = errors.New("apiToken must be in the format <user>@<realm>!<tokenid>=<secret>")

	// ErrInvalidGuestID is returned when a guest ID is not in the
	// "<type>/<vmid>" form produced by the cluster resources endpoint.
	ErrInvalidGuestID = errors.New("invalid guest id, want <type>/<vmid>")
)
