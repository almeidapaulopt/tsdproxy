// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package tailscale

import (
	"strings"

	"github.com/almeidapaulopt/tsdproxy/internal/model"
)

// cleanTags splits a comma-separated tag string and returns trimmed, non-empty tags.
func cleanTags(tags string) []string {
	parts := strings.Split(tags, ",")
	result := make([]string, 0, len(parts))
	for _, t := range parts {
		if t = strings.TrimSpace(t); t != "" {
			result = append(result, t)
		}
	}
	return result
}

// primaryScheme returns the primary URL scheme for the given port configuration.
// It prioritizes HTTPS over other protocols when multiple ports exist.
// Map iteration is non-deterministic, so a fallback order is used for non-HTTPS ports.
func primaryScheme(ports model.PortConfigList) string {
	for _, port := range ports {
		if port.ProxyProtocol == model.ProtoHTTPS {
			return model.ProtoHTTPS
		}
	}
	// Prefer HTTP, then TCP, then whatever comes first.
	for _, pref := range []string{model.ProtoHTTP, model.ProtoTCP} {
		for _, port := range ports {
			if port.ProxyProtocol == pref {
				return pref
			}
		}
	}
	for _, port := range ports {
		return port.ProxyProtocol
	}
	return model.ProtoHTTPS
}
