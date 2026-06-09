// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package sshtunnel

// Config holds SSH tunnel configuration for connecting to a remote Docker
// daemon via ssh:// URIs.
type Config struct {
	Host                  string
	PrivateKeyFile        string
	PrivateKeyPassphrase  string
	KnownHostsFile        string
	AgentSocket           string
	InsecureSkipHostCheck bool
}
