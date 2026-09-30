// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package tailscale

import (
	"reflect"
	"strings"

	"github.com/almeidapaulopt/tsdproxy/internal/model"
)

// isNilInterface reports whether v is a nil interface value or a typed nil
// pointer wrapped in an interface.
//
// Go's typed-nil pitfall: assigning `(*T)(nil)` to an interface variable
// produces a non-nil interface (the type slot is populated) that compares
// unequal to nil but panics on method invocation. This helper lets us guard
// interface parameters that callers typically satisfy with pointer types
// (e.g., *local.Client, *tsnet.Server) without requiring every call site to
// explicitly convert nil pointers to a nil interface.
//
// Performance: reflect.ValueOf is invoked at most once per call; this is
// negligible next to the network I/O these functions perform.
func isNilInterface(v any) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	//nolint:exhaustive -- only pointer-like kinds can be nil
	switch rv.Kind() {
	case reflect.Pointer, reflect.Interface, reflect.Slice, reflect.Map, reflect.Chan, reflect.Func:
		return rv.IsNil()
	}
	return false
}

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
