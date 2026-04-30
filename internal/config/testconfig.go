// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package config

func SetTestConfig(dataDir, authKey string) {
	Config = &config{
		DefaultProxyProvider: "default",
	}
	Config.Tailscale = TailscaleProxyProviderConfig{
		DataDir:   dataDir,
		Providers: make(map[string]*TailscaleServerConfig),
	}
	Config.Tailscale.Providers["default"] = &TailscaleServerConfig{
		AuthKey:    authKey,
		ControlURL: "https://controlplane.tailscale.com",
	}
	Config.Docker = make(map[string]*DockerTargetProviderConfig)
	Config.Lists = make(map[string]*ListTargetProviderConfig)
}
