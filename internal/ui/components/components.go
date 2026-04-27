// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package components

func IconURL(name string) string {
	if name == "" {
		name = "tsdproxy"
	}
	return "/icons/" + name + ".svg"
}
