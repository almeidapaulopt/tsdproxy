// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package components

import (
	"github.com/a-h/templ"

	"github.com/almeidapaulopt/tsdproxy/web"
)

func IconURL(name string) string {
	if name == "" {
		name = "tsdproxy"
	}
	return "/icons/" + name + ".svg"
}

func InlineIcon(name, class string) templ.Component {
	return templ.Raw(`<span class="` + class + `">` + web.IconSVG(name) + `</span>`)
}
