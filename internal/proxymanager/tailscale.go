package proxymanager

import (
	"net/http"
	"net/http/httputil"
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

func (pm *ProxyManager) reverseProxyFunc(p *httputil.ReverseProxy) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p.ServeHTTP(w, r)
	}
}
