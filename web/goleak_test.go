// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package web

import (
	"testing"

	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m,
		// singleflight.Group spawns an internal goroutine per Do call that
		// lingers briefly after the result is returned. Ignore it.
		goleak.IgnoreAnyFunction("golang.org/x/sync/singleflight.(*Group).Do"),
	)
}
