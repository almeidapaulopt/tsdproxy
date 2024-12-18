// SPDX-FileCopyrightText: 2024 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

//go:build prod
// +build prod

package layouts

const (
	scripts = `<script type="module" crossorigin src="/static/scripts.js"></script>`
	styles  = `<link rel="stylesheet" href="/static/styles.css"/>`
)
