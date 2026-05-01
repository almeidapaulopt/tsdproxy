// SPDX-FileCopyrightText: 2025 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package config

import (
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"

	"github.com/creasty/defaults"
)

// legacyEnvVars lists env vars handled by generateDefaultProviders that should
// not be processed by the general override system.
var legacyEnvVars = map[string]bool{
	"TSDPROXY_AUTHKEY":     true,
	"TSDPROXY_AUTHKEYFILE": true,
	"TSDPROXY_CONTROLURL":  true,
	"TSDPROXY_DATADIR":     true,
	"TSDPROXY_HOSTNAME":    true,
}

// applyEnvOverrides applies TSDPROXY_* environment variable overrides to the
// config struct. Each env var name is split by "_" (after stripping the
// TSDPROXY_ prefix) and the resulting segments are matched against struct
// fields via their yaml tags. Map fields (e.g. Docker, Providers) consume one
// segment as the map key.
//
// Known limitation: map keys cannot contain underscores.
func applyEnvOverrides(cfg *config) error {
	for _, env := range os.Environ() {
		name, value, ok := strings.Cut(env, "=")
		if !ok || !strings.HasPrefix(name, "TSDPROXY_") {
			continue
		}
		if legacyEnvVars[name] {
			continue
		}

		segments := strings.Split(strings.TrimPrefix(name, "TSDPROXY_"), "_")
		if err := setFieldBySegments(reflect.ValueOf(cfg).Elem(), segments, value); err != nil {
			return fmt.Errorf("env %s: %w", name, err)
		}
	}

	return nil
}

// setFieldBySegments walks the struct tree using path segments and sets the
// leaf value. It uses greedy matching: for each position it tries consuming
// 1, 2, ... segments to match a field's uppercased yaml tag. This handles
// multi-word yaml tags like "defaultProxyProvider" → DEFAULTPROXYPROVIDER.
func setFieldBySegments(v reflect.Value, segments []string, value string) error {
	if len(segments) == 0 {
		return fmt.Errorf("no path segments remaining")
	}

	t := v.Type()

	for n := 1; n <= len(segments); n++ {
		combined := strings.ToUpper(strings.Join(segments[:n], ""))
		remaining := segments[n:]

		fieldIdx := findFieldByYAMLTag(t, combined)
		if fieldIdx < 0 {
			continue
		}

		fieldVal := v.Field(fieldIdx)

		switch {
		case fieldVal.Kind() == reflect.Map && len(remaining) > 0:
			return setMapField(fieldVal, remaining, value)

		case fieldVal.Kind() == reflect.Struct && len(remaining) > 0:
			return setFieldBySegments(fieldVal, remaining, value)

		case isLeafKind(fieldVal.Kind()) && len(remaining) == 0:
			return setLeafValue(fieldVal, value)
		}

		// Match found but not usable with current remaining segments;
		// try consuming more segments.
	}

	return fmt.Errorf("unrecognized config path: %s", strings.Join(segments, "_"))
}

// findFieldByYAMLTag returns the index of the struct field whose uppercased
// yaml tag matches upperName, or -1 if not found.
func findFieldByYAMLTag(t reflect.Type, upperName string) int {
	for i := 0; i < t.NumField(); i++ {
		yamlTag := strings.Split(t.Field(i).Tag.Get("yaml"), ",")[0]
		if yamlTag != "" && yamlTag != "-" && strings.ToUpper(yamlTag) == upperName {
			return i
		}
	}
	return -1
}

// setMapField handles a map[string]*Struct field. The first segment is the map
// key (lowercased); the rest navigate into the struct value.
func setMapField(mapVal reflect.Value, segments []string, value string) error {
	mapKey := strings.ToLower(segments[0])
	remaining := segments[1:]

	if len(remaining) == 0 {
		return fmt.Errorf("map key %q requires a sub-field", mapKey)
	}

	keyVal := reflect.ValueOf(mapKey)
	elemType := mapVal.Type().Elem() // *Struct

	// Look up or create the map entry.
	entry := mapVal.MapIndex(keyVal)
	if !entry.IsValid() || entry.IsNil() {
		newEntry := reflect.New(elemType.Elem())
		if err := defaults.Set(newEntry.Interface()); err != nil {
			return fmt.Errorf("setting defaults for new map entry %q: %w", mapKey, err)
		}
		mapVal.SetMapIndex(keyVal, newEntry)
		entry = mapVal.MapIndex(keyVal)
	}

	return setFieldBySegments(entry.Elem(), remaining, value)
}

// setLeafValue converts the string value and sets it on the reflect.Value.
func setLeafValue(v reflect.Value, s string) error {
	//nolint:exhaustive // only these kinds are used in config structs
	switch v.Kind() {
	case reflect.String:
		v.SetString(s)

	case reflect.Bool:
		b, err := strconv.ParseBool(s)
		if err != nil {
			return fmt.Errorf("invalid bool value %q: %w", s, err)
		}
		v.SetBool(b)

	case reflect.Uint16:
		n, err := strconv.ParseUint(s, 10, 16)
		if err != nil {
			return fmt.Errorf("invalid uint16 value %q: %w", s, err)
		}
		v.SetUint(n)

	default:
		return fmt.Errorf("unsupported field type: %s", v.Kind())
	}

	return nil
}

// isLeafKind returns true for kinds that represent settable leaf values.
func isLeafKind(k reflect.Kind) bool {
	return k != reflect.Struct && k != reflect.Map
}
