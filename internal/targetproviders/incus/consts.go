// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package incus

import (
	"time"
)

const (
	// ConfigPrefix is the prefix for all tsdproxy instance configuration keys.
	// Incus reserves the user.* key namespace for free-form configuration,
	// making user.tsdproxy.* the equivalent of Docker's tsdproxy.* labels.
	ConfigPrefix = "user.tsdproxy."

	// ConfigIsEnabled marks an instance as enabled for proxying.
	ConfigIsEnabled = ConfigPrefix + "enable"

	// Instance config keys (equivalent to the Docker provider labels).
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

	// imageDescriptionKey is the standard instance config key holding the
	// description of the image the instance was launched from. Used for icon
	// guessing only.
	imageDescriptionKey = "image.description"

	// stateWaitTries and stateWaitSleep bound how long buildProxyConfig polls
	// the instance state waiting for a global network address. DHCP and
	// cloud-init may not have finished when the instance-started event fires.
	stateWaitTries = 5
	stateWaitSleep = 2 * time.Second

	// addressScopeGlobal filters state addresses to globally routable ones.
	addressScopeGlobal = "global"

	// listenerChannelSize is the buffer size of the event channel consumed
	// by the WatchEvents loop.
	listenerChannelSize = 64

	// primaryInterface is preferred when collecting instance addresses.
	primaryInterface = "eth0"

	// loopbackInterface is skipped when collecting instance addresses.
	loopbackInterface = "lo"

	// health check key bounds (mirror the Docker provider).
	healthCheckMaxIntervalSeconds = 86400
	healthCheckMaxFailures        = 100
	healthCheckMaxCooldownSeconds = 86400

	// Port options (same syntax as the Docker provider).
	PortOptionNoTLSValidate   = "no_tlsvalidate"
	PortOptionTailscaleFunnel = "tailscale_funnel"
)
