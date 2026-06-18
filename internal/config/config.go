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
	"github.com/rs/zerolog"

	"github.com/almeidapaulopt/tsdproxy/internal/core/secretstring"
)

const defaultControlURL = "https://controlplane.tailscale.com"

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
	// Data stores complete configuration.
	//
	Data struct {
		DNSProviders         map[string]*DNSProviderConfig          `yaml:"dnsProviders"`
		Lists                map[string]*ListTargetProviderConfig   `validate:"dive,required" yaml:"lists"`
		TLSProviders         map[string]*TLSProviderConfig          `yaml:"tlsProviders"`
		Docker               map[string]*DockerTargetProviderConfig `validate:"dive,required" yaml:"docker"`
		Tailscale            TailscaleProxyProviderConfig           `yaml:"tailscale"`
		DefaultProxyProvider string                                 `validate:"required" default:"default" yaml:"defaultProxyProvider"`
		APIKeyFile           string                                 `yaml:"apiKeyFile,omitempty"`
		APIKey               secretstring.SecretString              `yaml:"apiKey,omitempty"`
		DefaultTLSProvider   string                                 `yaml:"defaultTLSProvider"` //nolint:tagliatelle
		DefaultDNSProvider   string                                 `yaml:"defaultDNSProvider"` //nolint:tagliatelle
		Admins               []string                               `yaml:"admins,omitempty"`
		HTTP                 HTTPConfig                             `yaml:"http"`
		Log                  LogConfig                              `yaml:"log"`
		Webhooks             []WebhookConfig                        `yaml:"webhooks"`
		Telemetry            TelemetryConfig                        `yaml:"telemetry"`
		ProxyAccessLog       bool                                   `validate:"boolean" default:"true" yaml:"proxyAccessLog"`
		AdminAllowLocalhost  bool                                   `default:"false" validate:"boolean" yaml:"adminAllowLocalhost"`
		CleanupDNS           bool                                   `default:"true" yaml:"cleanupDNS"` //nolint:tagliatelle
		ShutdownDrainSeconds int                                    `validate:"numeric,min=0,max=300" default:"0" yaml:"shutdownDrainSeconds"`
	}

	WebhookConfig struct {
		URL      string            `yaml:"url"`
		Headers  map[string]string `yaml:"headers,omitempty"`
		Type     string            `yaml:"type"`
		Events   []string          `yaml:"events,omitempty"`
		Template string            `yaml:"template,omitempty"`
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
		SSHPrivateKeyFile        string `validate:"omitempty,file" yaml:"sshPrivateKeyFile,omitempty"`
		TargetHostname           string `validate:"ip|hostname" default:"172.31.0.1" yaml:"targetHostname"`
		DefaultProxyProvider     string `validate:"omitempty" yaml:"defaultProxyProvider,omitempty"`
		SSHAgentSocket           string `validate:"omitempty" yaml:"sshAgentSocket,omitempty"`
		SSHKnownHostsFile        string `validate:"omitempty,file" yaml:"sshKnownHostsFile,omitempty"`
		SSHPrivateKeyPassphrase  string `yaml:"sshPrivateKeyPassphrase,omitempty"`
		Host                     string `validate:"required,uri" default:"unix:///var/run/docker.sock" yaml:"host"`
		HealthCheckInterval      int    `validate:"numeric,min=1,max=86400" default:"30" yaml:"healthCheckInterval"`
		HealthCheckCooldown      int    `validate:"numeric,min=0,max=86400" default:"0" yaml:"healthCheckCooldown"`
		HealthCheckFailures      int    `validate:"numeric,min=1,max=100" default:"3" yaml:"healthCheckFailures"`
		HealthCheckEnabled       bool   `validate:"boolean" default:"true" yaml:"healthCheckEnabled"`
		AutoRestart              bool   `validate:"boolean" default:"true" yaml:"autoRestart"`
		SSHInsecureSkipHostCheck bool   `validate:"boolean" default:"false" yaml:"sshInsecureSkipHostCheck"`
		TryDockerInternalNetwork bool   `validate:"boolean" default:"false" yaml:"tryDockerInternalNetwork"`
		RateLimitEnabled         bool   `validate:"boolean" default:"true" yaml:"rateLimitEnabled"`
		RateLimitRPS             int    `validate:"numeric,min=1" default:"100" yaml:"rateLimitRps"`
		RateLimitBurst           int    `validate:"numeric,min=1" default:"200" yaml:"rateLimitBurst"`
	}

	// TailscaleProxyProviderConfig struct stores Tailscale ProxyProvider configuration
	TailscaleProxyProviderConfig struct {
		Providers map[string]*TailscaleServerConfig `validate:"dive,required" yaml:"providers"`
		DataDir   string                            `validate:"dir" default:"/data/" yaml:"dataDir"`
	}

	// AuthRetryConfig stores the authentication retry policy configuration.
	AuthRetryConfig struct {
		InitialBackoff string `default:"2s" yaml:"initialBackoff"`
		MaxBackoff     string `default:"30s" yaml:"maxBackoff"`
		MaxAttempts    int    `default:"3" validate:"min=1,max=10" yaml:"maxAttempts"`
		Enabled        bool   `default:"true" yaml:"enabled"`
	}

	// TailscaleServerConfig struct stores Tailscale Server configuration
	TailscaleServerConfig struct {
		Hostname            string                    `default:"" validate:"omitempty" yaml:"hostname,omitempty"`
		ClientID            string                    `default:"" validate:"omitempty" yaml:"clientId,omitempty"`
		ReconcileInterval   string                    `default:"0" yaml:"reconcileInterval"`
		ClientSecret        secretstring.SecretString `default:"" validate:"omitempty" yaml:"clientSecret,omitempty"`
		ClientSecretFile    string                    `default:"" validate:"omitempty" yaml:"clientSecretFile,omitempty"`
		Tags                string                    `default:"" validate:"omitempty" yaml:"tags,omitempty"`
		AuthKeyFile         string                    `default:"" validate:"omitempty" yaml:"authKeyFile,omitempty"`
		ControlURL          string                    `default:"https://controlplane.tailscale.com" validate:"uri" yaml:"controlUrl"`
		AuthKey             secretstring.SecretString `default:"" validate:"omitempty" yaml:"authKey,omitempty"`
		AuthRetry           AuthRetryConfig           `yaml:"authRetry"`
		MaxCertConcurrency  int64                     `default:"2" validate:"min=1,max=100" yaml:"maxCertConcurrency"`
		PreventDuplicates   bool                      `default:"false" yaml:"preventDuplicates"`
		Shared              bool                      `default:"false" yaml:"shared"`
		Services            bool                      `default:"false" yaml:"services"`
		AutoApproveDevices  bool                      `default:"false" yaml:"autoApproveDevices"`
		AutoRemoveConflicts bool                      `default:"false" yaml:"autoRemoveConflicts"`
		AutoProvisionACL    bool                      `default:"false" yaml:"autoProvisionAcl"`
	}

	// ListTargetProviderConfig struct stores a proxy list target provider configuration.
	ListTargetProviderConfig struct {
		Filename              string `validate:"required,file" yaml:"filename"`
		DefaultProxyProvider  string `validate:"omitempty" yaml:"defaultProxyProvider,omitempty"`
		HealthCheckInterval   int    `validate:"numeric,min=1,max=86400" default:"30" yaml:"healthCheckInterval"`
		HealthCheckFailures   int    `validate:"numeric,min=1,max=100" default:"3" yaml:"healthCheckFailures"`
		HealthCheckCooldown   int    `validate:"numeric,min=0,max=86400" default:"0" yaml:"healthCheckCooldown"`
		RateLimitRPS          int    `validate:"numeric,min=1" default:"100" yaml:"rateLimitRps"`
		RateLimitBurst        int    `validate:"numeric,min=1" default:"200" yaml:"rateLimitBurst"`
		DefaultProxyAccessLog bool   `default:"true" validate:"boolean" yaml:"defaultProxyAccessLog"`
		AutoRestart           bool   `validate:"boolean" default:"true" yaml:"autoRestart"`
		HealthCheckEnabled    bool   `validate:"boolean" default:"true" yaml:"healthCheckEnabled"`
		RateLimitEnabled      bool   `validate:"boolean" default:"true" yaml:"rateLimitEnabled"`
	}

	DNSProviderConfig struct {
		Provider     string                    `validate:"required,oneof=cloudflare magicdns" yaml:"provider"`
		APIToken     secretstring.SecretString `yaml:"apiToken,omitempty"`
		APITokenFile string                    `yaml:"apiTokenFile,omitempty"`
	}

	TLSProviderConfig struct {
		Provider    string `validate:"required,oneof=tailscale acme" yaml:"provider"`
		Email       string `yaml:"email,omitempty"`
		CA          string `default:"https://acme-v02.api.letsencrypt.org/directory" yaml:"ca,omitempty"`
		CertStorage string `yaml:"certStorage,omitempty"`
	}
)

// InitializeConfig loads, validates and returns configuration.
// Returns (*Data, error) on success so callers can inject the
// config into their constructors. The logger is used for config
// loading diagnostics (the configured logger doesn't exist yet).
func InitializeConfig(log zerolog.Logger) (*Data, error) {
	cfg := &Data{}
	cfg.Tailscale.Providers = make(map[string]*TailscaleServerConfig)
	cfg.Docker = make(map[string]*DockerTargetProviderConfig)
	cfg.Lists = make(map[string]*ListTargetProviderConfig)
	cfg.DNSProviders = make(map[string]*DNSProviderConfig)
	cfg.TLSProviders = make(map[string]*TLSProviderConfig)

	file := flag.String("config", "/config/tsdproxy.yaml", "loag configuration from file")
	flag.Parse()

	fileConfig := NewConfigFile(log, *file, cfg)

	log.Info().Str("file", *file).Msg("loading configuration")

	if err := cfg.loadConfigFile(fileConfig, *file, log); err != nil {
		return nil, err
	}

	// Load default values.
	// Make sure to set default values after loading from file
	// unless defaults of map type are not loaded.
	if err := defaults.Set(cfg); err != nil {
		log.Error().Err(err).Msg("error loading defaults")
	}

	cfg.applyDockerDefaults(log)

	if err := cfg.loadSecretsFromFiles(); err != nil {
		return nil, err
	}

	// Load auth key from env var (TSDPROXY_AUTHKEY) for all providers.
	cfg.loadTailscaleAuthKeyEnvOverrides()

	// Load env var overrides before validation so that TSDPROXY_TAILSCALE_*_CLIENTID
	// and TSDPROXY_TAILSCALE_*_CLIENTSECRET are available to the validator (e.g.
	// services mode requires clientId for the VIP Services API).
	cfg.LoadTailscaleEnvOverrides()

	if err := cfg.validate(log); err != nil {
		return nil, err
	}

	return cfg, nil
}

// applyDockerDefaults adjusts configuration defaults when running inside a
// Docker container.
//
// Outside Docker the defaults remain conservative (127.0.0.1 hostname,
// adminAllowLocalhost false) per GHSA-j8rq-87gr-gm9q. Inside Docker these
// values are impractical: 127.0.0.1 is unreachable via port mapping and
// port-mapped dashboard requests arrive from the Docker bridge gateway
// (private IP) without a Tailscale identity.
func (c *Data) applyDockerDefaults(log zerolog.Logger) {
	if !isRunningInDocker() {
		return
	}
	if c.HTTP.Hostname == "127.0.0.1" {
		c.HTTP.Hostname = "0.0.0.0"
		log.Info().Msg("running in Docker: defaulting http.hostname to 0.0.0.0")
	}
	if !c.AdminAllowLocalhost {
		c.AdminAllowLocalhost = true
		log.Info().Msg("running in Docker: enabling adminAllowLocalhost")
	}
}

// isRunningInDocker returns true if the process is running inside a Docker container.
func isRunningInDocker() bool {
	_, err := os.Stat("/.dockerenv")
	return err == nil
}

func (c *Data) loadConfigFile(fileConfig *File, path string, log zerolog.Logger) error {
	if err := fileConfig.Load(); err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		log.Info().Str("file", path).Msg("generating default configuration")

		if err := defaults.Set(c); err != nil {
			log.Error().Err(err).Msg("error loading defaults")
		}

		c.applyDockerDefaults(log)

		if err := c.generateDefaultProviders(); err != nil {
			return err
		}
		if err := fileConfig.Save(); err != nil {
			return err
		}
	}

	return nil
}

func (c *Data) loadSecretsFromFiles() error {
	if err := c.loadTailscaleAuthKeys(); err != nil {
		return err
	}

	if err := c.loadAPIKey(); err != nil {
		return err
	}

	if err := c.loadTailscaleClientSecrets(); err != nil {
		return err
	}

	if err := c.loadDNSProviderTokens(); err != nil {
		return err
	}

	return nil
}

func (c *Data) loadTailscaleAuthKeys() error {
	for _, d := range c.Tailscale.Providers {
		if d == nil || (d.ClientSecret != "" && d.ClientID != "") {
			continue
		}

		if d.AuthKeyFile != "" {
			authkey, err := c.getAuthKeyFromFile(d.AuthKeyFile)
			if err != nil {
				return err
			}
			d.AuthKey = secretstring.SecretString(authkey)
		}
	}

	return nil
}

func (c *Data) loadAPIKey() error {
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

	c.APIKey = secretstring.SecretString(key)

	return nil
}

func (c *Data) loadDNSProviderTokens() error {
	for name, d := range c.DNSProviders {
		if d == nil || d.APITokenFile == "" {
			continue
		}
		token, err := c.getAuthKeyFromFile(d.APITokenFile)
		if err != nil {
			return fmt.Errorf("error reading DNS provider %q API token file: %w", name, err)
		}
		d.APIToken = secretstring.SecretString(token)
	}

	return nil
}

func (c *Data) loadTailscaleClientSecrets() error {
	for name, d := range c.Tailscale.Providers {
		if d == nil || d.ClientSecretFile == "" {
			continue
		}
		secret, err := c.getAuthKeyFromFile(d.ClientSecretFile)
		if err != nil {
			return fmt.Errorf("error reading tailscale provider %q client secret file: %w", name, err)
		}
		if secret == "" {
			return fmt.Errorf("tailscale provider %q client secret file %s is empty", name, d.ClientSecretFile)
		}
		d.ClientSecret = secretstring.SecretString(secret)
	}

	return nil
}

// loadTailscaleEnvOverrides checks for TSDPROXY_TAILSCALE_<NAME>_CLIENTID and
// TSDPROXY_TAILSCALE_<NAME>_CLIENTSECRET environment variables and overrides
// the corresponding config fields if they are empty. This allows users to
// supply OAuth credentials without hardcoding them in the YAML config file.
//
// Env var names are derived from the Tailscale provider name:
//
//	provider "default"  → TSDPROXY_TAILSCALE_DEFAULT_CLIENTID
//	provider "my-eu-prod" → TSDPROXY_TAILSCALE_MY-EU-PROD_CLIENTSECRET
func (c *Data) LoadTailscaleEnvOverrides() {
	for name, d := range c.Tailscale.Providers {
		if d == nil {
			continue
		}
		prefix := "TSDPROXY_TAILSCALE_" + strings.ToUpper(strings.ReplaceAll(name, "-", "_"))

		if val, ok := os.LookupEnv(prefix + "_CLIENTID"); ok && d.ClientID == "" {
			d.ClientID = val
		}
		if val, ok := os.LookupEnv(prefix + "_CLIENTSECRET"); ok && d.ClientSecret == "" {
			d.ClientSecret = secretstring.SecretString(val)
		}
	}
}

func (c *Data) loadTailscaleAuthKeyEnvOverrides() {
	authKey := os.Getenv("TSDPROXY_AUTHKEY")
	if authKey == "" {
		return
	}
	for _, d := range c.Tailscale.Providers {
		if d != nil && d.AuthKey == "" {
			d.AuthKey = secretstring.SecretString(authKey)
		}
	}
}

func (c *Data) getAuthKeyFromFile(authKeyFile string) (string, error) {
	resolved, err := ValidateKeyFilePath(authKeyFile)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(resolved) //nolint:gosec // G703: path is validated by ValidateKeyFilePath
	if err != nil {
		return "", fmt.Errorf("error reading auth key file %s: %w", authKeyFile, err)
	}
	defer clear(data) // zero secret buffer after use
	return strings.TrimSpace(string(data)), nil
}
