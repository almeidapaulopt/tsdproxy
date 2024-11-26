// SPDX-FileCopyrightText: 2024 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT
package config

import (
	"errors"
	"fmt"
	"strings"

	"github.com/go-playground/validator/v10"
)

// validate method  Validate configurations.
func (c *config) validate() error {
	println("Validating configuration...")
	validate := validator.New()

	if err := validate.Struct(Config); err != nil {
		// validationErrors := err.(validator.ValidationErrors)
		var validationErrors validator.ValidationErrors
		if errors.As(err, &validationErrors) {
			for _, e := range validationErrors {
				fmt.Println(e)
			}
			return err
		}
	}

	// TODO: add validation for each provider
	// TODO: add default proxy provider to each proxy if not defined
	//

	// Set default Proxy Provider if not set.
	//
	if c.DefaultProxyProvider != "" {
		if !c.hasProxyProvider(c.DefaultProxyProvider) {
			return errors.New("Default proxy " + c.DefaultProxyProvider + " provider not found")
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
				return errors.New("Default proxy provider " + p.DefaultProxyProvider + " not found")
			}
		}
	}
	return nil
}

func (c *config) getDefaultProxyProvider() (string, error) {
	for name := range c.Tailscale.Providers {
		return strings.ToLower(name), nil
	}
	return "", errors.New("No default proxy provider")
}

func (c *config) hasProxyProvider(name string) bool {
	for n := range c.Tailscale.Providers {
		if strings.EqualFold(n, name) {
			return true
		}
	}
	return false
}
