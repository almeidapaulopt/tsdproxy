// SPDX-FileCopyrightText: 2024 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package config

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/creasty/defaults"
	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
)

const prefix = "TSDPROXY"

type (
	// config stores complete configuration.
	//
	config struct {
		DefaultProxyProvider string

		Docker    map[string]*DockerTargetProviderConfig `validate:"dive"`
		Files     map[string]*FilesTargetProviderConfig  `validate:"dive"`
		Tailscale TailscaleProxyProviderConfig

		HTTP HTTPConfig
		Log  LogConfig

		ProxyAccessLog bool `default:"true" validate:"boolean"`
	}

	// LogConfig stores logging configuration.
	LogConfig struct {
		Level string `default:"info" validate:"required,oneof=debug info warn error fatal panic"`
		JSON  bool   `default:"false" validate:"boolean"`
	}

	// HTTPConfig stores HTTP configuration.
	HTTPConfig struct {
		Hostname string `default:"0.0.0.0" validate:"ip|hostname,required"`
		Port     uint16 `default:"8080" validate:"numeric,min=1,max=65535,required"`
	}

	// DockerTargetProviderConfig struct stores Docker target provider configuration.
	DockerTargetProviderConfig struct {
		Host                 string `default:"unix:///var/run/docker.sock" validate:"required,uri"`
		TargetHostname       string `default:"172.31.0.1" validate:"ip|hostname"`
		DefaultProxyProvider string
	}

	// TailscaleProxyProviderConfig struct stores Tailscale ProxyProvider configuration
	TailscaleProxyProviderConfig struct {
		Providers map[string]*TailscaleServerConfig `validate:"dive"`
		DataDir   string                            `default:"/data/" validate:"dir"`
	}

	// TailscaleServerConfig struct stores Tailscale Server configuration
	TailscaleServerConfig struct {
		AuthKey     string `default:"your-authkey" validate:"omitempty"`
		AuthKeyFile string `validate:"omitempty"`
		ControlURL  string `default:"https://controlplane.tailscale.com" validate:"uri"`
	}

	// filesConfig struct stores File target provider configuration.
	FilesTargetProviderConfig struct {
		Filename              string `validate:"required,file"`
		DefaultProxyProvider  string
		DefaultProxyAccessLog bool `default:"true" validate:"boolean"`
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

	if _, err := NewViper(*file, Config); err != nil {
		return err
	}

	// load default values
	if err := defaults.Set(Config); err != nil {
		fmt.Printf("Error loading defaults: %v", err)
	}

	// generate Providers
	if _, err := os.Stat(*file); os.IsNotExist(err) {
		Config.generateDefaultProviders()
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

	// save default config if config file does not exist
	if _, err := os.Stat(*file); os.IsNotExist(err) {
		if err := Config.save(file); err != nil {
			return err
		}
	}

	return nil
}

func NewViper(file string, i any) (*viper.Viper, error) {
	filename := strings.TrimSuffix(filepath.Base(file), filepath.Ext(file))
	dir, _ := filepath.Split(file)
	filetype := strings.TrimPrefix(filepath.Ext(file), ".")

	println("loading configuration from:", file)

	v := viper.New()
	v.SetConfigName(filename)
	v.SetConfigType(filetype)
	v.AddConfigPath(dir)
	v.SetEnvPrefix(prefix)
	v.AutomaticEnv()

	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	_ = v.ReadInConfig()

	if err := v.Unmarshal(&i); err != nil {
		return nil, err
	}

	// generate the config file if it does not exist
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		if err1 := os.MkdirAll(dir, os.ModeDir); err1 != nil {
			return nil, err1
		}
	}

	return v, nil
}

func (c *config) save(file *string) error {
	yaml, err := yaml.Marshal(Config)
	if err != nil {
		return err
	}
	if err1 := os.WriteFile(*file, yaml, 0644); err1 != nil {
		return err1
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
