// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package model

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync"
)

type (
	PortConfig struct {
		targets       *targetState
		name          string        `validate:"string" yaml:"name"`
		ProxyProtocol string        `validate:"string" yaml:"proxyProtocol"`
		ProxyPort     int           `validate:"hostname_port" yaml:"proxyPort"`
		TLSValidate   bool          `validate:"boolean" yaml:"tlsValidate"`
		NoAutoDetect  bool          `validate:"boolean" yaml:"noAutoDetect"`
		IsRedirect    bool          `validate:"boolean" yaml:"isRedirect"`
		Tailscale     TailscalePort `validate:"dive" yaml:"tailscale"`
	}

	TailscalePort struct {
		Funnel bool `validate:"boolean" yaml:"funnel"`
	}

	targetState struct {
		targets []*url.URL
		mtx     sync.RWMutex
	}
)

const (
	redirectSeparator = "->"
	proxySeparator    = ":"
	protocolSeparator = "/"
	splitPairCount    = 2

	ProtoHTTPS = "https"
	ProtoHTTP  = "http"
	ProtoTCP   = "tcp"
	ProtoUDP   = "udp"

	TLSProviderTailscale = "tailscale"
	TLSProviderACME      = "acme"

	DNSProviderCloudflare = "cloudflare"
	DNSProviderMagicDNS   = "magicdns"

	rangeKeyPrefix = "range_0"
)

var (
	ErrInvalidPortFormat   = errors.New("invalid format, missing '" + protocolSeparator + "' or '" + redirectSeparator + "'")
	ErrInvalidProxyConfig  = errors.New("invalid proxy configuration")
	ErrInvalidTargetConfig = errors.New("invalid target configuration")
)

const (
	minPort     = 1
	maxPort     = 65535
	rangeSep    = "-"
	maxRangeLen = 1000
)

// validatePortRange checks that a port number is within the valid range.
func validatePortRange(port int) error {
	if port < minPort || port > maxPort {
		return fmt.Errorf("port %d out of valid range %d-%d", port, minPort, maxPort)
	}
	return nil
}

// NewPortLongLabel parses a port configuration string and returns a PortConfig struct.
//
// The input string `s` must follow one of these formats:
// 1. "<proxy port>/<proxy protocol>:<target port>/<target protocol>"
//   - Example: "443/https:80/http"
//
// 2. "<proxy port>:<target port>"
//   - Example: "443:80"
//   - Defaults: "https" for `proxy protocol` and "http" for `target protocol`.
//
// 3. "<proxy port>/<proxy protocol>-><target URL>"
//   - Example: "443/https->https://example.com"
//   - This format indicates a redirect, setting `IsRedirect` to true and TargetURL.
//
// Returns:
// - PortConfig: A struct containing parsed proxy and target configurations.
// - error: An error if the input string is invalid.
//
// Examples:
// 1. "443/https:80/http" -> ProxyPort=443, ProxyProtocol="https", TargetPort=80, TargetProtocol="http"
// 2. "443:80" -> ProxyPort=443, ProxyProtocol="https", TargetPort=80, TargetProtocol="http"
// 3. "443/https->https://example.com" -> ProxyPort=443, ProxyProtocol="https", IsRedirect=true, TargetURL=https://example.com

func NewPortLongLabel(s string) (PortConfig, error) {
	config := defaultPortConfig(s)

	separator := detectSeparator(s)

	parts := strings.Split(s, separator)
	if len(parts) != splitPairCount { //nolint:mnd
		return config, ErrInvalidProxyConfig
	}

	err := parseProxySegment(parts[0], &config)
	if err != nil {
		return config, err
	}

	if separator == redirectSeparator {
		config.IsRedirect = true
		err = parseRedirectTarget(parts[1], &config)
	} else {
		err = parseTargetSegment(parts[1], &config)
	}

	return config, err
}

// NewPortShortLabel parses a port configuration string and returns a PortConfig struct.
//
// The input string `s` must follow one of these formats:
// 1. "<proxy port>/<proxy protocol>"
//   - Example: "443/https"
func NewPortShortLabel(s string) (PortConfig, error) {
	config := defaultPortConfig(s)

	err := parseProxySegment(s, &config)
	if err != nil {
		return config, err
	}

	return config, nil
}

func (p *PortConfig) String() string {
	return p.name
}

// defaultPortConfig initializes a PortConfig with default values.
func defaultPortConfig(name string) PortConfig {
	return PortConfig{
		name:          name,
		ProxyProtocol: ProtoHTTPS,
		ProxyPort:     443, //nolint:mnd
		TLSValidate:   DefaultTLSValidate,
		IsRedirect:    false,
		targets:       &targetState{},
	}
}

// detectSeparator determines the separator used in the configuration string and whether it's a redirect.
func detectSeparator(s string) string {
	if strings.Contains(s, redirectSeparator) {
		return redirectSeparator
	}
	return proxySeparator
}

// parseProxySegment parses the proxy segment of the configuration string.
func parseProxySegment(segment string, config *PortConfig) error {
	proxyParts := strings.Split(segment, protocolSeparator)
	if len(proxyParts) > splitPairCount { //nolint:mnd
		return ErrInvalidProxyConfig
	}

	proxyPort, err := strconv.Atoi(proxyParts[0])
	if err != nil {
		return fmt.Errorf("invalid proxy port: %w", err)
	}
	if err := validatePortRange(proxyPort); err != nil {
		return fmt.Errorf("invalid proxy port: %w", err)
	}
	config.ProxyPort = proxyPort

	if len(proxyParts) == splitPairCount { //nolint:mnd
		config.ProxyProtocol = proxyParts[1]
	}

	return nil
}

func defaultTargetProtocol(proxyProtocol string) string {
	switch proxyProtocol {
	case ProtoTCP:
		return ProtoTCP
	case ProtoUDP:
		return ProtoUDP
	default:
		return ProtoHTTP
	}
}

func parseTargetSegment(segment string, config *PortConfig) error {
	targetParts := strings.Split(segment, protocolSeparator)
	if len(targetParts) > splitPairCount { //nolint:mnd
		return ErrInvalidTargetConfig
	}

	targetPort, err := strconv.Atoi(targetParts[0])
	if err != nil {
		return fmt.Errorf("invalid target port: %w", err)
	}
	if err2 := validatePortRange(targetPort); err2 != nil {
		return fmt.Errorf("invalid target port: %w", err2)
	}

	targetProtocol := defaultTargetProtocol(config.ProxyProtocol)

	if len(targetParts) == splitPairCount { //nolint:mnd
		targetProtocol = targetParts[1]
	}

	urlParsed, err := url.Parse(targetProtocol + "://0.0.0.0:" + targetParts[0])
	if err != nil {
		return fmt.Errorf("error to parse url: %w", err)
	}

	config.targets = &targetState{targets: []*url.URL{urlParsed}}

	return nil
}

func parseRedirectTarget(segment string, config *PortConfig) error {
	targetURL, err := url.Parse(segment)
	if err != nil || targetURL.Scheme == "" || targetURL.Host == "" {
		return fmt.Errorf("invalid target URL: %v", segment)
	}

	config.AddTarget(targetURL)

	return nil
}

func (p *PortConfig) GetTargets() []*url.URL {
	if p.targets == nil {
		return nil
	}
	return p.targets.get()
}

func (p *PortConfig) GetFirstTarget() *url.URL {
	if p.targets == nil {
		return &url.URL{}
	}
	return p.targets.getFirst()
}

func (p *PortConfig) AddTarget(target *url.URL) {
	if p.targets == nil {
		p.targets = &targetState{}
	}
	p.targets.add(target)
}

// ReplaceTarget replaces a target URL with a new one.
// used mainly for updating the target URL when the container IP changes like docker provider.
func (p *PortConfig) ReplaceTarget(origin, target *url.URL) {
	if p.targets == nil {
		return
	}
	p.targets.replace(origin, target)
}

func (ts *targetState) get() []*url.URL {
	ts.mtx.RLock()
	defer ts.mtx.RUnlock()
	res := make([]*url.URL, len(ts.targets))
	copy(res, ts.targets)
	return res
}

func (ts *targetState) getFirst() *url.URL {
	ts.mtx.RLock()
	defer ts.mtx.RUnlock()
	if len(ts.targets) == 0 {
		return &url.URL{}
	}
	// Deep copy via round-trip through Parse to avoid sharing pointer fields
	// (e.g. url.User) with the stored target.
	cp, _ := url.Parse(ts.targets[0].String())
	if cp == nil {
		return &url.URL{}
	}
	return cp
}

func (ts *targetState) add(target *url.URL) {
	ts.mtx.Lock()
	defer ts.mtx.Unlock()
	ts.targets = append(ts.targets, target)
}

func (ts *targetState) replace(origin, target *url.URL) {
	ts.mtx.Lock()
	defer ts.mtx.Unlock()
	for k, v := range ts.targets {
		if v.String() == origin.String() {
			ts.targets[k] = target
		}
	}
}

// isPortRange checks whether a port string contains a range expression (e.g., "56000-56100").
func isPortRange(s string) bool {
	parts := strings.SplitN(s, rangeSep, splitPairCount) //nolint:mnd
	if len(parts) != splitPairCount {                    //nolint:mnd
		return false
	}
	_, err1 := strconv.Atoi(parts[0])
	_, err2 := strconv.Atoi(parts[1])
	return err1 == nil && err2 == nil
}

// parsePortRange parses a port range string like "56000-56100" and returns the start and end ports.
func parsePortRange(s string) (start, end int, err error) {
	parts := strings.SplitN(s, rangeSep, splitPairCount) //nolint:mnd
	if len(parts) != splitPairCount {                    //nolint:mnd
		return 0, 0, fmt.Errorf("invalid port range %q: expected format start-end", s)
	}

	start, err = strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, fmt.Errorf("invalid port range start %q: %w", parts[0], err)
	}

	end, err = strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, fmt.Errorf("invalid port range end %q: %w", parts[1], err)
	}

	if err := validatePortRange(start); err != nil {
		return 0, 0, fmt.Errorf("invalid port range start: %w", err)
	}
	if err := validatePortRange(end); err != nil {
		return 0, 0, fmt.Errorf("invalid port range end: %w", err)
	}

	if start > end {
		return 0, 0, fmt.Errorf("invalid port range %q: start %d is greater than end %d", s, start, end)
	}

	count := end - start + 1
	if count > maxRangeLen {
		return 0, 0, fmt.Errorf("port range %q too large: %d ports (max %d)", s, count, maxRangeLen)
	}

	return start, end, nil
}

// IsPortRangeLabel checks whether a port configuration string uses range syntax
// in either the proxy or target segment (or both).
func IsPortRangeLabel(s string) bool {
	separator := detectSeparator(s)
	parts := strings.Split(s, separator)
	if len(parts) != splitPairCount { //nolint:mnd
		return false
	}

	proxyPort := strings.SplitN(parts[0], protocolSeparator, splitPairCount)[0] //nolint:mnd
	if isPortRange(proxyPort) {
		return true
	}

	if separator != redirectSeparator {
		targetPort := strings.SplitN(parts[1], protocolSeparator, splitPairCount)[0]
		return isPortRange(targetPort)
	}

	return false
}

// ExpandPortRangeLabel parses a port range configuration string and returns
// one PortConfig per port in the range. The configuration string format is:
//
//	"<proxy range>/<protocol>:<target range>/<protocol>"
//
// Both proxy and target ranges must have the same number of ports, or one of
// them can be a single port (which is reused for every port in the other range).
// The options string (comma-separated after the config) is applied to all expanded ports.
//
// Examples:
//   - "56000-56100/udp:56000-56100/udp" → 101 individual UDP PortConfigs
//   - "56000-56100/udp:8080/udp"         → 101 UDP PortConfigs, all targeting port 8080
//
// Returns a map key prefix → PortConfig for each expanded port.
func ExpandPortRangeLabel(s string) (map[string]PortConfig, error) {
	separator := detectSeparator(s)
	if separator == redirectSeparator {
		return nil, errors.New("port ranges are not supported with redirect syntax")
	}

	parts := strings.Split(s, separator)
	if len(parts) != splitPairCount { //nolint:mnd
		return nil, ErrInvalidProxyConfig
	}

	proxyParts := strings.SplitN(parts[0], protocolSeparator, splitPairCount)
	proxyProtocol := ProtoHTTPS
	if len(proxyParts) == splitPairCount { //nolint:mnd
		proxyProtocol = proxyParts[1]
	}

	targetParts := strings.SplitN(parts[1], protocolSeparator, splitPairCount)
	targetProtocol := defaultTargetProtocol(proxyProtocol)
	if len(targetParts) == splitPairCount { //nolint:mnd
		targetProtocol = targetParts[1]
	}

	proxyPorts, err := parsePortList(proxyParts[0])
	if err != nil {
		return nil, fmt.Errorf("invalid proxy port: %w", err)
	}

	targetPorts, err := parsePortList(targetParts[0])
	if err != nil {
		return nil, fmt.Errorf("invalid target port: %w", err)
	}

	return expandPortConfigs(proxyPorts, targetPorts, proxyProtocol, targetProtocol)
}

func parsePortList(s string) ([]int, error) {
	if isPortRange(s) {
		start, end, err := parsePortRange(s)
		if err != nil {
			return nil, err
		}
		ports := make([]int, 0, end-start+1)
		for p := start; p <= end; p++ {
			ports = append(ports, p)
		}
		return ports, nil
	}

	port, err := strconv.Atoi(s)
	if err != nil {
		return nil, err
	}
	if err := validatePortRange(port); err != nil {
		return nil, err
	}
	return []int{port}, nil
}

func expandPortConfigs(proxyPorts, targetPorts []int, proxyProtocol, targetProtocol string) (map[string]PortConfig, error) {
	if len(proxyPorts) > 1 && len(targetPorts) > 1 && len(proxyPorts) != len(targetPorts) {
		return nil, fmt.Errorf("proxy range (%d ports) and target range (%d ports) must have the same length",
			len(proxyPorts), len(targetPorts))
	}

	count := len(proxyPorts)
	if len(targetPorts) > count {
		count = len(targetPorts)
	}

	result := make(map[string]PortConfig, count)

	for i := range count {
		proxyPort := proxyPorts[0]
		if len(proxyPorts) > 1 {
			proxyPort = proxyPorts[i]
		}

		targetPort := targetPorts[0]
		if len(targetPorts) > 1 {
			targetPort = targetPorts[i]
		}

		name := fmt.Sprintf("%d/%s:%d/%s", proxyPort, proxyProtocol, targetPort, targetProtocol)

		targetURL, err := url.Parse(targetProtocol + "://0.0.0.0:" + strconv.Itoa(targetPort))
		if err != nil {
			return nil, fmt.Errorf("error parsing target URL: %w", err)
		}

		cfg := PortConfig{
			name:          name,
			ProxyProtocol: proxyProtocol,
			ProxyPort:     proxyPort,
			IsRedirect:    false,
			TLSValidate:   DefaultTLSValidate,
			targets:       &targetState{targets: []*url.URL{targetURL}},
		}

		key := fmt.Sprintf("range_%d", i)
		result[key] = cfg
	}

	return result, nil
}

// ExpandPortRangeShortLabel parses a short label that may contain a port range
// (e.g., "56000-56100/udp") and returns one PortConfig per port.
func ExpandPortRangeShortLabel(s string) (map[string]PortConfig, error) {
	proxyParts := strings.SplitN(s, protocolSeparator, splitPairCount)
	proxyProtocol := ProtoHTTPS
	if len(proxyParts) == splitPairCount { //nolint:mnd
		proxyProtocol = proxyParts[1]
	}

	if !isPortRange(proxyParts[0]) {
		cfg, err := NewPortShortLabel(s)
		if err != nil {
			return nil, err
		}
		return map[string]PortConfig{rangeKeyPrefix: cfg}, nil
	}

	start, end, err := parsePortRange(proxyParts[0])
	if err != nil {
		return nil, fmt.Errorf("invalid port range: %w", err)
	}

	result := make(map[string]PortConfig, end-start+1)

	for idx, p := 0, start; p <= end; idx, p = idx+1, p+1 {
		name := fmt.Sprintf("%d/%s", p, proxyProtocol)
		cfg := PortConfig{
			name:          name,
			ProxyProtocol: proxyProtocol,
			ProxyPort:     p,
			IsRedirect:    false,
			TLSValidate:   DefaultTLSValidate,
			targets:       &targetState{},
		}
		key := fmt.Sprintf("range_%d", idx)
		result[key] = cfg
	}

	return result, nil
}

// IsPortRangeShortLabel checks whether a short label uses range syntax.
func IsPortRangeShortLabel(s string) bool {
	portPart := strings.SplitN(s, protocolSeparator, splitPairCount)[0]
	return isPortRange(portPart)
}
