// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package incus

import (
	"github.com/almeidapaulopt/tsdproxy/internal/targetproviders/settings"
)

// instanceEnabled reports whether the instance config enables tsdproxy.
func instanceEnabled(configMap map[string]string) bool {
	return settings.Bool(configMap, ConfigIsEnabled, false)
}
