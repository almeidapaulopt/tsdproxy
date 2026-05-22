// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package config

import (
	"errors"
	"fmt"
	"strings"

	"github.com/go-playground/validator/v10"
)

type DefaultProxyProviderNotFoundError struct {
	ProviderName string
}

func (e *DefaultProxyProviderNotFoundError) Error() string {
	return "Default proxy provider " + e.ProviderName + " not found"
}

var ErrNoDefaultProxyProvider = errors.New("no default proxy provider")

type DomainProviderError struct {
	Domain    string
	FieldType string
}

func (e *DomainProviderError) Error() string {
	return fmt.Sprintf("domain %q set but %s provider not specified", e.Domain, e.FieldType)
}

// validate method  Validate configurations.
func (c *config) validate() error {
	println("Validating configuration...")
	validate := validator.New()

	if err := validate.Struct(Config); err != nil {
		var validationErrors validator.ValidationErrors
		if errors.As(err, &validationErrors) {
			for _, e := range validationErrors {
				fmt.Println(e)
			}
			return err
		}
	}

	if err := c.validateProxyProviders(); err != nil {
		return err
	}

	if err := c.validateDNSProviders(); err != nil {
		return err
	}

	if err := c.validateTLSProviders(); err != nil {
		return err
	}

	// Set default Proxy Provider if not set.
	//
	if c.DefaultProxyProvider != "" {
		if !c.hasProxyProvider(c.DefaultProxyProvider) {
			return &DefaultProxyProviderNotFoundError{ProviderName: c.DefaultProxyProvider}
		}
	} else {
		var temp string
		var err error
		if temp, err = c.getDefaultProxyProvider(); err != nil {
			return err
		}
		c.DefaultProxyProvider = strings.ToLower(temp)
	}

	// add default proxy provider to docker providers
	//
	err := c.addDefaultProxyProviderToDockerProviders()
	if err != nil {
		return err
	}
	return nil
}

func (c *config) addDefaultProxyProviderToDockerProviders() error {
	for _, p := range c.Docker {
		if p.DefaultProxyProvider == "" {
			p.DefaultProxyProvider = c.DefaultProxyProvider
		} else {
			if !c.hasProxyProvider(p.DefaultProxyProvider) {
				return &DefaultProxyProviderNotFoundError{ProviderName: p.DefaultProxyProvider}
			}
		}
	}
	return nil
}

func (c *config) getDefaultProxyProvider() (string, error) {
	for name := range c.Tailscale.Providers {
		return strings.ToLower(name), nil
	}
	return "", ErrNoDefaultProxyProvider
}

func (c *config) hasProxyProvider(name string) bool {
	for n := range c.Tailscale.Providers {
		if strings.EqualFold(n, name) {
			return true
		}
	}
	return false
}

// ValidateProxyConfig validates per-proxy domain/DNS/TLS provider requirements.
// Returns error if domain is set but DNS or TLS provider is missing.
func ValidateProxyConfig(domain, dnsProvider, tlsProvider, defaultDNSProvider, defaultTLSProvider string) error {
	if domain == "" {
		return nil
	}
	if dnsProvider == "" && defaultDNSProvider == "" {
		return &DomainProviderError{Domain: domain, FieldType: "DNS"}
	}
	if tlsProvider == "" && defaultTLSProvider == "" {
		return &DomainProviderError{Domain: domain, FieldType: "TLS"}
	}
	return nil
}

func (c *config) validateProxyProviders() error {
	if len(c.Tailscale.Providers) == 0 {
		return errors.New("no tailscale proxy providers configured")
	}
	for name, p := range c.Tailscale.Providers {
		if p == nil {
			return fmt.Errorf("tailscale provider %q has nil configuration", name)
		}
	}
	return nil
}

func (c *config) validateDNSProviders() error {
	if c.DefaultDNSProvider != "" {
		if _, ok := c.DNSProviders[c.DefaultDNSProvider]; !ok {
			return fmt.Errorf("defaultDNSProvider %q not found in dnsProviders", c.DefaultDNSProvider)
		}
	}
	return nil
}

func (c *config) validateTLSProviders() error {
	if c.DefaultTLSProvider != "" {
		if _, ok := c.TLSProviders[c.DefaultTLSProvider]; !ok {
			return fmt.Errorf("defaultTLSProvider %q not found in tlsProviders", c.DefaultTLSProvider)
		}
	}
	return nil
}
