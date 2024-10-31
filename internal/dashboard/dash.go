package dashboard

import (
	"net/http"

	"github.com/almeidapaulopt/tsdproxy/internal/core"
	"github.com/almeidapaulopt/tsdproxy/internal/proxymanager"
)

type Dashboard struct {
	Log     *core.Logger
	HTTP    *core.HTTPServer
	Config  *core.Config
	proxies proxymanager.ProxyList
}

func NewDashboard(http *core.HTTPServer, log *core.Logger, cfg *core.Config, pl proxymanager.ProxyList) *Dashboard {
	return &Dashboard{
		Log:     log,
		HTTP:    http,
		Config:  cfg,
		proxies: pl,
	}
}

func (dash *Dashboard) AddRoutes() {
	dash.HTTP.Handle("GET /", dash.index())
}

func (dash *Dashboard) index() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<!DOCTYPE html>
		<html>
			<head>
				<meta charset="utf-8">
			</head>
			<body>`))
		for _, p := range dash.proxies {
			w.Write([]byte(p.URL.String() + "\n"))
		}
		w.Write([]byte(`</body></html>`))
	}
}
