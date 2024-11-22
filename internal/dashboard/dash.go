// SPDX-FileCopyrightText: 2024 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT
package dashboard

import (
	"net/http"

	"github.com/almeidapaulopt/tsdproxy/internal/core"
	"github.com/almeidapaulopt/tsdproxy/internal/proxymanager"
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

func (dash *Dashboard) AddRoutes() {
	dash.HTTP.Handle("GET /", dash.index())
}

func (dash *Dashboard) index() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`<!DOCTYPE html>
		<html>
			<head>
				<meta charset="utf-8">
			</head>
			<body>`))
		for _, p := range dash.proxies {
			_, _ = w.Write([]byte(p.URL.String() + "\n"))
		}
		_, _ = w.Write([]byte(`</body></html>`))
	}
}
