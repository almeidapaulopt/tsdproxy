// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package incus

import (
	"errors"
)

var (
	// ErrInstanceNotEnabled is returned when AddTarget or ReResolve is called
	// for an instance without user.tsdproxy.enable=true.
	ErrInstanceNotEnabled = errors.New("instance not enabled for tsdproxy")

	// ErrInstanceNotRunning is returned when a proxy config is requested for
	// an instance that is not in the Running state.
	ErrInstanceNotRunning = errors.New("instance not running")

	// ErrNoAddressFound is returned when the instance state has no
	// global-scope network address and no targetHostname fallback is set.
	ErrNoAddressFound = errors.New("no global address found in instance state")

	// ErrSocketAndURL is returned when the provider configuration sets both
	// socket and url, which are mutually exclusive connection modes.
	ErrSocketAndURL = errors.New("socket and url are mutually exclusive")

	// ErrTLSClientMaterial is returned when only one of tlsClientCertFile /
	// tlsClientKeyFile is configured. Both are required for client certs.
	ErrTLSClientMaterial = errors.New("tlsClientCertFile and tlsClientKeyFile must be set together")
)
