// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package docker

import (
	"time"
)

const (
	// Constants to be used in container labels
	LabelPrefix    = "tsdproxy."
	LabelIsEnabled = LabelEnable + "=true"

	// Container config labels.
	LabelEnable             = LabelPrefix + "enable"
	LabelName               = LabelPrefix + "name"
	LabelContainerAccessLog = LabelPrefix + "containeraccesslog"
	LabelProxyProvider      = LabelPrefix + "proxyprovider"
	LabelPort               = LabelPrefix + "port."
	LabelLoadBalance        = LabelPrefix + "loadbalance"
	// Tailscale
	LabelEphemeral    = LabelPrefix + "ephemeral"
	LabelRunWebClient = LabelPrefix + "runwebclient"
	LabelTsnetVerbose = LabelPrefix + "tsnet_verbose"
	LabelAuthKey      = LabelPrefix + "authkey"
	LabelAuthKeyFile  = LabelPrefix + "authkeyfile"
	LabelAutoDetect   = LabelPrefix + "autodetect"
	LabelTags         = LabelPrefix + "tags"
	// Identity / auth header injection (default: enabled)
	LabelIdentityHeaders     = LabelPrefix + "identity_headers"
	LabelAutoRestart         = LabelPrefix + "auto_restart"
	LabelHealthCheckEnabled  = LabelPrefix + "health_check_enabled"
	LabelHealthCheckInterval = LabelPrefix + "health_check_interval"
	LabelHealthCheckFailures = LabelPrefix + "health_check_failures"
	LabelHealthCheckCooldown = LabelPrefix + "health_check_cooldown"
	LabelRateLimitEnabled    = LabelPrefix + "ratelimit.enabled"
	LabelRateLimitRPS        = LabelPrefix + "ratelimit.rps"
	LabelRateLimitBurst      = LabelPrefix + "ratelimit.burst"
	// Legacy
	LabelContainerPort = LabelPrefix + "container_port"
	LabelScheme        = LabelPrefix + "scheme"
	LabelTLSValidate   = LabelPrefix + "tlsvalidate"
	// Legacy Tailscale
	LabelFunnel = LabelPrefix + "funnel"
	// Dashboard config labels
	LabelDashboardPrefix   = LabelPrefix + "dash."
	LabelDashboardVisible  = LabelDashboardPrefix + "visible"
	LabelDashboardLabel    = LabelDashboardPrefix + "label"
	LabelDashboardIcon     = LabelDashboardPrefix + "icon"
	LabelDashboardCategory = LabelDashboardPrefix + "category"

	// Custom domain / DNS / TLS labels
	LabelDomain      = LabelPrefix + "domain"
	LabelDNSProvider = LabelPrefix + "dnsprovider"
	LabelTLSProvider = LabelPrefix + "tlsprovider"

	// docker only defaults
	DefaultTargetScheme = "http"

	// auto detect
	dialTimeout     = 2 * time.Second
	autoDetectTries = 5
	autoDetectSleep = 5 * time.Second

	// health check label bounds
	healthCheckMaxIntervalSeconds = 86400
	healthCheckMaxFailures        = 100
	healthCheckMaxCooldownSeconds = 86400

	// Port options
	PortOptionNoTLSValidate   = "no_tlsvalidate"
	PortOptionTailscaleFunnel = "tailscale_funnel"
	PortOptionNoAutoDetect    = "no_autodetect"
	PortOptionLoadBalance     = "loadbalance"
)
