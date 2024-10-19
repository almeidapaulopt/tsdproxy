package proxymanager

import (
	"path/filepath"

	"tailscale.com/tsnet"
)

func (pm *ProxyManager) GetTsNetServer(hostname string) *tsnet.Server {
	return &tsnet.Server{
		Hostname:  hostname,
		Dir:       filepath.Join(pm.config.DataDir, hostname),
		Ephemeral: true,
	}
}
