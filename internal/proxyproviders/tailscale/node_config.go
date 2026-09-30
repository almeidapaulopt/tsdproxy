// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package tailscale

// Mode constants for NodeConfig.Mode.
const (
	ModePerProxy = ""
	ModeShared   = "shared"
	ModeServices = "services"
)

// NodeConfig describes the desired Tailscale node configuration, independent
// of how traffic is exposed (per-proxy listeners, SNI routing, VIP Services).
type NodeConfig struct {
	Hostname      string
	DataDir       string
	ControlURL    string
	Tags          string
	Mode          string
	AdvertiseTags []string
	Ephemeral     bool
	RunWebClient  bool
	Verbose       bool
}
