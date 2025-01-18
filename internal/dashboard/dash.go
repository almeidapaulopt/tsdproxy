// SPDX-FileCopyrightText: 2025 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package dashboard

import (
	"net/http"

	"github.com/almeidapaulopt/tsdproxy/internal/core"
	"github.com/almeidapaulopt/tsdproxy/internal/proxyconfig"
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

// AddRoutes method add dashboard related routes to the http server
func (dash *Dashboard) AddRoutes() {
	dash.HTTP.Get("/r/list", dash.list())
	dash.HTTP.Get("/", web.Static)
}

// index is the HandlerFunc to index page of dashboard
func (dash *Dashboard) list() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		data := make([]pages.ListData, 0)
		sortedNames := make([]string, 0, len(dash.proxies))

		// Collect names for sorting
		for name, p := range dash.proxies {
			if p.Config.Dashboard.Visible {
				sortedNames = append(sortedNames, name)
			}
		}

		// Sort the names
		sort.Strings(sortedNames)

		// Process proxies in sorted order
		for _, name := range sortedNames {
			p := dash.proxies[name]
				state := p.GetState()

				url := p.GetURL()
				if state == proxyconfig.ProxyStateAuthenticating {
					url = p.GetAuthURL()
				}

				icon := p.Config.Dashboard.Icon
				if icon == "" {
					icon = proxyconfig.DefaultDashboardIcon
				}

				label := p.Config.Dashboard.Label
				if label == "" {
					label = name
				}

				enabled := state == proxyconfig.ProxyStateAuthenticating || state == proxyconfig.ProxyStateRunning

				data = append(data, pages.ListData{
					Enabled:    enabled,
					URL:        url,
					ProxyState: state,
					Icon:       icon,
					Label:      label,
				})
			}
		}

		err := ui.Render(w, r, pages.List(data))
		if err != nil {
			dash.Log.Error().Err(err).Msg("Render failed")
	}

import (
	"sort"
)

}
