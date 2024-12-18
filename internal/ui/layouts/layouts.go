// SPDX-FileCopyrightText: 2024 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

//go:build !prod
// +build !prod

package layouts

const (
	scripts = `<script type="module" src="http://127.0.0.1:5173/scripts.js"></script>`
	styles  = `<script type="module" src="http://127.0.0.1:5173/@vite/client"></script>`
)
