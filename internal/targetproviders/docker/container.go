// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package docker

import (
	"context"
	"fmt"
	"net/netip"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/almeidapaulopt/tsdproxy/internal/model"
	"github.com/almeidapaulopt/tsdproxy/web"

	ctypes "github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/swarm"
	"github.com/rs/zerolog"
)

var rfc1123Hostname = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?$`)

type (
	container struct {
		log                      zerolog.Logger
		defaultBridgeAddress     netip.Addr
		ports                    map[string]string
		labels                   map[string]string
		assets                   *web.Assets
		image                    string
		id                       string
		targetProviderName       string
		name                     string
		hostname                 string
		networkMode              ctypes.NetworkMode
		defaultTargetHostname    string
		ipAddress                []netip.Addr
		gateways                 []netip.Addr
		healthCheckCooldown      int
		providerRateLimitRPS     int
		healthCheckInterval      int
		providerHealthFailures   int
		providerHealthCooldown   int
		providerHealthInterval   int
		rateLimitBurst           int
		rateLimitRPS             int
		providerRateLimitBurst   int
		healthCheckFailures      int
		proxyAccessLogDefault    bool
		providerAutoRestart      bool
		providerRateLimitEnabled bool
		autodetect               bool
		providerHealthEnabled    bool
		rateLimitEnabled         bool
		healthCheckEnabled       bool
		autoRestart              bool
		allowContainerFunnel     bool
		allowTLSValidateDisable  bool
	}

	ContainerOption func(*container)
)

func newContainer(logger zerolog.Logger, dcontainer ctypes.InspectResponse, dservice swarm.Service,
	providerAutoDetect bool, opts ...ContainerOption,
) *container {
	newlog := logger.With().Str("container", dcontainer.Name).Logger()
	newlog.Trace().Msg("New Container")
	defer newlog.Trace().Msg("End New Container")

	c := &container{
		log:         newlog,
		id:          dcontainer.ID,
		name:        dcontainer.Name,
		hostname:    dcontainer.Config.Hostname,
		networkMode: dcontainer.HostConfig.NetworkMode,
		image:       dcontainer.Config.Image,
		labels:      dcontainer.Config.Labels,
		ports:       make(map[string]string),
	}

	for _, opt := range opts {
		opt(c)
	}

	c.autodetect = c.getLabelBool(LabelAutoDetect, providerAutoDetect)
	c.autoRestart = c.getLabelBool(LabelAutoRestart, c.providerAutoRestart)
	c.healthCheckEnabled = c.getLabelBool(LabelHealthCheckEnabled, c.providerHealthEnabled)
	c.healthCheckInterval = c.getLabelInt(LabelHealthCheckInterval, c.providerHealthInterval, 1, healthCheckMaxIntervalSeconds)
	c.healthCheckFailures = c.getLabelInt(LabelHealthCheckFailures, c.providerHealthFailures, 1, healthCheckMaxFailures)
	c.healthCheckCooldown = c.getLabelInt(LabelHealthCheckCooldown, c.providerHealthCooldown, 0, healthCheckMaxCooldownSeconds)

	c.rateLimitEnabled = c.getLabelBool(LabelRateLimitEnabled, c.providerRateLimitEnabled)
	c.rateLimitRPS = c.getLabelInt(LabelRateLimitRPS, c.providerRateLimitRPS, model.RateLimitMinRPS, model.RateLimitMaxRPS)
	c.rateLimitBurst = c.getLabelInt(LabelRateLimitBurst, c.providerRateLimitBurst, model.RateLimitMinBurst, model.RateLimitMaxBurst)

	c.setContainerPorts(dcontainer, dservice)
	c.setContainerNetwork(dcontainer)

	return c
}

func (c *container) setContainerPorts(dcontainer ctypes.InspectResponse, dservice swarm.Service) {
	c.log.Trace().Msg("start setContainerPorts")
	defer c.log.Trace().Msg("end setContainerPorts")

	if c.networkMode.IsHost() {
		for p := range dcontainer.HostConfig.PortBindings {
			c.ports[p.Port()] = p.Port()
		}
		return
	}

	if dcontainer.NetworkSettings == nil {
		return
	}

	for p, b := range dcontainer.NetworkSettings.Ports {
		if len(b) > 0 {
			c.ports[p.Port()] = b[0].HostPort
		}
	}

	for _, b := range dservice.Endpoint.Ports {
		if _, ok := c.ports[strconv.Itoa(int(b.TargetPort))]; ok {
			continue
		}
		c.ports[strconv.Itoa(int(b.TargetPort))] = strconv.Itoa(int(b.PublishedPort))
	}
}

func (c *container) setContainerNetwork(dcontainer ctypes.InspectResponse) {
	c.log.Trace().Msg("start setContainerNetwork")
	defer c.log.Trace().Msg("end setContainerNetwork")

	if dcontainer.NetworkSettings == nil {
		return
	}

	// Collect network entries for deterministic ordering.
	// Go map iteration order is non-deterministic, which makes c.ipAddress[0]
	// unreliable for multi-network containers.
	type networkEntry struct {
		ip   netip.Addr
		gw   netip.Addr
		name string
	}
	var entries []networkEntry

	for name, network := range dcontainer.NetworkSettings.Networks {
		entries = append(entries, networkEntry{
			name: name,
			ip:   network.IPAddress,
			gw:   network.Gateway,
		})
	}

	// Sort by network name for stable ordering.
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].name < entries[j].name
	})

	// Prefer the network whose gateway matches defaultBridgeAddress.
	// This ensures the proxy connects via the expected Docker network.
	if c.defaultBridgeAddress.IsValid() {
		sort.SliceStable(entries, func(i, j int) bool {
			iMatch := entries[i].gw == c.defaultBridgeAddress
			jMatch := entries[j].gw == c.defaultBridgeAddress
			return iMatch && !jMatch
		})
	}

	for _, entry := range entries {
		if entry.ip.IsValid() {
			c.ipAddress = append(c.ipAddress, entry.ip)
		}
		if entry.gw.IsValid() {
			c.gateways = append(c.gateways, entry.gw)
		}
	}
}

func (c *container) newProxyConfig(ctx context.Context) (*model.Config, error) {
	c.log.Trace().Msg("New ProxyConfig")
	defer c.log.Trace().Msg("End New ProxyConfig")

	hostname, err := c.getProxyHostname()
	if err != nil {
		return nil, fmt.Errorf("error parsing Hostname: %w", err)
	}

	tailscale, err := c.getTailscaleConfig()
	if err != nil {
		return nil, err
	}

	pcfg, err := model.NewConfig()
	if err != nil {
		return nil, err
	}

	pcfg.TargetID = c.id
	pcfg.TargetImage = c.image
	pcfg.Hostname = hostname
	pcfg.TargetProvider = c.targetProviderName
	pcfg.Tailscale = *tailscale
	pcfg.ProxyProvider = c.getLabelString(LabelProxyProvider, model.DefaultProxyProvider)
	pcfg.Domain = c.getLabelString(LabelDomain, "")
	pcfg.DNSProvider = c.getLabelString(LabelDNSProvider, "")
	pcfg.TLSProvider = c.getLabelString(LabelTLSProvider, "")
	pcfg.ProxyAccessLog = c.getLabelBool(LabelContainerAccessLog, c.proxyAccessLogDefault)
	pcfg.IdentityHeaders = c.getLabelBool(LabelIdentityHeaders, model.DefaultIdentityHeaders)
	pcfg.AutoRestart = c.autoRestart
	pcfg.HealthCheckEnabled = c.healthCheckEnabled
	pcfg.HealthCheckInterval = c.healthCheckInterval
	pcfg.HealthCheckFailures = c.healthCheckFailures
	pcfg.HealthCheckCooldown = c.healthCheckCooldown
	pcfg.RateLimitEnabled = c.rateLimitEnabled
	pcfg.RateLimitRPS = c.rateLimitRPS
	pcfg.RateLimitBurst = c.rateLimitBurst
	pcfg.Dashboard.Visible = c.getLabelBool(LabelDashboardVisible, model.DefaultDashboardVisible)
	pcfg.Dashboard.Label = c.getLabelString(LabelDashboardLabel, pcfg.Hostname)

	pcfg.Dashboard.Category = c.getLabelString(LabelDashboardCategory, "")
	pcfg.Dashboard.Icon = c.getLabelString(LabelDashboardIcon, "")
	if pcfg.Dashboard.Icon == "" {
		pcfg.Dashboard.Icon = c.assets.GuessIcon(c.image)
	}

	pcfg.Ports = c.getPorts(ctx)

	// add port from legacy labels if no port configured
	if len(pcfg.Ports) == 0 {
		if legacyPort, err := c.getLegacyPort(ctx); err == nil {
			pcfg.Ports["legacy"] = legacyPort
		}
	}

	return pcfg, nil
}

func (c *container) getPorts(ctx context.Context) model.PortConfigList {
	c.log.Trace().Msg("getPorts")
	defer c.log.Trace().Msg("End getPorts")

	ports := make(model.PortConfigList)
	for k, v := range c.labels {
		if !strings.HasPrefix(k, LabelPort) {
			continue
		}

		parts := strings.Split(v, ",")

		configStr := parts[0]

		if model.IsPortRangeLabel(configStr) {
			expanded, err := model.ExpandPortRangeLabel(configStr)
			if err != nil {
				c.log.Error().Err(err).Str("port", k).Msg("error expanding port range")
				continue
			}

			for rangeKey, port := range expanded {
				c.applyPortOptions(k, &port, parts[1:])

				if !port.IsRedirect {
					port, err = c.generateTargetFromFirstTarget(ctx, port)
					if err != nil {
						c.log.Error().Err(err).Str("port", k).Msg("error generating target for range port")
						continue
					}
				}

				expandedKey := k + "." + rangeKey
				ports[expandedKey] = port
			}
			continue
		}

		port, err := model.NewPortLongLabel(parts[0])
		if err != nil {
			c.log.Error().Err(err).Str("port", k).Msg("error creating port config")
			continue
		}

		c.applyPortOptions(k, &port, parts[1:])

		if !port.IsRedirect {
			port, err = c.generateTargetFromFirstTarget(ctx, port)
			if err != nil {
				c.log.Error().Err(err).Str("port", k).Msg("error generating target")
				continue
			}
		}

		ports[k] = port
	}

	return ports
}

func (c *container) applyPortOptions(labelKey string, port *model.PortConfig, options []string) {
	for _, opt := range options {
		opt = strings.TrimSpace(opt)
		if strings.HasPrefix(opt, PortOptionLoadBalance+"=") {
			strategy := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(opt, PortOptionLoadBalance+"=")))
			switch strategy {
			case model.LoadBalanceFirst, model.LoadBalanceRoundRobin:
				port.LoadBalance = strategy
			default:
				c.log.Warn().Str("option", opt).Str("port", labelKey).Str("strategy", strategy).
					Msg("unknown loadbalance strategy; defaulting to first")
				port.LoadBalance = model.LoadBalanceFirst
			}
			continue
		}
		switch opt {
		case PortOptionNoTLSValidate:
			if !c.allowTLSValidateDisable {
				c.log.Warn().Str("option", opt).Str("port", labelKey).
					Msg("container requested no_tlsvalidate but operator has not enabled allowTLSValidateDisable; ignoring")
				continue
			}
			port.TLSValidate = false
		case PortOptionTailscaleFunnel:
			if !c.allowContainerFunnel {
				c.log.Warn().Str("option", opt).Str("port", labelKey).
					Msg("container requested tailscale_funnel but operator has not enabled allowContainerFunnel; ignoring")
				continue
			}
			port.Tailscale.Funnel = true
		case PortOptionNoAutoDetect:
			port.NoAutoDetect = true
		default:
			c.log.Warn().Str("option", opt).Str("port", labelKey).
				Msg("unrecognized port option (valid: no_tlsvalidate, tailscale_funnel, no_autodetect, loadbalance=first|roundrobin)")
		}
	}
}

func (c *container) generateTargetFromFirstTarget(ctx context.Context, port model.PortConfig) (model.PortConfig, error) {
	c.log.Trace().Msg("generateTargetFromFirstTarget")
	defer c.log.Trace().Msg("End generateTargetFromFirstTarget")

	// multiple targets not supported in this TargetProvider
	p := port.GetFirstTarget()
	if p == nil {
		return port, fmt.Errorf("no target URL for port %s", port.String())
	}

	targetURL, err := c.getTargetURL(ctx, p, port.NoAutoDetect)
	if err != nil {
		return port, err
	}
	c.log.Debug().Str("port", port.String()).Str("target", targetURL.String()).Msg("target URL")

	port.ReplaceTarget(p, targetURL)

	return port, nil
}

func (c *container) getTailscaleConfig() (*model.Tailscale, error) {
	c.log.Trace().Msg("getTailscaleConfig")
	defer c.log.Trace().Msg("End getTailscaleConfig")

	authKey := c.getLabelString(LabelAuthKey, "")

	authKeySecret, err := c.getAuthKeyFromAuthFile(authKey)
	if err != nil {
		return nil, fmt.Errorf("error setting auth key from file : %w", err)
	}

	tags := c.getLabelString(LabelTags, "")

	return &model.Tailscale{
		Ephemeral:    c.getLabelBool(LabelEphemeral, model.DefaultTailscaleEphemeral),
		RunWebClient: c.getLabelBool(LabelRunWebClient, model.DefaultTailscaleRunWebClient),
		Verbose:      c.getLabelBool(LabelTsnetVerbose, model.DefaultTailscaleVerbose),
		AuthKey:      authKeySecret,
		Tags:         tags,
	}, nil
}

func (c *container) getName() string {
	return strings.TrimLeft(c.name, "/")
}

// getTargetURL method returns the container target URL by trying resolution
// strategies in priority order.
func (c *container) getTargetURL(ctx context.Context, iPort *url.URL, noAutoDetect bool) (*url.URL, error) {
	c.log.Trace().Msg("getTargetURL")
	defer c.log.Trace().Msg("End getTargetURL")

	internalPort := iPort.Port()
	publishedPort := c.getPublishedPort(internalPort)

	if internalPort == "" && publishedPort == "" {
		return nil, ErrNoPortFoundInContainer
	}

	// Try resolvers in priority order.
	if u, ok := c.resolveSelfHost(iPort.Scheme, internalPort); ok {
		return u, nil
	}

	if u, ok := c.resolveByProbing(ctx, iPort.Scheme, internalPort, publishedPort, noAutoDetect); ok {
		return u, nil
	}

	if u, ok := c.resolvePublished(iPort, publishedPort, internalPort); ok {
		return u, nil
	}

	// Bridge-mode fallbacks: gateway via published port, then direct container IP.
	if !c.networkMode.IsHost() {
		if u, ok := c.resolveViaGateway(iPort.Scheme, publishedPort); ok {
			return u, nil
		}

		if u, ok := c.resolveContainerIP(iPort.Scheme, internalPort); ok {
			return u, nil
		}
	}

	return nil, ErrNoPortFoundInContainer
}

// resolveSelfHost returns a localhost target when the container IS the tsdproxy process.
func (c *container) resolveSelfHost(scheme, internalPort string) (*url.URL, bool) {
	osname, err := os.Hostname()
	if err != nil {
		return nil, false
	}
	if osname == "" || c.hostname != osname {
		return nil, false
	}
	u, err := url.Parse(scheme + "://127.0.0.1:" + internalPort)
	return u, err == nil
}

// resolveByProbing tries to auto-detect the target URL by probing connectivity.
func (c *container) resolveByProbing(ctx context.Context, scheme, internalPort, publishedPort string, noAutoDetect bool) (*url.URL, bool) {
	if !c.autodetect || noAutoDetect {
		return nil, false
	}
	for try := range autoDetectTries {
		c.log.Info().Int("try", try).Msg("Trying to auto detect target URL")
		if port, err := c.tryConnectContainer(scheme, internalPort, publishedPort); err == nil {
			return port, true
		}
		select {
		case <-ctx.Done():
			return nil, false
		case <-time.After(autoDetectSleep):
		}
	}
	return nil, false
}

// resolveViaGateway tries to reach the container through the Docker network
// gateway using the published port. Works across networks since published
// ports are accessible on the host, which is typically the gateway.
func (c *container) resolveViaGateway(scheme, publishedPort string) (*url.URL, bool) {
	if publishedPort == "" {
		return nil, false
	}
	for _, gateway := range c.gateways {
		gw := gateway.String()
		u, err := url.Parse(scheme + "://" + gw + ":" + publishedPort)
		if err != nil {
			continue
		}

		c.log.Info().
			Str("scheme", scheme).
			Str("gateway", gw).
			Str("port", publishedPort).
			Msg("resolving via gateway and published port")

		return u, true
	}
	return nil, false
}

// resolveContainerIP connects directly to the container's internal IP.
// Last-resort fallback: only works when tsdproxy shares a Docker network
// with the target container.
func (c *container) resolveContainerIP(scheme, internalPort string) (*url.URL, bool) {
	if len(c.ipAddress) == 0 || internalPort == "" {
		return nil, false
	}

	ip := c.ipAddress[0].String()
	c.log.Info().
		Str("scheme", scheme).
		Str("container_ip", ip).
		Str("internal_port", internalPort).
		Msg("resolving via direct container IP (requires shared network)")

	u, err := url.Parse(scheme + "://" + ip + ":" + internalPort)
	return u, err == nil
}

// resolvePublished resolves the target URL using the published port or
// falls back to the default hostname with the internal port.
//
// The internal-port fallback is allowed when the user explicitly declared the
// target port via a tsdproxy.container_port or tsdproxy.port.* label — they are
// stating the port is reachable at defaultTargetHostname. For auto-detected
// ports (no explicit label, port inferred from Docker bindings) the fallback is
// restricted to host-network containers only: a bridge-mode container's
// internal port is isolated and falling back here would silently route to
// whatever happens to listen on that port on the Docker host (e.g. TSDProxy's
// own dashboard).
func (c *container) resolvePublished(iPort *url.URL, publishedPort, internalPort string) (*url.URL, bool) {
	if c.defaultTargetHostname == "" {
		return nil, false
	}
	port := publishedPort
	if port == "" {
		if !c.networkMode.IsHost() && !c.hasExplicitPortLabel() {
			return nil, false
		}
		port = internalPort
	}
	if port == "" {
		return nil, false
	}
	u, err := url.Parse(iPort.Scheme + "://" + c.defaultTargetHostname + ":" + port)
	return u, err == nil
}

// hasExplicitPortLabel reports whether the user declared the target port via a
// tsdproxy.container_port or tsdproxy.port.* label (as opposed to auto-detecting
// the port from the container's Docker port bindings).
func (c *container) hasExplicitPortLabel() bool {
	if _, ok := c.labels[LabelContainerPort]; ok {
		return true
	}
	for key := range c.labels {
		if strings.HasPrefix(key, LabelPort) {
			return true
		}
	}
	return false
}

func (c *container) getPublishedPort(internalPort string) string {
	c.log.Trace().Msg("getPublishedPort")
	defer c.log.Trace().Msg("End getPublishedPort")

	for internal, published := range c.ports {
		if internal == internalPort {
			return published
		}
	}

	return ""
}

func (c *container) getProxyHostname() (string, error) {
	c.log.Trace().Msg("getProxyHostname")
	defer c.log.Trace().Msg("End getProxyHostname")

	if customName, ok := c.labels[LabelName]; ok {
		if !rfc1123Hostname.MatchString(customName) {
			return "", fmt.Errorf("invalid hostname %q: must match RFC 1123 (alphanumeric, hyphens, 1-63 chars)", customName)
		}
		return strings.ToLower(customName), nil
	}

	return c.getName(), nil
}

func withTargetProviderName(name string) ContainerOption {
	return func(c *container) {
		c.targetProviderName = name
	}
}

func withDefaultBridgeAddress(address netip.Addr) ContainerOption {
	return func(c *container) {
		c.defaultBridgeAddress = address
	}
}

func withDefaultTargetHostname(hostname string) ContainerOption {
	return func(c *container) {
		c.defaultTargetHostname = hostname
	}
}

func withProviderAutoRestart(autoRestart bool) ContainerOption {
	return func(c *container) {
		c.providerAutoRestart = autoRestart
	}
}

func withProviderHealthCheck(enabled bool, interval, failures, cooldown int) ContainerOption {
	return func(c *container) {
		c.providerHealthEnabled = enabled
		c.providerHealthInterval = interval
		c.providerHealthFailures = failures
		c.providerHealthCooldown = cooldown
	}
}

func withProviderRateLimit(enabled bool, rps, burst int) ContainerOption {
	return func(c *container) {
		c.providerRateLimitEnabled = enabled
		c.providerRateLimitRPS = rps
		c.providerRateLimitBurst = burst
	}
}

func withProxyAccessLogDefault(defaultVal bool) ContainerOption {
	return func(c *container) {
		c.proxyAccessLogDefault = defaultVal
	}
}

func withAssets(assets *web.Assets) ContainerOption {
	return func(c *container) {
		c.assets = assets
	}
}

func withAllowContainerFunnel(allowed bool) ContainerOption {
	return func(c *container) {
		c.allowContainerFunnel = allowed
	}
}

func withAllowTLSValidateDisable(allowed bool) ContainerOption {
	return func(c *container) {
		c.allowTLSValidateDisable = allowed
	}
}
