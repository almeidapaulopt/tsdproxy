// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package config

import "github.com/almeidapaulopt/tsdproxy/internal/core/secretstring"

func SetTestConfig(dataDir, authKey string) {
	Config = &config{
		DefaultProxyProvider: TailscaleDefaultProviderName,
	}
	Config.Tailscale = TailscaleProxyProviderConfig{
		DataDir:   dataDir,
		Providers: make(map[string]*TailscaleServerConfig),
	}
	Config.Tailscale.Providers[TailscaleDefaultProviderName] = &TailscaleServerConfig{
		AuthKey:    secretstring.SecretString(authKey),
		ControlURL: "https://controlplane.tailscale.com",
	}
	Config.Docker = make(map[string]*DockerTargetProviderConfig)
	Config.Lists = make(map[string]*ListTargetProviderConfig)
}
