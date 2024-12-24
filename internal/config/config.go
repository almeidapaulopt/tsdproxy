// SPDX-FileCopyrightText: 2024 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package config

import (
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"

	"github.com/creasty/defaults"
	"github.com/rs/zerolog/log"
)

type (
	// config stores complete configuration.
	//
	config struct {
		DefaultProxyProvider string `validate:"required" default:"default" yaml:"defaultProxyProvider"`

		Docker    map[string]*DockerTargetProviderConfig `validate:"dive,required" yaml:"docker"`
		Files     map[string]*FilesTargetProviderConfig  `validate:"dive,required" yaml:"files"`
		Tailscale TailscaleProxyProviderConfig           `yaml:"tailscale"`

		HTTP HTTPConfig `yaml:"http"`
		Log  LogConfig  `yaml:"log"`

		ProxyAccessLog bool `validate:"boolean" default:"true" yaml:"proxyAccessLog"`
	}

	// LogConfig stores logging configuration.
	LogConfig struct {
		Level string `validate:"required,oneof=debug info warn error fatal panic trace" default:"info" yaml:"level"`
		JSON  bool   `validate:"boolean" default:"false" yaml:"json"`
	}

	// HTTPConfig stores HTTP configuration.
	HTTPConfig struct {
		Hostname string `validate:"ip|hostname,required" default:"0.0.0.0" yaml:"hostname"`
		Port     uint16 `validate:"numeric,min=1,max=65535,required" default:"8080" yaml:"port"`
	}

	// DockerTargetProviderConfig struct stores Docker target provider configuration.
	DockerTargetProviderConfig struct {
		Host                 string `validate:"required,uri" default:"unix:///var/run/docker.sock" yaml:"host"`
		TargetHostname       string `validate:"ip|hostname" default:"172.31.0.1" yaml:"targetHostname"`
		DefaultProxyProvider string `validate:"omitempty" yaml:"defaultProxyProvider,omitempty"`
	}

	// TailscaleProxyProviderConfig struct stores Tailscale ProxyProvider configuration
	TailscaleProxyProviderConfig struct {
		Providers map[string]*TailscaleServerConfig `validate:"dive,required" yaml:"providers"`
		DataDir   string                            `validate:"dir" default:"/data/" yaml:"dataDir"`
	}

	// TailscaleServerConfig struct stores Tailscale Server configuration
	TailscaleServerConfig struct {
		AuthKey     string `default:"your-authkey" validate:"omitempty" yaml:"authKey"`
		AuthKeyFile string `validate:"omitempty" yaml:"authKeyFile"`
		ControlURL  string `default:"https://controlplane.tailscale.com" validate:"uri" yaml:"controlUrl"`
	}

	// filesConfig struct stores File target provider configuration.
	FilesTargetProviderConfig struct {
		Filename              string `validate:"required,file" yaml:"filename"`
		DefaultProxyProvider  string `yaml:"defaultProxyProvider"`
		DefaultProxyAccessLog bool   `default:"true" validate:"boolean" yaml:"defaultProxyAccessLog"`
	}
)

// Config  is a global variable to store configuration.
var Config *config

// GetConfig loads, validates and returns configuration.
func InitializeConfig() error {
	Config = &config{}
	Config.Tailscale.Providers = make(map[string]*TailscaleServerConfig)
	Config.Docker = make(map[string]*DockerTargetProviderConfig)
	Config.Files = make(map[string]*FilesTargetProviderConfig)

	file := flag.String("config", "/config/tsdproxy.yaml", "loag configuration from file")
	flag.Parse()

	_, err := os.Stat(*file)
	configFileExist := !errors.Is(err, fs.ErrNotExist)

	fileConfig := NewFile(log.Logger, *file, Config)

	// generate Providers
	if !configFileExist {
		println("Generating default configuration to:", *file)
		Config.generateDefaultProviders()
		// load default values
		if err := defaults.Set(Config); err != nil {
			fmt.Printf("Error loading defaults: %v", err)
		}
		if err := fileConfig.Save(); err != nil {
			return err
		}
	}

	println("loading configuration from:", *file)

	if err := fileConfig.Load(); err != nil {
		return err
	}

	// load default values
	if err := defaults.Set(Config); err != nil {
		fmt.Printf("Error loading defaults: %v", err)
	}

	// load auth keys from files
	for _, d := range Config.Tailscale.Providers {
		if d.AuthKeyFile != "" {
			authkey, err := Config.getAuthKeyFromFile(d.AuthKeyFile)
			if err != nil {
				return err
			}
			d.AuthKey = authkey
		}
	}

	// validate config
	if err := Config.validate(); err != nil {
		return err
	}

	return nil
}

func (c *config) getAuthKeyFromFile(authKeyFile string) (string, error) {
	authkey, err := os.ReadFile(authKeyFile)
	if err != nil {
		println("Error reading auth key file:", err)
		return "", err
	}
	return string(authkey), nil
}
