// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package proxmox

import (
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"

	"github.com/creasty/defaults"
	"gopkg.in/yaml.v3"

	"github.com/almeidapaulopt/tsdproxy/internal/model"
)

type (
	// notesConfig is the parsed tsdproxy data of a guest notes/description
	// field. Two YAML styles are accepted and can be mixed:
	//
	// Docker/Incus style (flat settings, port label grammar):
	//
	//	tsdproxy:
	//	  enable: true
	//	  port:
	//	    443: 8443/https:8080/http
	//	  dash.icon: jellyfin
	//
	// list provider style (structured port entries with explicit targets):
	//
	//	tsdproxy:
	//	  ports:
	//	    "443/https":
	//	      targets:
	//	        - "http://10.0.0.5:8080"
	//	      tailscale:
	//	        funnel: true
	//
	// Flat keys are canonical: list-style scalar fields are translated into
	// them and only fill keys the flat form has not set. A guest is enabled
	// when its notes carry any tsdproxy data, unless tsdproxy.enable=false.
	notesConfig struct {
		Settings map[string]string
		Ports    map[string]notesPort
		Enable   bool
	}

	// notesPort is a list-provider style port entry. Empty targets mean the
	// target URL is generated from the guest address.
	notesPort struct {
		Targets     []string            `yaml:"targets,omitempty"`
		Tailscale   model.TailscalePort `validate:"dive" yaml:"tailscale"`
		IsRedirect  bool                `default:"false" validate:"boolean" yaml:"isRedirect,omitempty"`
		TLSValidate bool                `validate:"boolean" default:"true" yaml:"tlsValidate"`
	}
)

// UnmarshalYAML sets defaults before decoding, matching the list provider
// port entries.
func (n *notesPort) UnmarshalYAML(unmarshal func(any) error) error {
	if err := defaults.Set(n); err != nil {
		return fmt.Errorf("error setting defaults: %w", err)
	}

	type plain notesPort
	if err := unmarshal((*plain)(n)); err != nil {
		return err
	}

	return nil
}

// listFields are the list-provider style scalar keys translated into flat
// settings. Pointer fields distinguish "absent" from "zero value".
type listFields struct {
	Hostname        *string        `yaml:"hostname"`
	ProxyProvider   *string        `yaml:"proxyProvider"`
	DNSProvider     *string        `yaml:"dnsProvider"`
	TLSProvider     *string        `yaml:"tlsProvider"`
	IdentityHeaders *bool          `yaml:"identityHeaders"`
	Dashboard       *listDashboard `yaml:"dashboard"`
	Tailscale       *listTailscale `yaml:"tailscale"`
}

type (
	listDashboard struct {
		Label    *string `yaml:"label"`
		Icon     *string `yaml:"icon"`
		Category *string `yaml:"category"`
		Visible  *bool   `yaml:"visible"`
	}

	listTailscale struct {
		Tags         *string `yaml:"tags"`
		Ephemeral    *bool   `yaml:"ephemeral"`
		RunWebClient *bool   `yaml:"runWebClient"`
		Verbose      *bool   `yaml:"verbose"`
		AuthKey      *string `yaml:"authKey"`
	}
)

// listStyleKeys are the block keys owned by the list-provider style. They
// are excluded from generic flattening and consumed by listFields/ports
// instead, so camelCase list keys never leak into the settings map.
var listStyleKeys = map[string]bool{
	"hostname":        true,
	"proxyProvider":   true,
	"dnsProvider":     true,
	"tlsProvider":     true,
	"identityHeaders": true,
	"dashboard":       true,
	"tailscale":       true,
	"ports":           true,
}

// parseTsdproxyConfig extracts the tsdproxy data from a guest
// notes/description field. The returned config is always non-nil: notes
// without tsdproxy data (or with a scalar tsdproxy value) yield a config
// with Enable=false. A present-but-malformed block is an error, so
// misconfiguration is surfaced instead of silently skipped.
func parseTsdproxyConfig(notes string) (*notesConfig, error) {
	doc := parseNotesDocument(notes)
	if doc == nil {
		return &notesConfig{}, nil
	}

	block, hasBlock := doc[notesRootKey].(map[string]any)
	dotted := dottedTsdproxyKeys(doc)
	if !hasBlock && len(dotted) == 0 {
		return &notesConfig{}, nil
	}

	cfg := &notesConfig{Settings: map[string]string{}, Enable: true}

	// Flat form: nested scalars under the tsdproxy block...
	flattenSettings(cfg.Settings, block, ConfigPrefix)

	// ...and top-level keys already carrying the tsdproxy. prefix.
	for key, value := range dotted {
		if nested, ok := value.(map[string]any); ok {
			flattenSettings(cfg.Settings, nested, key+".")
			continue
		}
		assignSetting(cfg.Settings, key, value)
	}

	// List-provider form: scalar fields translated to flat settings keys,
	// filling only keys the flat form has not set.
	if err := mergeListFields(cfg.Settings, block); err != nil {
		return nil, err
	}

	// List-provider form: structured port entries.
	if err := mergeListPorts(cfg, block); err != nil {
		return nil, err
	}

	cfg.Enable = !settingsDisable(cfg.Settings)

	return cfg, nil
}

// dottedTsdproxyKeys collects the top-level document keys carrying the
// tsdproxy. prefix.
func dottedTsdproxyKeys(doc map[string]any) map[string]any {
	dotted := make(map[string]any)
	for key, value := range doc {
		if strings.HasPrefix(key, ConfigPrefix) {
			dotted[key] = value
		}
	}
	return dotted
}

// settingsDisable reports whether the flat settings carry an explicit
// tsdproxy.enable=false. Any other state (absent, true, unparsable) keeps
// the guest enabled: the tsdproxy block itself is the opt-in.
func settingsDisable(settings map[string]string) bool {
	value, ok := settings[ConfigIsEnabled]
	if !ok {
		return false
	}
	enable, err := strconv.ParseBool(value)
	return err == nil && !enable
}

// flattenSettings walks the tsdproxy block producing "<prefix><key>"
// settings entries, where prefix always ends with a dot. List-style
// subtrees and keys owned by the list style are skipped at the block root.
func flattenSettings(settings map[string]string, block map[string]any, prefix string) {
	if block == nil {
		return
	}

	for key, value := range block {
		full := prefix + key
		if prefix == ConfigPrefix && listStyleKeys[key] {
			continue
		}
		if nested, ok := value.(map[string]any); ok {
			flattenSettings(settings, nested, full+".")
			continue
		}
		assignSetting(settings, full, value)
	}
}

// mergeListFields translates list-style scalar fields into flat settings
// keys, filling only keys the flat form has not set.
func mergeListFields(settings map[string]string, block map[string]any) error {
	if block == nil {
		return nil
	}

	raw, err := yaml.Marshal(block)
	if err != nil {
		return fmt.Errorf("error encoding tsdproxy block: %w", err)
	}

	var fields listFields
	if err := yaml.Unmarshal(raw, &fields); err != nil {
		return fmt.Errorf("invalid tsdproxy block: %w", err)
	}

	setString := func(key string, value *string) {
		if value != nil && *value != "" {
			if _, ok := settings[key]; !ok {
				settings[key] = *value
			}
		}
	}
	setBool := func(key string, value *bool) {
		if value != nil {
			if _, ok := settings[key]; !ok {
				settings[key] = strconv.FormatBool(*value)
			}
		}
	}

	setString(ConfigName, fields.Hostname)
	setString(ConfigProxyProvider, fields.ProxyProvider)
	setString(ConfigDNSProvider, fields.DNSProvider)
	setString(ConfigTLSProvider, fields.TLSProvider)
	setBool(ConfigIdentityHeaders, fields.IdentityHeaders)

	if d := fields.Dashboard; d != nil {
		setString(ConfigDashboardLabel, d.Label)
		setString(ConfigDashboardIcon, d.Icon)
		setString(ConfigDashboardCategory, d.Category)
		setBool(ConfigDashboardVisible, d.Visible)
	}

	if t := fields.Tailscale; t != nil {
		setString(ConfigTags, t.Tags)
		setBool(ConfigEphemeral, t.Ephemeral)
		setBool(ConfigRunWebClient, t.RunWebClient)
		setBool(ConfigTsnetVerbose, t.Verbose)
		setString(ConfigAuthKey, t.AuthKey)
	}

	return nil
}

// mergeListPorts decodes the list-style ports map into structured port
// entries. Port labels are validated by the port processing later; this
// step only fails on structurally invalid YAML.
func mergeListPorts(cfg *notesConfig, block map[string]any) error {
	if block == nil {
		return nil
	}

	raw, ok := block["ports"]
	if !ok || raw == nil {
		return nil
	}

	encoded, err := yaml.Marshal(raw)
	if err != nil {
		return fmt.Errorf("error encoding tsdproxy ports: %w", err)
	}

	// Pointers distinguish "label:" (null) from "label: {}": null wipes the
	// defaults a value target would have kept, so it is rebuilt fresh.
	var rawPorts map[string]*notesPort
	if err := yaml.Unmarshal(encoded, &rawPorts); err != nil {
		return fmt.Errorf("invalid tsdproxy ports: %w", err)
	}

	ports := make(map[string]notesPort, len(rawPorts))
	for label, port := range rawPorts {
		if port == nil {
			ports[label] = *newNotesPort()
			continue
		}
		ports[label] = *port
	}

	cfg.Ports = ports

	return nil
}

// newNotesPort returns a port entry with defaults applied.
func newNotesPort() *notesPort {
	port := &notesPort{}
	if err := defaults.Set(port); err != nil {
		return &notesPort{TLSValidate: true}
	}
	return port
}

// parseNotesDocument decodes the notes field into a generic YAML document.
// Proxmox may deliver multi-line notes base64-encoded; when the raw text is
// not valid YAML but its base64 decoding is, the decoded form is used.
func parseNotesDocument(notes string) map[string]any {
	trimmed := strings.TrimSpace(notes)
	if trimmed == "" {
		return nil
	}

	for _, candidate := range []string{trimmed, decodeBase64Notes(trimmed)} {
		if candidate == "" {
			continue
		}
		var doc map[string]any
		if err := yaml.Unmarshal([]byte(candidate), &doc); err == nil && doc != nil {
			normalized := normalizeTree(doc)
			if asMap, ok := normalized.(map[string]any); ok {
				return asMap
			}
		}
	}

	return nil
}

// normalizeTree converts yaml map[any]any nodes into map[string]any with
// stringified keys — yaml.v3 produces them for nested maps with non-string
// keys (e.g. numeric port keys), and every later lookup assumes string
// maps.
func normalizeTree(value any) any {
	switch v := value.(type) {
	case map[string]any:
		for k, val := range v {
			v[k] = normalizeTree(val)
		}
		return v
	case map[any]any:
		out := make(map[string]any, len(v))
		for k, val := range v {
			out[fmt.Sprint(k)] = normalizeTree(val)
		}
		return out
	case []any:
		for i := range v {
			v[i] = normalizeTree(v[i])
		}
		return v
	default:
		return value
	}
}

// decodeBase64Notes decodes whole-string base64 notes, tolerating the
// url-safe alphabet and missing padding. Returns "" when decoding fails.
func decodeBase64Notes(notes string) string {
	notes = strings.TrimPrefix(notes, b64Prefix)

	decoded, err := base64.StdEncoding.DecodeString(padBase64(notes))
	if err != nil {
		decoded, err = base64.URLEncoding.DecodeString(padBase64(notes))
	}
	if err != nil {
		return ""
	}

	return string(decoded)
}

const (
	// b64Prefix marks base64-encoded notes.
	b64Prefix = "b64:"

	// base64BlockSize is the block size base64 padding aligns to.
	base64BlockSize = 4
	// base64Pad2 and base64Pad3 are the remainders needing 2 and 1 "=".
	base64Pad2 = 2
	base64Pad3 = 3
)

func padBase64(s string) string {
	switch len(s) % base64BlockSize {
	case base64Pad2:
		return s + "=="
	case base64Pad3:
		return s + "="
	default:
		return s
	}
}

// assignSetting stores a scalar YAML value as a string. Non-scalar values
// (sequences are never valid tsdproxy settings) are skipped.
func assignSetting(settings map[string]string, key string, value any) {
	switch v := value.(type) {
	case string:
		settings[key] = v
	case bool:
		settings[key] = strconv.FormatBool(v)
	case int:
		settings[key] = strconv.Itoa(v)
	case int64:
		settings[key] = strconv.FormatInt(v, 10)
	case float64:
		settings[key] = strconv.FormatFloat(v, 'f', -1, 64)
	case nil:
		// present-but-null keys keep their "set" semantics with an empty
		// value, matching how Docker labels with empty values behave.
		settings[key] = ""
	}
}
