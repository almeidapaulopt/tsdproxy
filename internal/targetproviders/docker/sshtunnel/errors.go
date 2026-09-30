// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package sshtunnel

import "errors"

var (
	ErrTunnelClosed  = errors.New("ssh tunnel closed")
	ErrNotSSHHost    = errors.New("host is not an ssh:// URI")
	ErrNoAuthMethods = errors.New("no SSH auth methods configured")
)
