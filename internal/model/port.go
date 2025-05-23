// SPDX-FileCopyrightText: 2025 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package model

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

type (
	PortConfig struct {
		name          string `validate:"string" yaml:"name"`
		ProxyProtocol string `validate:"string" yaml:"proxyProtocol"`
		targets       []*url.URL
		ProxyPort     int           `validate:"hostname_port" yaml:"proxyPort"`
		TLSValidate   bool          `validate:"boolean" yaml:"tlsValidate"`
		IsRedirect    bool          `validate:"boolean" yaml:"isRedirect"`
		Tailscale     TailscalePort `validate:"dive" yaml:"tailscale"`
	}

	TailscalePort struct {
		Funnel bool `validate:"boolean" yaml:"funnel"`
	}
)

const (
	redirectSeparator = "->"
	proxySeparator    = ":"
	protocolSeparator = "/"
)

var (
	ErrInvalidPortFormat   = errors.New("invalid format, missing '" + protocolSeparator + "' or '" + redirectSeparator + "'")
	ErrInvalidProxyConfig  = errors.New("invalid proxy configuration")
	ErrInvalidTargetConfig = errors.New("invalid target configuration")
)

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
	if len(parts) != 2 { //nolint:mnd
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
		ProxyProtocol: "https",
		ProxyPort:     443, //nolint:mnd
		IsRedirect:    false,
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
	if len(proxyParts) > 2 { //nolint:mnd
		return ErrInvalidProxyConfig
	}

	proxyPort, err := strconv.Atoi(proxyParts[0])
	if err != nil {
		return fmt.Errorf("invalid proxy port: %w", err)
	}
	config.ProxyPort = proxyPort

	if len(proxyParts) == 2 { //nolint:mnd
		config.ProxyProtocol = proxyParts[1]
	}

	return nil
}

func parseTargetSegment(segment string, config *PortConfig) error {
	targetParts := strings.Split(segment, protocolSeparator)
	if len(targetParts) > 2 { //nolint:mnd
		return ErrInvalidTargetConfig
	}

	_, err := strconv.Atoi(targetParts[0])
	if err != nil {
		return fmt.Errorf("invalid target port: %w", err)
	}

	targetProtocol := "http"

	if len(targetParts) == 2 { //nolint:mnd
		targetProtocol = targetParts[1]
	}

	urlParsed, err := url.Parse(targetProtocol + "://0.0.0.0:" + targetParts[0])
	if err != nil {
		return fmt.Errorf("error to parse url: %w", err)
	}

	config.targets = []*url.URL{urlParsed}
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
	return p.targets
}

func (p *PortConfig) GetFirstTarget() *url.URL {
	if len(p.GetTargets()) > 0 {
		return p.GetTargets()[0]
	}
	return &url.URL{}
}

func (p *PortConfig) AddTarget(target *url.URL) {
	p.targets = append(p.targets, target)
}

// ReplaceTarget replaces a target URL with a new one.
// used mainly for updating the target URL when the container IP changes like docker provider.
func (p *PortConfig) ReplaceTarget(origin, target *url.URL) {
	for k, v := range p.targets {
		if v.String() == origin.String() {
			p.targets[k] = target
		}
	}
}
