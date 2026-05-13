// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package dom

import (
	"encoding/json"
	"fmt"
	"strings"
)

// JSString returns s as a safely-escaped JavaScript string literal
// (including surrounding double-quotes), suitable for embedding in
// inline event handlers.  It uses json.Marshal so all characters
// that could break out of a JS string (quotes, backslashes, control
// chars) are properly escaped.
func JSString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func SafeID(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z',
			r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9',
			r == '-':
			b.WriteRune(r)
		default:
			fmt.Fprintf(&b, "_%x_", r)
		}
	}
	return b.String()
}
