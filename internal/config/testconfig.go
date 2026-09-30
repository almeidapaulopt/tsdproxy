// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package config

import "github.com/almeidapaulopt/tsdproxy/internal/core/secretstring"

// NewTestData creates a minimal *Data for use in tests.
func NewTestData(dataDir, authKey string) *Data {
	cfg := &Data{
		DefaultProxyProvider: TailscaleDefaultProviderName,
		Tailscale: TailscaleProxyProviderConfig{
			DataDir:   dataDir,
			Providers: make(map[string]*TailscaleServerConfig),
		},
		Docker: make(map[string]*DockerTargetProviderConfig),
		Lists:  make(map[string]*ListTargetProviderConfig),
	}
	cfg.Tailscale.Providers[TailscaleDefaultProviderName] = &TailscaleServerConfig{
		AuthKey:    secretstring.SecretString(authKey),
		ControlURL: defaultControlURL,
	}
	return cfg
}
