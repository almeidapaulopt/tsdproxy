// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package components

import (
	"strings"

	"github.com/a-h/templ"

	"github.com/almeidapaulopt/tsdproxy/web"
)

// IconURL returns the URL path for an icon.
// If name has an explicit extension (.png, .webp), it preserves it.
// Otherwise it appends .svg. Empty name returns /icons/tsdproxy.svg.
func IconURL(name string) string {
	if name == "" {
		return "/icons/tsdproxy.svg"
	}
	// Preserve explicit extension
	if strings.HasSuffix(name, ".png") || strings.HasSuffix(name, ".svg") || strings.HasSuffix(name, ".webp") {
		return "/icons/" + name
	}
	return "/icons/" + name + ".svg"
}

// IconImg renders an <img> tag for a proxy icon.
// The browser drives the HTTP request, which triggers on-demand download.
func IconImg(name, class string) templ.Component {
	extra := ""
	if strings.HasPrefix(name, "mdi/") || strings.HasPrefix(name, "si/") {
		extra = " dark:invert"
	}
	return templ.Raw(`<img class="` + class + extra + `" src="` + IconURL(name) + `" loading="lazy" alt="">`)
}

// InlineIcon renders an inline SVG for chrome glyphs that need currentColor.
func InlineIcon(name, class string) templ.Component {
	return templ.Raw(`<span class="` + class + `">` + web.IconSVG(name) + `</span>`)
}
