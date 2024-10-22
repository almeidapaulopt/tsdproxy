package proxymanager

import (
	"path/filepath"

	"tailscale.com/tsnet"
)

func (pm *ProxyManager) GetTsNetServer(hostname string) *tsnet.Server {
	return &tsnet.Server{
		Hostname:  hostname,
		AuthKey:   pm.config.AuthKey,
		Dir:       filepath.Join(pm.config.DataDir, hostname),
		Ephemeral: true,
	}
}
