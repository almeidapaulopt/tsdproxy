// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package httpclient

import "net/http"

// Doer is an interface for HTTP clients. *http.Client satisfies this interface.
type Doer interface {
	Do(req *http.Request) (*http.Response, error)
}

// compile-time check
var _ Doer = (*http.Client)(nil)
