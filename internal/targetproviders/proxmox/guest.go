// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package proxmox

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/rs/zerolog"

	"github.com/almeidapaulopt/tsdproxy/internal/model"
	"github.com/almeidapaulopt/tsdproxy/internal/targetproviders/settings"
	"github.com/almeidapaulopt/tsdproxy/web"
)

var rfc1123Hostname = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?$`)

type (
	// guest stores the data from a Proxmox guest (QEMU VM or LXC container)
	// used to build the per-proxy configuration.
	guest struct {
		log                      zerolog.Logger
		settings                 map[string]string
		listPorts                map[string]notesPort
		assets                   *web.Assets
		tags                     string
		name                     string
		guestType                string
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
		allowGuestFunnel         bool
		allowTLSValidateDisable  bool
		healthCheckEnabled       bool
		rateLimitEnabled         bool
		autoRestart              bool
	}

	// GuestOption configures a guest during construction.
	GuestOption func(*guest)
)

// newGuest returns a new guest from the resource, config, parsed notes and
// interfaces fetched from the Proxmox API.
func newGuest(logger zerolog.Logger, resource GuestResource, cfg *GuestConfig, notesCfg *notesConfig, interfaces []GuestInterface, opts ...GuestOption) *guest {
	newlog := logger.With().Str("guest", resource.Name).Logger()
	newlog.Trace().Msg("New Guest")
	defer newlog.Trace().Msg("End New Guest")

	g := &guest{
		log:       newlog,
		settings:  notesCfg.Settings,
		listPorts: notesCfg.Ports,
		tags:      cfg.Tags,
		name:      resource.Name,
		guestType: resource.Type,
	}

	for _, opt := range opts {
		opt(g)
	}

	g.autoRestart = settings.Bool(g.settings, ConfigAutoRestart, g.providerAutoRestart)
	g.healthCheckEnabled = settings.Bool(g.settings, ConfigHealthCheckEnabled, g.providerHealthEnabled)
	g.healthCheckInterval = settings.Int(g.log, g.settings, ConfigHealthCheckInterval,
		g.providerHealthInterval, model.HealthCheckMinIntervalSeconds, model.HealthCheckMaxIntervalSeconds)
	g.healthCheckFailures = settings.Int(g.log, g.settings, ConfigHealthCheckFailures,
		g.providerHealthFailures, model.HealthCheckMinFailures, model.HealthCheckMaxFailures)
	g.healthCheckCooldown = settings.Int(g.log, g.settings, ConfigHealthCheckCooldown,
		g.providerHealthCooldown, model.HealthCheckMinCooldownSeconds, model.HealthCheckMaxCooldownSeconds)

	g.rateLimitEnabled = settings.Bool(g.settings, ConfigRateLimitEnabled, g.providerRateLimitEnabled)
	g.rateLimitRPS = settings.Int(g.log, g.settings, ConfigRateLimitRPS, g.providerRateLimitRPS, model.RateLimitMinRPS, model.RateLimitMaxRPS)
	g.rateLimitBurst = settings.Int(g.log, g.settings, ConfigRateLimitBurst, g.providerRateLimitBurst, model.RateLimitMinBurst, model.RateLimitMaxBurst)

	g.setInterfaces(interfaces)

	return g
}

// setInterfaces collects usable guest addresses. Loopback interfaces,
// loopback and link-local addresses are dropped, IPv4 addresses are
// preferred over IPv6, the primary interface (eth0) over the rest, and
// interfaces are visited in name order for deterministic results.
func (g *guest) setInterfaces(interfaces []GuestInterface) {
	g.log.Trace().Msg("start setInterfaces")
	defer g.log.Trace().Msg("end setInterfaces")

	names := make([]string, 0, len(interfaces))
	byName := make(map[string]GuestInterface, len(interfaces))
	for _, iface := range interfaces {
		if iface.Name == loopbackInterface {
			continue
		}
		if _, ok := byName[iface.Name]; !ok {
			names = append(names, iface.Name)
		}
		byName[iface.Name] = iface
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
		for _, addr := range byName[name].Addresses {
			switch {
			case addr.IsLoopback(), addr.IsLinkLocalUnicast(), addr.IsMulticast():
				continue
			}

			if addr.Is4() || addr.Is4In6() {
				ipv4 = append(ipv4, addr.Unmap())
			} else {
				ipv6 = append(ipv6, addr)
			}
		}
	}

	g.ips = append(ipv4, ipv6...)
}

// newProxyConfig method returns a new model.Config for the guest.
func (g *guest) newProxyConfig(ctx context.Context, targetID string) (*model.Config, error) {
	g.log.Trace().Msg("New ProxyConfig")
	defer g.log.Trace().Msg("End New ProxyConfig")

	hostname, err := g.getProxyHostname()
	if err != nil {
		return nil, fmt.Errorf("error parsing Hostname: %w", err)
	}

	tailscale, err := g.getTailscaleConfig()
	if err != nil {
		return nil, err
	}

	pcfg, err := model.NewConfig()
	if err != nil {
		return nil, err
	}

	pcfg.TargetID = targetID
	pcfg.TargetImage = g.tags
	pcfg.Hostname = hostname
	pcfg.TargetProvider = g.targetProviderName
	pcfg.Tailscale = *tailscale
	pcfg.ProxyProvider = settings.String(g.settings, ConfigProxyProvider, model.DefaultProxyProvider)
	pcfg.Domain = settings.String(g.settings, ConfigDomain, "")
	pcfg.DNSProvider = settings.String(g.settings, ConfigDNSProvider, "")
	pcfg.TLSProvider = settings.String(g.settings, ConfigTLSProvider, "")
	pcfg.ProxyAccessLog = settings.Bool(g.settings, ConfigContainerAccessLog, g.proxyAccessLogDefault)
	pcfg.IdentityHeaders = settings.Bool(g.settings, ConfigIdentityHeaders, model.DefaultIdentityHeaders)
	pcfg.AutoRestart = g.autoRestart
	pcfg.HealthCheckEnabled = g.healthCheckEnabled
	pcfg.HealthCheckInterval = g.healthCheckInterval
	pcfg.HealthCheckFailures = g.healthCheckFailures
	pcfg.HealthCheckCooldown = g.healthCheckCooldown
	pcfg.RateLimitEnabled = g.rateLimitEnabled
	pcfg.RateLimitRPS = g.rateLimitRPS
	pcfg.RateLimitBurst = g.rateLimitBurst
	pcfg.Dashboard.Visible = settings.Bool(g.settings, ConfigDashboardVisible, model.DefaultDashboardVisible)
	pcfg.Dashboard.Label = settings.String(g.settings, ConfigDashboardLabel, pcfg.Hostname)

	pcfg.Dashboard.Category = settings.String(g.settings, ConfigDashboardCategory, "")
	pcfg.Dashboard.Icon = settings.String(g.settings, ConfigDashboardIcon, "")
	if pcfg.Dashboard.Icon == "" {
		pcfg.Dashboard.Icon = g.assets.GuessIcon(g.iconHint())
	}

	pcfg.Ports = g.getPorts(ctx)

	return pcfg, nil
}

// iconHint feeds icon guessing. Proxmox has no image concept; the guest
// tags are the closest analog (often distro names).
func (g *guest) iconHint() string {
	return strings.ReplaceAll(g.tags, ";", " ")
}

// getPorts returns the port configuration from both notes styles. Flat
// tsdproxy.port.* settings use the label grammar shared with Docker and
// Incus; list-style ports entries use explicit targets (or a generated
// target from the guest address). Keys cannot collide, so both merge into
// one list.
func (g *guest) getPorts(ctx context.Context) model.PortConfigList {
	ports := model.Ports(ctx, g.log, g.settings, ConfigPort, g.portOptionGates(), g.generateTargetFromFirstTarget)

	for label, entry := range g.listPorts {
		g.processListPort(ports, label, entry)
	}

	return ports
}

// processListPort turns a list-style port entry into a PortConfig and adds
// it to the list. The label is a short ("443/https") or long
// ("443/https:8080/http") port label.
func (g *guest) processListPort(ports model.PortConfigList, label string, entry notesPort) {
	if model.IsPortRangeShortLabel(label) {
		g.processListPortRange(ports, label, entry)
		return
	}

	cfg, err := g.newPortFromLabel(label)
	if err != nil {
		g.log.Error().Err(err).Str("port", label).Msg("error creating port config")
		return
	}

	if g.applyListPort(&cfg, entry, label) {
		ports[label] = cfg
	}
}

func (g *guest) processListPortRange(ports model.PortConfigList, label string, entry notesPort) {
	expanded, err := model.ExpandPortRangeShortLabel(label)
	if err != nil {
		g.log.Error().Err(err).Str("port", label).Msg("error expanding port range")
		return
	}

	for rangeKey, cfg := range expanded {
		portCfg := cfg
		if g.applyListPort(&portCfg, entry, label) {
			ports[label+"."+rangeKey] = portCfg
		}
	}
}

// newPortFromLabel parses a port label. Labels carrying a target part
// ("<proxy>:<target>" or a redirect URL) use the long form.
func (g *guest) newPortFromLabel(label string) (model.PortConfig, error) {
	if strings.Contains(label, ":") || strings.Contains(label, redirectSeparator) {
		return model.NewPortLongLabel(label)
	}
	return model.NewPortShortLabel(label)
}

// applyListPort applies a list-style entry to a port config, resolving
// targets: explicit targets are used verbatim (list provider semantics);
// when the entry has none, the target URL is generated from the guest
// address using the label's target part, or the proxy port. Returns false
// when the port has no usable target and must be dropped.
func (g *guest) applyListPort(cfg *model.PortConfig, entry notesPort, label string) bool {
	cfg.IsRedirect = entry.IsRedirect
	cfg.TLSValidate = g.gateTLSValidate(entry.TLSValidate, label)
	cfg.Tailscale = model.TailscalePort{Funnel: g.gateFunnel(entry.Tailscale.Funnel, label)}

	if len(entry.Targets) > 0 {
		return g.addExplicitTargets(cfg, entry.Targets, label)
	}

	if cfg.IsRedirect {
		g.log.Error().Str("port", label).Msg("redirect port requires explicit targets")
		return false
	}

	return g.addGeneratedTarget(cfg, label)
}

// gateTLSValidate enforces the operator's allowTlsValidateDisable gate on
// list-style entries, mirroring the no_tlsvalidate port option policy.
func (g *guest) gateTLSValidate(tlsValidate bool, label string) bool {
	if tlsValidate || g.allowTLSValidateDisable {
		return tlsValidate
	}
	g.log.Warn().Str("port", label).
		Msg("requested tlsValidate=false but operator has not enabled allowTlsValidateDisable; ignoring")
	return true
}

// gateFunnel enforces the operator's allowGuestFunnel gate on list-style
// entries, mirroring the tailscale_funnel port option policy.
func (g *guest) gateFunnel(funnel bool, label string) bool {
	if !funnel || g.allowGuestFunnel {
		return funnel
	}
	g.log.Warn().Str("port", label).
		Msg("requested funnel but operator has not enabled allowGuestFunnel; ignoring")
	return false
}

// addExplicitTargets parses explicit target URLs. The first one replaces
// the label placeholder of a long label, the rest are appended.
func (g *guest) addExplicitTargets(cfg *model.PortConfig, targets []string, label string) bool {
	placeholder := g.labelPlaceholder(cfg)
	added := false

	for _, raw := range targets {
		targetURL, err := url.Parse(raw)
		if err != nil || targetURL.Scheme == "" || targetURL.Host == "" {
			g.log.Error().Err(err).Str("port", label).Str("targetUrl", raw).Msg("invalid target URL")
			continue
		}

		if placeholder != nil {
			cfg.ReplaceTarget(placeholder, targetURL)
			placeholder = nil
		} else {
			cfg.AddTarget(targetURL)
		}
		added = true
	}

	if !added {
		g.log.Error().Str("port", label).Msg("no valid targets found for port")
	}

	return added
}

// addGeneratedTarget builds the target URL from the guest address: the
// provider targetHostname when set, else the first guest address. Long
// labels carry their own target scheme and port in the placeholder; short
// labels use the default scheme for the proxy protocol and the proxy port.
func (g *guest) addGeneratedTarget(cfg *model.PortConfig, label string) bool {
	host := g.defaultTargetHostname
	if host == "" {
		if len(g.ips) == 0 {
			g.log.Error().Err(ErrNoAddressFound).Str("port", label).Msg("no guest address and no targetHostname to generate target URL")
			return false
		}
		host = g.ips[0].String()
	}

	if placeholder := g.labelPlaceholder(cfg); placeholder != nil {
		targetURL, err := url.Parse(placeholder.Scheme + "://" + net.JoinHostPort(host, placeholder.Port()))
		if err != nil {
			g.log.Error().Err(err).Str("port", label).Msg("error generating target URL")
			return false
		}
		cfg.ReplaceTarget(placeholder, targetURL)
		g.log.Debug().Str("port", label).Str("target", targetURL.String()).Msg("target URL")
		return true
	}

	targetURL, err := url.Parse(targetSchemeFor(cfg.ProxyProtocol) + "://" + net.JoinHostPort(host, strconv.Itoa(cfg.ProxyPort)))
	if err != nil {
		g.log.Error().Err(err).Str("port", label).Msg("error generating target URL")
		return false
	}
	cfg.AddTarget(targetURL)
	g.log.Debug().Str("port", label).Str("target", targetURL.String()).Msg("target URL")

	return true
}

// labelPlaceholder returns the 0.0.0.0 target long labels insert to carry
// the target scheme and port, if any.
func (g *guest) labelPlaceholder(cfg *model.PortConfig) *url.URL {
	for _, target := range cfg.GetTargets() {
		if target.Hostname() == placeholderHost {
			return target
		}
	}
	return nil
}

// targetSchemeFor mirrors the model's default target protocol mapping.
func targetSchemeFor(proxyProtocol string) string {
	switch proxyProtocol {
	case model.ProtoTCP:
		return model.ProtoTCP
	case model.ProtoUDP:
		return model.ProtoUDP
	default:
		return model.ProtoHTTP
	}
}

// portOptionGates returns the provider's port option policy from the
// operator's settings. Proxmox has no autodetection, so no_autodetect is
// not supported.
func (g *guest) portOptionGates() model.PortOptionGates {
	return model.PortOptionGates{
		AllowTLSValidateDisable: g.allowTLSValidateDisable,
		AllowFunnel:             g.allowGuestFunnel,
		FunnelSettingName:       "allowGuestFunnel",
		SupportNoAutoDetect:     false,
	}
}

// generateTargetFromFirstTarget resolves the port's target URL against the
// guest network. Multiple targets are not supported in this TargetProvider.
func (g *guest) generateTargetFromFirstTarget(ctx context.Context, port model.PortConfig) (model.PortConfig, error) {
	g.log.Trace().Msg("generateTargetFromFirstTarget")
	defer g.log.Trace().Msg("End generateTargetFromFirstTarget")

	select {
	case <-ctx.Done():
		return port, ctx.Err()
	default:
	}

	p := port.GetFirstTarget()
	if p == nil {
		return port, fmt.Errorf("no target URL for port %s", port.String())
	}

	targetURL, err := g.getTargetURL(p)
	if err != nil {
		return port, err
	}
	g.log.Debug().Str("port", port.String()).Str("target", targetURL.String()).Msg("target URL")

	port.ReplaceTarget(p, targetURL)

	return port, nil
}

// getTailscaleConfig method returns the tailscale configuration.
func (g *guest) getTailscaleConfig() (*model.Tailscale, error) {
	g.log.Trace().Msg("getTailscaleConfig")
	defer g.log.Trace().Msg("End getTailscaleConfig")

	authKey := settings.String(g.settings, ConfigAuthKey, "")

	authKeySecret, err := settings.AuthKeyFromFile(g.settings, ConfigAuthKeyFile, authKey)
	if err != nil {
		return nil, fmt.Errorf("error setting auth key from file : %w", err)
	}

	tags := settings.String(g.settings, ConfigTags, "")

	return &model.Tailscale{
		Ephemeral:    settings.Bool(g.settings, ConfigEphemeral, model.DefaultTailscaleEphemeral),
		RunWebClient: settings.Bool(g.settings, ConfigRunWebClient, model.DefaultTailscaleRunWebClient),
		Verbose:      settings.Bool(g.settings, ConfigTsnetVerbose, model.DefaultTailscaleVerbose),
		AuthKey:      authKeySecret,
		Tags:         tags,
	}, nil
}

// getTargetURL returns the target URL for the guest: the configured
// targetHostname (provider-level fallback) or the first guest address,
// with the port's target port.
func (g *guest) getTargetURL(iPort *url.URL) (*url.URL, error) {
	g.log.Trace().Msg("getTargetURL")
	defer g.log.Trace().Msg("End getTargetURL")

	port := iPort.Port()
	if port == "" {
		return nil, fmt.Errorf("%w: %s", ErrNoAddressFound, g.name)
	}

	host := g.defaultTargetHostname
	if host == "" {
		if len(g.ips) == 0 {
			return nil, fmt.Errorf("%w: %s", ErrNoAddressFound, g.name)
		}
		host = g.ips[0].String()
	}

	return url.Parse(iPort.Scheme + "://" + net.JoinHostPort(host, port))
}

// getProxyHostname method returns the proxy hostname from the guest
// settings, falling back to the guest name.
func (g *guest) getProxyHostname() (string, error) {
	g.log.Trace().Msg("getProxyHostname")
	defer g.log.Trace().Msg("End getProxyHostname")

	if customName, ok := g.settings[ConfigName]; ok {
		if !rfc1123Hostname.MatchString(customName) {
			return "", fmt.Errorf("invalid hostname %q: must match RFC 1123 (alphanumeric, hyphens, 1-63 chars)", customName)
		}
		return strings.ToLower(customName), nil
	}

	return strings.ToLower(g.name), nil
}

func withTargetProviderName(name string) GuestOption {
	return func(g *guest) {
		g.targetProviderName = name
	}
}

func withDefaultTargetHostname(hostname string) GuestOption {
	return func(g *guest) {
		g.defaultTargetHostname = hostname
	}
}

func withProviderAutoRestart(autoRestart bool) GuestOption {
	return func(g *guest) {
		g.providerAutoRestart = autoRestart
	}
}

func withProviderHealthCheck(enabled bool, interval, failures, cooldown int) GuestOption {
	return func(g *guest) {
		g.providerHealthEnabled = enabled
		g.providerHealthInterval = interval
		g.providerHealthFailures = failures
		g.providerHealthCooldown = cooldown
	}
}

func withProviderRateLimit(enabled bool, rps, burst int) GuestOption {
	return func(g *guest) {
		g.providerRateLimitEnabled = enabled
		g.providerRateLimitRPS = rps
		g.providerRateLimitBurst = burst
	}
}

func withProxyAccessLogDefault(defaultVal bool) GuestOption {
	return func(g *guest) {
		g.proxyAccessLogDefault = defaultVal
	}
}

func withAssets(assets *web.Assets) GuestOption {
	return func(g *guest) {
		g.assets = assets
	}
}

func withAllowGuestFunnel(allowed bool) GuestOption {
	return func(g *guest) {
		g.allowGuestFunnel = allowed
	}
}

func withAllowTLSValidateDisable(allowed bool) GuestOption {
	return func(g *guest) {
		g.allowTLSValidateDisable = allowed
	}
}
