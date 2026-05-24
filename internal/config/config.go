// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package config

import (
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/creasty/defaults"
	"github.com/rs/zerolog/log"
)

// ValidateKeyFilePath resolves symlinks and verifies the path points to a
// regular file, preventing reads through symlinks, FIFOs, or device files.
func ValidateKeyFilePath(path string) (string, error) {
	cleaned := filepath.Clean(path)

	absPath, err := filepath.Abs(cleaned)
	if err != nil {
		return "", fmt.Errorf("error resolving absolute path: %w", err)
	}

	resolved, err := filepath.EvalSymlinks(absPath)
	if err != nil {
		return "", fmt.Errorf("error resolving symlinks: %w", err)
	}

	info, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("error checking file: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("path %q is not a regular file", resolved)
	}

	return resolved, nil
}

type (
	// config stores complete configuration.
	//
	config struct {
		DNSProviders         map[string]*DNSProviderConfig          `yaml:"dnsProviders"`
		Lists                map[string]*ListTargetProviderConfig   `validate:"dive,required" yaml:"lists"`
		TLSProviders         map[string]*TLSProviderConfig          `yaml:"tlsProviders"`
		Docker               map[string]*DockerTargetProviderConfig `validate:"dive,required" yaml:"docker"`
		Tailscale            TailscaleProxyProviderConfig           `yaml:"tailscale"`
		DefaultProxyProvider string                                 `validate:"required" default:"default" yaml:"defaultProxyProvider"`
		APIKeyFile           string                                 `yaml:"apiKeyFile,omitempty"`
		APIKey               string                                 `yaml:"apiKey,omitempty"`
		DefaultTLSProvider   string                                 `yaml:"defaultTLSProvider"`
		DefaultDNSProvider   string                                 `yaml:"defaultDNSProvider"`
		Admins               []string                               `yaml:"admins,omitempty"`
		HTTP                 HTTPConfig                             `yaml:"http"`
		Log                  LogConfig                              `yaml:"log"`
		Webhooks             []WebhookConfig                        `yaml:"webhooks"`
		Telemetry            TelemetryConfig                        `yaml:"telemetry"`
		ProxyAccessLog       bool                                   `validate:"boolean" default:"true" yaml:"proxyAccessLog"`
		AdminAllowLocalhost  bool                                   `default:"false" validate:"boolean" yaml:"adminAllowLocalhost"`
		CleanupDNS           bool                                   `default:"true" yaml:"cleanupDNS"`
	}

	WebhookConfig struct {
		URL     string            `yaml:"url"`
		Headers map[string]string `yaml:"headers,omitempty"`
		Type    string            `yaml:"type"`
		Events  []string          `yaml:"events,omitempty"`
	}

	// LogConfig stores logging configuration.
	LogConfig struct {
		Level string `validate:"required,oneof=debug info warn error fatal panic trace" default:"info" yaml:"level"`
		JSON  bool   `validate:"boolean" default:"false" yaml:"json"`
	}

	// TelemetryConfig stores OpenTelemetry configuration.
	TelemetryConfig struct {
		Endpoint string `default:"localhost:4317" yaml:"endpoint"`
		Enabled  bool   `default:"false" yaml:"enabled"`
		Insecure bool   `default:"false" yaml:"insecure"`
	}

	// HTTPConfig stores HTTP configuration.
	HTTPConfig struct {
		Hostname string `validate:"ip|hostname,required" default:"127.0.0.1" yaml:"hostname"`
		Port     uint16 `validate:"numeric,min=1,max=65535,required" default:"8080" yaml:"port"`
	}

	// DockerTargetProviderConfig struct stores Docker target provider configuration.
	DockerTargetProviderConfig struct {
		Host                     string `validate:"required,uri" default:"unix:///var/run/docker.sock" yaml:"host"`
		TargetHostname           string `validate:"ip|hostname" default:"172.31.0.1" yaml:"targetHostname"`
		DefaultProxyProvider     string `validate:"omitempty" yaml:"defaultProxyProvider,omitempty"`
		TryDockerInternalNetwork bool   `validate:"boolean" default:"false" yaml:"tryDockerInternalNetwork"`
		AutoRestart              bool   `validate:"boolean" default:"true" yaml:"autoRestart"`
		HealthCheckEnabled       bool   `validate:"boolean" default:"true" yaml:"healthCheckEnabled"`
		HealthCheckInterval      int    `validate:"numeric,min=1" default:"30" yaml:"healthCheckInterval"`
		HealthCheckFailures      int    `validate:"numeric,min=1" default:"3" yaml:"healthCheckFailures"`
		HealthCheckCooldown      int    `validate:"numeric,min=0" default:"0" yaml:"healthCheckCooldown"`
	}

	// TailscaleProxyProviderConfig struct stores Tailscale ProxyProvider configuration
	TailscaleProxyProviderConfig struct {
		Providers map[string]*TailscaleServerConfig `validate:"dive,required" yaml:"providers"`
		DataDir   string                            `validate:"dir" default:"/data/" yaml:"dataDir"`
	}

	// TailscaleServerConfig struct stores Tailscale Server configuration
	TailscaleServerConfig struct {
		AuthKey            string `default:"" validate:"omitempty" yaml:"authKey,omitempty"`
		AuthKeyFile        string `default:"" validate:"omitempty" yaml:"authKeyFile,omitempty"`
		ClientID           string `default:"" validate:"omitempty" yaml:"clientId,omitempty"`
		ClientSecret       string `default:"" validate:"omitempty" yaml:"clientSecret,omitempty"`
		Tags               string `default:"" validate:"omitempty" yaml:"tags,omitempty"`
		ControlURL         string `default:"https://controlplane.tailscale.com" validate:"uri" yaml:"controlUrl"`
		Hostname           string `default:"" validate:"omitempty" yaml:"hostname,omitempty"`
		MaxCertConcurrency int64  `default:"2" validate:"min=1" yaml:"maxCertConcurrency"`
		PreventDuplicates  bool   `default:"false" yaml:"preventDuplicates"`
		Shared             bool   `default:"false" yaml:"shared"`
	}

	// ListTargetProviderConfig struct stores a proxy list target provider configuration.
	ListTargetProviderConfig struct {
		Filename              string `validate:"required,file" yaml:"filename"`
		DefaultProxyProvider  string `validate:"omitempty" yaml:"defaultProxyProvider,omitempty"`
		DefaultProxyAccessLog bool   `default:"true" validate:"boolean" yaml:"defaultProxyAccessLog"`
		AutoRestart          bool   `validate:"boolean" default:"true" yaml:"autoRestart"`
		HealthCheckEnabled   bool   `validate:"boolean" default:"true" yaml:"healthCheckEnabled"`
		HealthCheckInterval  int    `validate:"numeric,min=1" default:"30" yaml:"healthCheckInterval"`
		HealthCheckFailures  int    `validate:"numeric,min=1" default:"3" yaml:"healthCheckFailures"`
		HealthCheckCooldown  int    `validate:"numeric,min=0" default:"0" yaml:"healthCheckCooldown"`
	}

	DNSProviderConfig struct {
		Provider     string `validate:"required,oneof=cloudflare magicdns" yaml:"provider"`
		APIToken     string `yaml:"apiToken,omitempty"`
		APITokenFile string `yaml:"apiTokenFile,omitempty"`
	}

	TLSProviderConfig struct {
		Provider    string `validate:"required,oneof=tailscale acme" yaml:"provider"`
		Email       string `yaml:"email,omitempty"`
		CA          string `default:"https://acme-v02.api.letsencrypt.org/directory" yaml:"ca,omitempty"`
		CertStorage string `yaml:"certStorage,omitempty"`
	}
)

// Config  is a global variable to store configuration.
var Config *config

// GetConfig loads, validates and returns configuration.
func InitializeConfig() error {
	Config = &config{}
	Config.Tailscale.Providers = make(map[string]*TailscaleServerConfig)
	Config.Docker = make(map[string]*DockerTargetProviderConfig)
	Config.Lists = make(map[string]*ListTargetProviderConfig)
	Config.DNSProviders = make(map[string]*DNSProviderConfig)
	Config.TLSProviders = make(map[string]*TLSProviderConfig)

	file := flag.String("config", "/config/tsdproxy.yaml", "loag configuration from file")
	flag.Parse()

	fileConfig := NewConfigFile(log.Logger, *file, Config)

	log.Info().Str("file", *file).Msg("loading configuration")

	if err := Config.loadConfigFile(fileConfig, *file); err != nil {
		return err
	}
	}

	// Load default values.
	// Make sure to set default values after loading from file
	// unless defaults of map type are not loaded.
	if err := defaults.Set(Config); err != nil {
		log.Error().Err(err).Msg("error loading defaults")
	}

	applyDockerHostnameDefault()

	if err := Config.loadSecretsFromFiles(); err != nil {
		return err
	}

	// validate config
	if err := Config.validate(); err != nil {
		return err
	}

	return nil
}

func (c *config) loadConfigFile(fileConfig *File, path string) error {
	if err := fileConfig.Load(); err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		log.Info().Str("file", path).Msg("generating default configuration")

		if err := defaults.Set(c); err != nil {
			log.Error().Err(err).Msg("error loading defaults")
		}

		c.generateDefaultProviders()
		if err := fileConfig.Save(); err != nil {
			return err
		}
	}

	return nil
}

func (c *config) loadSecretsFromFiles() error {
	if err := c.loadTailscaleAuthKeys(); err != nil {
		return err
	}

	if err := c.loadAPIKey(); err != nil {
		return err
	}

	return c.loadDNSProviderTokens()
}

func (c *config) loadTailscaleAuthKeys() error {
	for _, d := range c.Tailscale.Providers {
		if d == nil || (d.ClientSecret != "" && d.ClientID != "") {
			continue
		}

		if d.AuthKeyFile != "" {
			authkey, err := c.getAuthKeyFromFile(d.AuthKeyFile)
			if err != nil {
				return err
			}
			d.AuthKey = authkey
		}
	}

	return nil
}

func (c *config) loadAPIKey() error {
	if c.APIKeyFile == "" {
		return nil
	}

	key, err := c.getAuthKeyFromFile(c.APIKeyFile)
	if err != nil {
		return fmt.Errorf("error reading API key file: %w", err)
	}

	if key == "" {
		return fmt.Errorf("API key file %q is empty", c.APIKeyFile)
	}

	c.APIKey = key

	return nil
}

func (c *config) loadDNSProviderTokens() error {
	for name, d := range c.DNSProviders {
		if d == nil || d.APITokenFile == "" {
			continue
		}
		token, err := c.getAuthKeyFromFile(d.APITokenFile)
		if err != nil {
			return fmt.Errorf("error reading DNS provider %q API token file: %w", name, err)
		}
		d.APIToken = token
	}

	return nil
}

// applyDockerHostnameDefault overrides the HTTP hostname from 127.0.0.1 to
// 0.0.0.0 when running inside a Docker container. Outside Docker the default
// remains 127.0.0.1 per GHSA-j8rq-87gr-gm9q to avoid exposing management
// endpoints on the network. Inside Docker, 127.0.0.1 is unreachable via
// port mapping, so 0.0.0.0 is required.
func applyDockerHostnameDefault() {
	if isRunningInDocker() && Config.HTTP.Hostname == "127.0.0.1" {
		Config.HTTP.Hostname = "0.0.0.0"
		log.Info().Msg("running in Docker: defaulting http.hostname to 0.0.0.0")
	}
}

// isRunningInDocker returns true if the process is running inside a Docker container.
func isRunningInDocker() bool {
	_, err := os.Stat("/.dockerenv")
	return err == nil
}

func (c *config) getAuthKeyFromFile(authKeyFile string) (string, error) {
	resolved, err := ValidateKeyFilePath(authKeyFile)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(resolved) //nolint:gosec // G703: path is validated by ValidateKeyFilePath
	if err != nil {
		return "", fmt.Errorf("error reading auth key file %s: %w", authKeyFile, err)
	}
	return strings.TrimSpace(string(data)), nil
}
