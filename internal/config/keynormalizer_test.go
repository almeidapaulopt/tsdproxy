// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package config

import (
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func silentLogger() zerolog.Logger {
	return zerolog.Nop()
}

func TestBuildKeyLookup_FindsAllYamlTags(t *testing.T) {
	t.Parallel()
	lookup := buildKeyLookup(&Data{})

	want := []string{
		"clientId",
		"clientSecret",
		"clientSecretFile",
		"authKey",
		"authKeyFile",
		"controlUrl",
		"dataDir",
		"providers",
		"defaultProxyProvider",
		"defaultDNSProvider",
		"defaultTLSProvider",
		"apiToken",
		"apiTokenFile",
		"healthCheckInterval",
		"healthCheckFailures",
		"healthCheckCooldown",
		"healthCheckEnabled",
		"hostname",
		"port",
		"level",
		"json",
		"docker",
		"lists",
		"dnsProviders",
		"tlsProviders",
		"tailscale",
		"webhooks",
		"telemetry",
		"preventDuplicates",
		"shared",
		"services",
		"autoApproveDevices",
		"autoRemoveConflicts",
		"reconcileInterval",
		"maxCertConcurrency",
		"authRetry",
	}
	for _, key := range want {
		_, _, ok := lookup.find(key)
		if !ok {
			t.Errorf("expected key %q to be discoverable in lookup, but find() returned false", key)
		}
	}
}

func TestBuildKeyLookup_NilRoot(t *testing.T) {
	lookup := buildKeyLookup(nil)
	require.NotNil(t, lookup)
	_, _, ok := lookup.find("anything")
	assert.False(t, ok)
}

func TestBuildKeyLookup_RespectsDashTag(t *testing.T) {
	type sample struct {
		Visible string `yaml:"visible"`
		Hidden  string `yaml:"-"`
	}
	lookup := buildKeyLookup(&sample{})
	_, _, okVisible := lookup.find("visible")
	assert.True(t, okVisible, "tagged field should be indexed")
	_, _, okVisibleCI := lookup.find("VISIBLE")
	assert.True(t, okVisibleCI, "case-insensitive lookup should match a tagged field")
	_, _, okHidden := lookup.find("hidden")
	assert.False(t, okHidden, "yaml:'-' field must not be indexed")
}

func TestLookup_FindIsCaseAndSeparatorInsensitive(t *testing.T) {
	lookup := buildKeyLookup(&TailscaleServerConfig{})
	cases := []string{
		"clientId",
		"clientid",
		"ClientID",
		"CLIENTID",
		"client_id",
		"client-id",
		"Client_ID",
	}
	for _, in := range cases {
		canonical, _, ok := lookup.find(in)
		if !ok {
			t.Errorf("expected find(%q) to match, got false", in)
			continue
		}
		if canonical != "clientId" {
			t.Errorf("find(%q) returned canonical %q, want %q", in, canonical, "clientId")
		}
	}
}

func TestNormalize_CaseInsensitive(t *testing.T) {
	cases := []struct {
		name string
		yaml string
	}{
		{name: "lowercase", yaml: "tailscale:\n  providers:\n    default:\n      clientid: id-lowercase\n"},
		{name: "PascalCase", yaml: "tailscale:\n  providers:\n    default:\n      ClientID: id-PascalCase\n"},
		{name: "UPPERCASE", yaml: "tailscale:\n  providers:\n    default:\n      CLIENTID: id-UPPERCASE\n"},
		{name: "snake_case", yaml: "tailscale:\n  providers:\n    default:\n      client_id: id-snake_case\n"},
		{name: "kebab-case", yaml: "tailscale:\n  providers:\n    default:\n      client-id: id-kebab-case\n"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &Data{}
			cfg.Tailscale.Providers = make(map[string]*TailscaleServerConfig)
			cfg.Docker = make(map[string]*DockerTargetProviderConfig)
			cfg.Lists = make(map[string]*ListTargetProviderConfig)
			cfg.DNSProviders = make(map[string]*DNSProviderConfig)
			cfg.TLSProviders = make(map[string]*TLSProviderConfig)

			err := unmarshalNormalized([]byte(tc.yaml), cfg)
			require.NoError(t, err, "case variant %q should load successfully", tc.name)

			p, ok := cfg.Tailscale.Providers["default"]
			require.True(t, ok, "provider 'default' should be present")
			require.NotNil(t, p, "provider pointer should not be nil")
			assert.Equal(t, "id-"+tc.name, p.ClientID,
				"normalized key should land its value in the canonical field")
		})
	}
}

func TestNormalize_FuzzySuggestion(t *testing.T) {
	yamlIn := "tailscale:\n  providers:\n    default:\n      clientdi: transpose-typo\n"
	cfg := &Data{}
	err := unmarshalNormalized([]byte(yamlIn), cfg)
	require.Error(t, err)
	msg := err.Error()
	assert.Contains(t, msg, "unknown field")
	assert.Contains(t, msg, "clientdi")
	assert.Contains(t, msg, "did you mean")
	assert.Contains(t, msg, "clientId",
		"error should suggest clientId for transpose 'clientdi'")
}

func TestNormalize_RejectsTrulyUnknown(t *testing.T) {
	yamlIn := "tailscale:\n  providers:\n    default:\n      totallyMadeUp: 42\n"
	cfg := &Data{}
	err := unmarshalNormalized([]byte(yamlIn), cfg)
	require.Error(t, err)
	msg := err.Error()
	assert.Contains(t, msg, "unknown field")
	assert.Contains(t, msg, "totallyMadeUp")
	assert.Contains(t, msg, "line")
}

func TestNormalize_ReportedLineAndColumn(t *testing.T) {
	yamlIn := "tailscale:\n  dataDir: /tmp\n  bogus: 1\n"
	cfg := &Data{}
	err := unmarshalNormalized([]byte(yamlIn), cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "line 3")
}

func TestNormalize_PreservesValidConfig(t *testing.T) {
	yamlIn := strings.Join([]string{
		"tailscale:",
		"  dataDir: /data",
		"  providers:",
		"    default:",
		"      controlUrl: https://controlplane.tailscale.com",
		"      hostname: myhost",
		"      clientId: id-123",
		"      clientSecret: secret-123",
		"http:",
		"  hostname: 127.0.0.1",
		"  port: 8080",
		"log:",
		"  level: info",
	}, "\n")
	cfg := &Data{}
	cfg.Tailscale.Providers = make(map[string]*TailscaleServerConfig)
	cfg.Docker = make(map[string]*DockerTargetProviderConfig)
	cfg.Lists = make(map[string]*ListTargetProviderConfig)
	cfg.DNSProviders = make(map[string]*DNSProviderConfig)
	cfg.TLSProviders = make(map[string]*TLSProviderConfig)

	err := unmarshalNormalized([]byte(yamlIn), cfg)
	require.NoError(t, err)
	assert.Equal(t, "/data", cfg.Tailscale.DataDir)
	require.Contains(t, cfg.Tailscale.Providers, "default")
	p := cfg.Tailscale.Providers["default"]
	require.NotNil(t, p)
	assert.Equal(t, "myhost", p.Hostname)
	assert.Equal(t, "id-123", p.ClientID)
	assert.Equal(t, "secret-123", string(p.ClientSecret))
}

func TestNormalize_EmptyDocument(t *testing.T) {
	t.Parallel()
	err := unmarshalNormalized([]byte(""), &Data{})
	assert.NoError(t, err)

	err = unmarshalNormalized([]byte("\n\n"), &Data{})
	assert.NoError(t, err)

	err = unmarshalNormalized(nil, &Data{})
	assert.NoError(t, err)
}

func TestNormalize_MapAtRootSupportsUnknownInsideScope(t *testing.T) {
	yamlIn := "dnsProviders:\n  cf:\n    provider: cloudflare\n    apiTken: wrong-spelling\n"
	cfg := &Data{}
	cfg.Tailscale.Providers = make(map[string]*TailscaleServerConfig)
	cfg.Docker = make(map[string]*DockerTargetProviderConfig)
	cfg.Lists = make(map[string]*ListTargetProviderConfig)
	cfg.DNSProviders = make(map[string]*DNSProviderConfig)
	cfg.TLSProviders = make(map[string]*TLSProviderConfig)

	err := unmarshalNormalized([]byte(yamlIn), cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "apiTken")
	assert.Contains(t, err.Error(), "apiToken", "suggestion should reference the canonical apiToken")
}

func TestNormalize_DoesNotMutateValidCanonicalKeys(t *testing.T) {
	yamlIn := "http:\n  hostname: 127.0.0.1\n  port: 9000\n"
	cfg := &Data{}
	err := unmarshalNormalized([]byte(yamlIn), cfg)
	require.NoError(t, err)
	assert.Equal(t, "127.0.0.1", cfg.HTTP.Hostname)
	assert.EqualValues(t, 9000, cfg.HTTP.Port)
}

func TestNormalize_NodeKeyRewritesAreVisibleToDecode(t *testing.T) {
	yamlIn := "log:\n  level: debug\n  JSON: true\n"
	cfg := &Data{}
	err := unmarshalNormalized([]byte(yamlIn), cfg)
	require.NoError(t, err)
	assert.Equal(t, "debug", cfg.Log.Level)
	assert.True(t, cfg.Log.JSON, "uppercase JSON key should normalize to 'json' and decode to bool true")
}

func TestNormalizeNodeKeys_NilInputs(t *testing.T) {
	issues := normalizeNodeKeys(nil, &keyLookup{}, silentLogger())
	assert.Nil(t, issues)

	var root yaml.Node
	lookup := buildKeyLookup(&Data{})
	issues = normalizeNodeKeys(&root, lookup, silentLogger())
	assert.Nil(t, issues, "zero-value node (Kind==0) should produce no issues")
}

func TestSuggest_Levenshtein(t *testing.T) {
	valid := []string{"clientId", "clientSecret", "hostname", "dataDir"}

	got := suggest("clientId", valid)
	assert.Contains(t, got, "clientId", "exact match should always be present")

	got = suggest("cleintId", valid)
	assert.Contains(t, got, "clientId", "single transpose should match clientId")

	got = suggest("hostnameX", valid)
	if assert.Len(t, got, 1, "hostnameX should only match hostname") {
		assert.Equal(t, "hostname", got[0])
	}

	got = suggest("supercalifragilistic", valid)
	assert.Empty(t, got, "completely unrelated input should yield no suggestions")
}

func TestSuggest_PrefixInclusion(t *testing.T) {
	valid := []string{"healthCheckInterval", "healthCheckFailures", "hostname"}
	got := suggest("health", valid)
	assert.NotEmpty(t, got, "shared 6-char prefix should be enough even though Levenshtein is high")
	assert.Contains(t, got, "healthCheckInterval")
	assert.Contains(t, got, "healthCheckFailures")
}

func TestSuggest_EmptyValid(t *testing.T) {
	assert.Nil(t, suggest("anything", nil))
	assert.Nil(t, suggest("anything", []string{}))
}

func TestSuggest_LimitedToThree(t *testing.T) {
	valid := []string{
		"clientId",
		"clientIdA",
		"clientIdB",
		"clientIdC",
		"clientIdD",
	}
	got := suggest("clientId", valid)
	assert.LessOrEqual(t, len(got), 3)
}

func TestLevenshtein_Corners(t *testing.T) {
	assert.Equal(t, 0, levenshtein("", ""))
	assert.Equal(t, 5, levenshtein("", "hello"))
	assert.Equal(t, 5, levenshtein("hello", ""))
	assert.Equal(t, 0, levenshtein("same", "same"))
	assert.Equal(t, 1, levenshtein("cat", "cot"))
	assert.Equal(t, 3, levenshtein("kitten", "sitting"))
}

func TestQuoteList(t *testing.T) {
	assert.Equal(t, "", quoteList(nil))
	assert.Equal(t, `"only"`, quoteList([]string{"only"}))
	assert.Equal(t, `"a" or "b"`, quoteList([]string{"a", "b"}))
	assert.Equal(t, `"a", "b" or "c"`, quoteList([]string{"a", "b", "c"}))
}
