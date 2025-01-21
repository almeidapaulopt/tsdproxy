// SPDX-FileCopyrightText: 2025 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package dashboard

import (
	"net/http"

	"github.com/almeidapaulopt/tsdproxy/internal/core"
	"github.com/almeidapaulopt/tsdproxy/internal/models"
	"github.com/almeidapaulopt/tsdproxy/internal/proxymanager"
	"github.com/almeidapaulopt/tsdproxy/internal/ui"
	"github.com/almeidapaulopt/tsdproxy/internal/ui/pages"
	"github.com/almeidapaulopt/tsdproxy/web"

	"github.com/rs/zerolog"
)

type Dashboard struct {
	Log     zerolog.Logger
	HTTP    *core.HTTPServer
	proxies proxymanager.ProxyList
}

func NewDashboard(http *core.HTTPServer, log zerolog.Logger, pl proxymanager.ProxyList) *Dashboard {
	return &Dashboard{
		Log:     log.With().Str("module", "dashboard").Logger(),
		HTTP:    http,
		proxies: pl,
	}
}

// AddRoutes method add dashboard related routes to the http server
func (dash *Dashboard) AddRoutes() {
	dash.HTTP.Get("/r/list", dash.list())
	dash.HTTP.Get("/", web.Static)
}

// index is the HandlerFunc to index page of dashboard
func (dash *Dashboard) list() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		data := make(map[string]pages.ListData)

		for name, p := range dash.proxies {
			if p.Config.Dashboard.Visible {
				status := p.GetStatus()

				url := p.GetURL()
				if status == models.ProxyStatusAuthenticating {
					url = p.GetAuthURL()
				}

				icon := p.Config.Dashboard.Icon
				if icon == "" {
					icon = models.DefaultDashboardIcon
				}

				label := p.Config.Dashboard.Label
				if label == "" {
					label = name
				}

				enabled := status == models.ProxyStatusAuthenticating || status == models.ProxyStatusRunning

				data[name] = pages.ListData{
					Enabled:     enabled,
					URL:         url,
					ProxyStatus: status,
					Icon:        icon,
					Label:       label,
				}
			}
		}

		err := ui.Render(w, r, pages.List(data))
		if err != nil {
			dash.Log.Error().Err(err).Msg("Render failed")
		}
	}
}
