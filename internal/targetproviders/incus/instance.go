// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package incus

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"regexp"
	"sort"
	"strings"

	incusapi "github.com/lxc/incus/v7/shared/api"
	"github.com/rs/zerolog"

	"github.com/almeidapaulopt/tsdproxy/internal/model"
	"github.com/almeidapaulopt/tsdproxy/internal/targetproviders/settings"
	"github.com/almeidapaulopt/tsdproxy/web"
)

var rfc1123Hostname = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?$`)

type (
	// instance stores the data from an Incus instance (container or VM) used
	// to build the per-proxy configuration.
	instance struct {
		log                      zerolog.Logger
		config                   map[string]string
		assets                   *web.Assets
		image                    string
		name                     string
		instanceType             string
		targetProviderName       string
		defaultTargetHostname    string
		ips                      []netip.Addr
		healthCheckInterval      int
		rateLimitBurst           int
		providerHealthCooldown   int
		providerRateLimitRPS     int
		providerRateLimitBurst   int
		providerHealthInterval   int
		healthCheckFailures      int
		healthCheckCooldown      int
		rateLimitRPS             int
		providerHealthFailures   int
		providerAutoRestart      bool
		providerHealthEnabled    bool
		providerRateLimitEnabled bool
		proxyAccessLogDefault    bool
		allowInstanceFunnel      bool
		allowTLSValidateDisable  bool
		healthCheckEnabled       bool
		rateLimitEnabled         bool
		autoRestart              bool
	}

	// InstanceOption configures an instance during construction.
	InstanceOption func(*instance)
)

// newInstance returns a new instance from the Incus API objects.
func newInstance(logger zerolog.Logger, inst *incusapi.Instance, state *incusapi.InstanceState, opts ...InstanceOption) *instance {
	newlog := logger.With().Str("instance", inst.Name).Logger()
	newlog.Trace().Msg("New Instance")
	defer newlog.Trace().Msg("End New Instance")

	i := &instance{
		log:          newlog,
		config:       inst.Config,
		ips:          make([]netip.Addr, 0),
		image:        inst.Config[imageDescriptionKey],
		name:         inst.Name,
		instanceType: inst.Type,
	}

	for _, opt := range opts {
		opt(i)
	}

	i.autoRestart = settings.Bool(i.config, ConfigAutoRestart, i.providerAutoRestart)
	i.healthCheckEnabled = settings.Bool(i.config, ConfigHealthCheckEnabled, i.providerHealthEnabled)
	i.healthCheckInterval = settings.Int(i.log, i.config, ConfigHealthCheckInterval,
		i.providerHealthInterval, model.HealthCheckMinIntervalSeconds, model.HealthCheckMaxIntervalSeconds)
	i.healthCheckFailures = settings.Int(i.log, i.config, ConfigHealthCheckFailures,
		i.providerHealthFailures, model.HealthCheckMinFailures, model.HealthCheckMaxFailures)
	i.healthCheckCooldown = settings.Int(i.log, i.config, ConfigHealthCheckCooldown,
		i.providerHealthCooldown, model.HealthCheckMinCooldownSeconds, model.HealthCheckMaxCooldownSeconds)

	i.rateLimitEnabled = settings.Bool(i.config, ConfigRateLimitEnabled, i.providerRateLimitEnabled)
	i.rateLimitRPS = settings.Int(i.log, i.config, ConfigRateLimitRPS, i.providerRateLimitRPS, model.RateLimitMinRPS, model.RateLimitMaxRPS)
	i.rateLimitBurst = settings.Int(i.log, i.config, ConfigRateLimitBurst, i.providerRateLimitBurst, model.RateLimitMinBurst, model.RateLimitMaxBurst)

	i.setInstanceNetwork(state)

	return i
}

// setInstanceNetwork collects global-scope addresses from the instance
// state. IPv4 addresses are preferred over IPv6, the primary interface
// (eth0) over the rest, and interfaces are visited in name order for
// deterministic results.
func (i *instance) setInstanceNetwork(state *incusapi.InstanceState) {
	i.log.Trace().Msg("start setInstanceNetwork")
	defer i.log.Trace().Msg("end setInstanceNetwork")

	if state == nil {
		return
	}

	names := make([]string, 0, len(state.Network))
	for name := range state.Network {
		if name == loopbackInterface {
			continue
		}
		names = append(names, name)
	}

	sort.Slice(names, func(a, b int) bool {
		if names[a] == primaryInterface {
			return names[b] != primaryInterface
		}
		if names[b] == primaryInterface {
			return false
		}
		return names[a] < names[b]
	})

	var ipv4, ipv6 []netip.Addr
	for _, name := range names {
		for _, address := range state.Network[name].Addresses {
			if address.Scope != addressScopeGlobal {
				continue
			}

			addr, err := netip.ParseAddr(address.Address)
			if err != nil {
				i.log.Debug().Str("interface", name).Str("address", address.Address).Msg("invalid address in instance state")
				continue
			}

			if addr.Is4() || addr.Is4In6() {
				ipv4 = append(ipv4, addr.Unmap())
			} else {
				ipv6 = append(ipv6, addr)
			}
		}
	}

	i.ips = append(ipv4, ipv6...)
}

// newProxyConfig method returns a new model.Config for the instance.
func (i *instance) newProxyConfig(ctx context.Context) (*model.Config, error) {
	i.log.Trace().Msg("New ProxyConfig")
	defer i.log.Trace().Msg("End New ProxyConfig")

	hostname, err := i.getProxyHostname()
	if err != nil {
		return nil, fmt.Errorf("error parsing Hostname: %w", err)
	}

	tailscale, err := i.getTailscaleConfig()
	if err != nil {
		return nil, err
	}

	pcfg, err := model.NewConfig()
	if err != nil {
		return nil, err
	}

	pcfg.TargetID = i.name
	pcfg.TargetImage = i.image
	pcfg.Hostname = hostname
	pcfg.TargetProvider = i.targetProviderName
	pcfg.Tailscale = *tailscale
	pcfg.ProxyProvider = settings.String(i.config, ConfigProxyProvider, model.DefaultProxyProvider)
	pcfg.Domain = settings.String(i.config, ConfigDomain, "")
	pcfg.DNSProvider = settings.String(i.config, ConfigDNSProvider, "")
	pcfg.TLSProvider = settings.String(i.config, ConfigTLSProvider, "")
	pcfg.ProxyAccessLog = settings.Bool(i.config, ConfigContainerAccessLog, i.proxyAccessLogDefault)
	pcfg.IdentityHeaders = settings.Bool(i.config, ConfigIdentityHeaders, model.DefaultIdentityHeaders)
	pcfg.AutoRestart = i.autoRestart
	pcfg.HealthCheckEnabled = i.healthCheckEnabled
	pcfg.HealthCheckInterval = i.healthCheckInterval
	pcfg.HealthCheckFailures = i.healthCheckFailures
	pcfg.HealthCheckCooldown = i.healthCheckCooldown
	pcfg.RateLimitEnabled = i.rateLimitEnabled
	pcfg.RateLimitRPS = i.rateLimitRPS
	pcfg.RateLimitBurst = i.rateLimitBurst
	pcfg.Dashboard.Visible = settings.Bool(i.config, ConfigDashboardVisible, model.DefaultDashboardVisible)
	pcfg.Dashboard.Label = settings.String(i.config, ConfigDashboardLabel, pcfg.Hostname)

	pcfg.Dashboard.Category = settings.String(i.config, ConfigDashboardCategory, "")
	pcfg.Dashboard.Icon = settings.String(i.config, ConfigDashboardIcon, "")
	if pcfg.Dashboard.Icon == "" {
		pcfg.Dashboard.Icon = i.assets.GuessIcon(i.image)
	}

	pcfg.Ports = i.getPorts(ctx)

	return pcfg, nil
}

// getPorts returns the port configuration from user.tsdproxy.port.* keys.
// The value format matches the Docker provider:
// "<proxy port>/<proxy protocol>:<target port>/<target protocol>[,option]".
func (i *instance) getPorts(ctx context.Context) model.PortConfigList {
	return model.Ports(ctx, i.log, i.config, ConfigPort, i.portOptionGates(), i.generateTargetFromFirstTarget)
}

// portOptionGates returns the provider's port option policy from the
// operator's settings. Incus has no autodetection, so no_autodetect is not
// supported.
func (i *instance) portOptionGates() model.PortOptionGates {
	return model.PortOptionGates{
		AllowTLSValidateDisable: i.allowTLSValidateDisable,
		AllowFunnel:             i.allowInstanceFunnel,
		FunnelSettingName:       "allowInstanceFunnel",
		SupportNoAutoDetect:     false,
	}
}

// generateTargetFromFirstTarget resolves the port's target URL against the
// instance network. Multiple targets are not supported in this TargetProvider.
func (i *instance) generateTargetFromFirstTarget(ctx context.Context, port model.PortConfig) (model.PortConfig, error) {
	i.log.Trace().Msg("generateTargetFromFirstTarget")
	defer i.log.Trace().Msg("End generateTargetFromFirstTarget")

	select {
	case <-ctx.Done():
		return port, ctx.Err()
	default:
	}

	p := port.GetFirstTarget()
	if p == nil {
		return port, fmt.Errorf("no target URL for port %s", port.String())
	}

	targetURL, err := i.getTargetURL(p)
	if err != nil {
		return port, err
	}
	i.log.Debug().Str("port", port.String()).Str("target", targetURL.String()).Msg("target URL")

	port.ReplaceTarget(p, targetURL)

	return port, nil
}

// getTailscaleConfig method returns the tailscale configuration.
func (i *instance) getTailscaleConfig() (*model.Tailscale, error) {
	i.log.Trace().Msg("getTailscaleConfig")
	defer i.log.Trace().Msg("End getTailscaleConfig")

	authKey := settings.String(i.config, ConfigAuthKey, "")

	authKeySecret, err := settings.AuthKeyFromFile(i.config, ConfigAuthKeyFile, authKey)
	if err != nil {
		return nil, fmt.Errorf("error setting auth key from file : %w", err)
	}

	tags := settings.String(i.config, ConfigTags, "")

	return &model.Tailscale{
		Ephemeral:    settings.Bool(i.config, ConfigEphemeral, model.DefaultTailscaleEphemeral),
		RunWebClient: settings.Bool(i.config, ConfigRunWebClient, model.DefaultTailscaleRunWebClient),
		Verbose:      settings.Bool(i.config, ConfigTsnetVerbose, model.DefaultTailscaleVerbose),
		AuthKey:      authKeySecret,
		Tags:         tags,
	}, nil
}

// getTargetURL returns the target URL for the instance: the configured
// targetHostname (provider-level fallback) or the first instance address,
// with the port's target port.
func (i *instance) getTargetURL(iPort *url.URL) (*url.URL, error) {
	i.log.Trace().Msg("getTargetURL")
	defer i.log.Trace().Msg("End getTargetURL")

	port := iPort.Port()
	if port == "" {
		return nil, fmt.Errorf("%w: %s", ErrNoAddressFound, i.name)
	}

	host := i.defaultTargetHostname
	if host == "" {
		if len(i.ips) == 0 {
			return nil, fmt.Errorf("%w: %s", ErrNoAddressFound, i.name)
		}
		host = i.ips[0].String()
	}

	return url.Parse(iPort.Scheme + "://" + net.JoinHostPort(host, port))
}

// getProxyHostname method returns the proxy hostname from the instance
// config key, falling back to the instance name.
func (i *instance) getProxyHostname() (string, error) {
	i.log.Trace().Msg("getProxyHostname")
	defer i.log.Trace().Msg("End getProxyHostname")

	if customName, ok := i.config[ConfigName]; ok {
		if !rfc1123Hostname.MatchString(customName) {
			return "", fmt.Errorf("invalid hostname %q: must match RFC 1123 (alphanumeric, hyphens, 1-63 chars)", customName)
		}
		return strings.ToLower(customName), nil
	}

	return strings.ToLower(i.name), nil
}

func withTargetProviderName(name string) InstanceOption {
	return func(i *instance) {
		i.targetProviderName = name
	}
}

func withDefaultTargetHostname(hostname string) InstanceOption {
	return func(i *instance) {
		i.defaultTargetHostname = hostname
	}
}

func withProviderAutoRestart(autoRestart bool) InstanceOption {
	return func(i *instance) {
		i.providerAutoRestart = autoRestart
	}
}

func withProviderHealthCheck(enabled bool, interval, failures, cooldown int) InstanceOption {
	return func(i *instance) {
		i.providerHealthEnabled = enabled
		i.providerHealthInterval = interval
		i.providerHealthFailures = failures
		i.providerHealthCooldown = cooldown
	}
}

func withProviderRateLimit(enabled bool, rps, burst int) InstanceOption {
	return func(i *instance) {
		i.providerRateLimitEnabled = enabled
		i.providerRateLimitRPS = rps
		i.providerRateLimitBurst = burst
	}
}

func withProxyAccessLogDefault(defaultVal bool) InstanceOption {
	return func(i *instance) {
		i.proxyAccessLogDefault = defaultVal
	}
}

func withAssets(assets *web.Assets) InstanceOption {
	return func(i *instance) {
		i.assets = assets
	}
}

func withAllowInstanceFunnel(allowed bool) InstanceOption {
	return func(i *instance) {
		i.allowInstanceFunnel = allowed
	}
}

func withAllowTLSValidateDisable(allowed bool) InstanceOption {
	return func(i *instance) {
		i.allowTLSValidateDisable = allowed
	}
}
