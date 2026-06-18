// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

// Package grafana provides embedded Grafana dashboard provisioning assets.
// TSDProxy serves these at /-/grafana/* so users can auto-provision Grafana
// via init containers, startup scripts, or the Grafana HTTP API.
package grafana

import "embed"

// Content embeds the dashboard JSON and Prometheus datasource YAML.
// Served by TSDProxy at /-/grafana/dashboard.json and /-/grafana/datasource.yaml.
//
//go:embed dashboard.json datasource.yaml
var Content embed.FS
