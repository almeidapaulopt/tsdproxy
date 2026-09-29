// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package incus

import (
	"os"
	"testing"

	"go.uber.org/goleak"

	"github.com/almeidapaulopt/tsdproxy/web"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

var testAssets = web.NewAssets("", os.TempDir(), "sh", false)
