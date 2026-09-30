// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package web

// Chrome glyph SVGs — tiny icons used for inline chrome UI elements
// (copy, star, info). These are embedded directly so they always render
// correctly, even on a fresh install with no icon cache.

const chromeGlyphContentCopy = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24">` +
	`<path d="M19,21H8V7H19M19,5H8A2,2 0 0,0 6,7V21A2,2 0 0,0 8,23H19A2,2 0 0,0 21,21V7A2,2 0 0,0 19,5M16,1H4A2,2 0 0,0 2,3V17H4V3H16V1Z" />` +
	`</svg>`

const chromeGlyphStar = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24">` +
	`<path d="M12,17.27L18.18,21L16.54,13.97L22,9.24L14.81,8.62L12,2L9.19,8.62L2,9.24L7.45,13.97L5.82,21L12,17.27Z" />` +
	`</svg>`

//nolint:lll // SVG path data — can't be split without breaking coordinates
const chromeGlyphInfoVariant = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><path d="M13.5,4A1.5,1.5 0 0,0 12,5.5A1.5,1.5 0 0,0 13.5,7A1.5,1.5 0 0,0 15,5.5A1.5,1.5 0 0,0 13.5,4M13.14,8.77C11.95,8.87 8.7,11.46 8.7,11.46C8.5,11.61 8.56,11.6 8.72,11.88C8.88,12.15 8.86,12.17 9.05,12.04C9.25,11.91 9.58,11.7 10.13,11.36C12.25,10 10.47,13.14 9.56,18.43C9.2,21.05 11.56,19.7 12.17,19.3C12.77,18.91 14.38,17.8 14.54,17.69C14.76,17.54 14.6,17.42 14.43,17.17C14.31,17 14.19,17.12 14.19,17.12C13.54,17.55 12.35,18.45 12.19,17.88C12,17.31 13.22,13.4 13.89,10.71C14,10.07 14.3,8.67 13.14,8.77Z" /></svg>`

// chromeGlyphMap maps icon names (e.g. "mdi/content-copy") to their inline SVGs.
var chromeGlyphMap = map[string]string{
	"mdi/content-copy":        chromeGlyphContentCopy,
	"mdi/star":                chromeGlyphStar,
	"mdi/information-variant": chromeGlyphInfoVariant,
}

// getChromeGlyph returns the embedded SVG for a known chrome glyph.
// Returns empty string if the name is not a chrome glyph.
func getChromeGlyph(name string) (string, bool) {
	svg, ok := chromeGlyphMap[name]
	return svg, ok
}
