// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package model

import "context"

type (
	Whois struct {
		ID            string
		DisplayName   string
		Username      string
		ProfilePicURL string
	}
)

func WhoisFromContext(ctx context.Context) (Whois, bool) {
	who, ok := ctx.Value(ContextKeyWhois).(Whois)

	return who, ok
}

func WhoisNewContext(ctx context.Context, who Whois) context.Context {
	return context.WithValue(ctx, ContextKeyWhois, who)
}
