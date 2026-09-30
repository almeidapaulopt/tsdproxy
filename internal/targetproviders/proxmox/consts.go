// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package proxmox

import (
	"time"
)

const (
	// ConfigPrefix is the prefix of all tsdproxy settings extracted from the
	// guest notes/description YAML block. Keys below mirror the Docker label
	// names and the Incus user.tsdproxy.* keys after stripping their prefix.
	ConfigPrefix = "tsdproxy."

	// ConfigIsEnabled marks a guest as enabled for proxying.
	ConfigIsEnabled = ConfigPrefix + "enable"

	// Guest settings (equivalent to the Docker provider labels).
	ConfigName               = ConfigPrefix + "name"
	ConfigContainerAccessLog = ConfigPrefix + "containeraccesslog"
	ConfigProxyProvider      = ConfigPrefix + "proxyprovider"
	ConfigPort               = ConfigPrefix + "port."
	// Tailscale
	ConfigEphemeral    = ConfigPrefix + "ephemeral"
	ConfigRunWebClient = ConfigPrefix + "runwebclient"
	ConfigTsnetVerbose = ConfigPrefix + "tsnet_verbose"
	ConfigAuthKey      = ConfigPrefix + "authkey"
	ConfigAuthKeyFile  = ConfigPrefix + "authkeyfile"
	ConfigTags         = ConfigPrefix + "tags"
	// Identity / auth header injection (default: enabled)
	ConfigIdentityHeaders     = ConfigPrefix + "identity_headers"
	ConfigAutoRestart         = ConfigPrefix + "auto_restart"
	ConfigHealthCheckEnabled  = ConfigPrefix + "health_check_enabled"
	ConfigHealthCheckInterval = ConfigPrefix + "health_check_interval"
	ConfigHealthCheckFailures = ConfigPrefix + "health_check_failures"
	ConfigHealthCheckCooldown = ConfigPrefix + "health_check_cooldown"
	ConfigRateLimitEnabled    = ConfigPrefix + "ratelimit.enabled"
	ConfigRateLimitRPS        = ConfigPrefix + "ratelimit.rps"
	ConfigRateLimitBurst      = ConfigPrefix + "ratelimit.burst"
	// Dashboard config keys
	ConfigDashboardPrefix   = ConfigPrefix + "dash."
	ConfigDashboardVisible  = ConfigDashboardPrefix + "visible"
	ConfigDashboardLabel    = ConfigDashboardPrefix + "label"
	ConfigDashboardIcon     = ConfigDashboardPrefix + "icon"
	ConfigDashboardCategory = ConfigDashboardPrefix + "category"
	// Custom domain / DNS / TLS keys
	ConfigDomain      = ConfigPrefix + "domain"
	ConfigDNSProvider = ConfigPrefix + "dnsprovider"
	ConfigTLSProvider = ConfigPrefix + "tlsprovider"

	// notesRootKey is the YAML key holding the tsdproxy settings block.
	notesRootKey = "tsdproxy"

	// Guest types returned by the cluster resources endpoint.
	guestTypeQemu = "qemu"
	guestTypeLXC  = "lxc"

	// Guest statuses returned by the cluster resources endpoint.
	statusRunning = "running"
	statusStopped = "stopped"

	// guestRequestTimeout bounds each API call made while building a proxy
	// config.
	guestRequestTimeout = 30 * time.Second

	// primaryInterface is preferred when collecting guest addresses.
	primaryInterface = "eth0"

	// loopbackInterface is skipped when collecting guest addresses.
	loopbackInterface = "lo"

	// redirectSeparator marks redirect port labels ("<proxy>-><url>"),
	// mirroring the model package grammar.
	redirectSeparator = "->"

	// placeholderHost is the host long port labels use to carry the target
	// scheme and port until a provider resolves the real address.
	placeholderHost = "0.0.0.0"
)
