// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package docker

import (
	"context"
	"maps"
	"strings"
	"testing"

	ctypes "github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/swarm"
	"github.com/rs/zerolog"

	"github.com/almeidapaulopt/tsdproxy/internal/config"
)

func boolPtr(b bool) *bool { return &b }

// giteaPreset is the reference preset from issue #432.
func giteaPreset() *config.PresetConfig {
	return &config.PresetConfig{
		Ports: map[string]*config.PresetPort{
			"443/https:3000/http": {},
		},
		Dashboard: config.PresetDashboard{
			Icon:     "gitea",
			Category: "Development",
		},
	}
}

func testPresets() map[string]*config.PresetConfig {
	return map[string]*config.PresetConfig{"gitea": giteaPreset()}
}

func TestApplyPresetLabels_ExpandsPortsAndDashboard(t *testing.T) {
	t.Parallel()

	labels := map[string]string{
		LabelEnable:   "true",
		LabelTemplate: "gitea",
	}

	got := applyPresetLabels(zerolog.Nop(), labels, testPresets(), "gitea")

	// preset port expanded into a synthetic port label
	if v, ok := got[LabelPort+"443/https:3000/http"]; !ok || v != "443/https:3000/http" {
		t.Errorf("preset port label: got %q (present=%v), want %q", v, ok, "443/https:3000/http")
	}
	// dashboard fields expanded
	if got[LabelDashboardIcon] != "gitea" {
		t.Errorf("dash icon: got %q, want %q", got[LabelDashboardIcon], "gitea")
	}
	if got[LabelDashboardCategory] != "Development" {
		t.Errorf("dash category: got %q, want %q", got[LabelDashboardCategory], "Development")
	}
	// unset dashboard fields must not expand (icon guess fallback stays intact)
	if _, ok := got[LabelDashboardLabel]; ok {
		t.Errorf("dash label should not be expanded when unset in preset")
	}
	if _, ok := got[LabelDashboardVisible]; ok {
		t.Errorf("dash visible should not be expanded when unset in preset")
	}
	// original label map untouched
	if _, ok := labels[LabelPort+"443/https:3000/http"]; ok {
		t.Errorf("input labels map was mutated")
	}
}

func TestApplyPresetLabels_ExpandsPortOptions(t *testing.T) {
	t.Parallel()

	presets := map[string]*config.PresetConfig{
		"web": {
			Ports: map[string]*config.PresetPort{
				"443/https:8443/https": {NoTLSValidate: true, Funnel: true, NoAutoDetect: true},
			},
		},
	}

	got := applyPresetLabels(zerolog.Nop(), map[string]string{}, presets, "web")

	want := "443/https:8443/https,no_tlsvalidate,tailscale_funnel,no_autodetect"
	if v, ok := got[LabelPort+"443/https:8443/https"]; !ok || v != want {
		t.Errorf("port label value: got %q (present=%v), want %q", v, ok, want)
	}
}

func TestApplyPresetLabels_ExplicitDashboardLabelWins(t *testing.T) {
	t.Parallel()

	labels := map[string]string{
		LabelTemplate:       "gitea",
		LabelDashboardIcon:  "custom-icon",
		LabelDashboardLabel: "My Gitea",
	}
	labels[LabelDashboardCategory] = "Ops"

	got := applyPresetLabels(zerolog.Nop(), labels, testPresets(), "gitea")

	if got[LabelDashboardIcon] != "custom-icon" {
		t.Errorf("explicit icon overridden: got %q, want %q", got[LabelDashboardIcon], "custom-icon")
	}
	if got[LabelDashboardLabel] != "My Gitea" {
		t.Errorf("explicit label overridden: got %q, want %q", got[LabelDashboardLabel], "My Gitea")
	}
	if got[LabelDashboardCategory] != "Ops" {
		t.Errorf("explicit category overridden: got %q, want %q", got[LabelDashboardCategory], "Ops")
	}
}

func TestApplyPresetLabels_ExplicitPortSameProxyPortWins(t *testing.T) {
	t.Parallel()

	labels := map[string]string{
		LabelTemplate:     "gitea",
		LabelPort + "web": "443/https:3001/http",
	}

	got := applyPresetLabels(zerolog.Nop(), labels, testPresets(), "gitea")

	if v, ok := got[LabelPort+"web"]; !ok || v != "443/https:3001/http" {
		t.Errorf("explicit port lost: got %q (present=%v)", v, ok)
	}
	if _, ok := got[LabelPort+"443/https:3000/http"]; ok {
		t.Errorf("preset port should be skipped when explicit label uses the same proxy port")
	}
}

func TestApplyPresetLabels_ExplicitPortDefaultProtocolWins(t *testing.T) {
	t.Parallel()

	// "443:3001" omits the proxy protocol which defaults to https — it must
	// still override the preset's "443/https:..." port.
	labels := map[string]string{
		LabelTemplate:     "gitea",
		LabelPort + "web": "443:3001",
	}

	got := applyPresetLabels(zerolog.Nop(), labels, testPresets(), "gitea")

	if _, ok := got[LabelPort+"443/https:3000/http"]; ok {
		t.Errorf("preset port should be skipped when explicit label uses same proxy port with default protocol")
	}
}

func TestApplyPresetLabels_ComposesExtraPorts(t *testing.T) {
	t.Parallel()

	// container adds an SSH port on a different proxy port — both apply.
	labels := map[string]string{
		LabelTemplate:     "gitea",
		LabelPort + "ssh": "22/tcp:22/tcp",
	}

	got := applyPresetLabels(zerolog.Nop(), labels, testPresets(), "gitea")

	if _, ok := got[LabelPort+"443/https:3000/http"]; !ok {
		t.Errorf("preset port should compose with explicit ports on other proxy ports")
	}
	if got[LabelPort+"ssh"] != "22/tcp:22/tcp" {
		t.Errorf("explicit ssh port lost: %q", got[LabelPort+"ssh"])
	}
}

func TestApplyPresetLabels_DashboardVisibleFalse(t *testing.T) {
	t.Parallel()

	presets := map[string]*config.PresetConfig{
		"hidden": {Dashboard: config.PresetDashboard{Visible: boolPtr(false)}},
	}

	got := applyPresetLabels(zerolog.Nop(), map[string]string{}, presets, "hidden")

	if v, ok := got[LabelDashboardVisible]; !ok || v != "false" {
		t.Errorf("dash visible: got %q (present=%v), want \"false\"", v, ok)
	}
}

func TestApplyPresetLabels_UnknownPresetNoOp(t *testing.T) {
	t.Parallel()

	labels := map[string]string{LabelTemplate: "nope"}

	got := applyPresetLabels(zerolog.Nop(), labels, testPresets(), "nope")

	if len(got) != len(labels) {
		t.Errorf("unknown preset must not expand anything: got %v", got)
	}
	if !maps.Equal(got, labels) {
		t.Errorf("unknown preset must return labels unchanged")
	}
}

func TestApplyPresetLabels_NilPresetMapNoOp(t *testing.T) {
	t.Parallel()

	labels := map[string]string{LabelTemplate: "gitea"}

	got := applyPresetLabels(zerolog.Nop(), labels, nil, "gitea")

	if !maps.Equal(got, labels) {
		t.Errorf("nil presets must return labels unchanged")
	}
}

func TestApplyPresetLabels_NilPortEntryUsesDefaults(t *testing.T) {
	t.Parallel()

	presets := map[string]*config.PresetConfig{
		"gitea": {
			Ports:     map[string]*config.PresetPort{"443/https:3000/http": nil},
			Dashboard: config.PresetDashboard{Icon: "gitea"},
		},
	}

	got := applyPresetLabels(zerolog.Nop(), map[string]string{}, presets, "gitea")

	if v, ok := got[LabelPort+"443/https:3000/http"]; !ok || v != "443/https:3000/http" {
		t.Errorf("nil port entry: got %q (present=%v), want bare spec", v, ok)
	}
}

// --- integration through newContainer → newProxyConfig ---

func newPresetTestContainer(t *testing.T, labels map[string]string, presets map[string]*config.PresetConfig) *container {
	t.Helper()

	dcontainer := ctypes.InspectResponse{
		Config: &ctypes.Config{
			Labels: labels,
			Image:  "gitea/gitea:latest",
		},
		HostConfig: &ctypes.HostConfig{
			NetworkMode: ctypes.NetworkMode("bridge"),
		},
	}

	return newContainer(zerolog.Nop(), dcontainer, swarm.Service{}, false,
		withTargetProviderName("local"),
		withDefaultTargetHostname("host.docker.internal"),
		withAssets(testAssets),
		withPresets(presets),
	)
}

func TestNewProxyConfig_PresetExpansion(t *testing.T) {
	t.Parallel()

	c := newPresetTestContainer(t, map[string]string{
		LabelEnable:   "true",
		LabelTemplate: "gitea",
	}, testPresets())
	c.ports = map[string]string{"3000": "32768"}

	pcfg, err := c.newProxyConfig(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(pcfg.Ports) != 1 {
		t.Fatalf("ports: got %d, want 1 (%v)", len(pcfg.Ports), pcfg.Ports)
	}
	port, ok := pcfg.Ports[LabelPort+"443/https:3000/http"]
	if !ok {
		t.Fatalf("preset port missing from proxy config: %v", pcfg.Ports)
	}
	if port.ProxyPort != 443 || port.ProxyProtocol != "https" {
		t.Errorf("preset port proxy side: got %d/%s, want 443/https", port.ProxyPort, port.ProxyProtocol)
	}
	if target := port.GetFirstTargetString(); target != "http://host.docker.internal:32768" {
		t.Errorf("preset port target: got %q, want published port target", target)
	}

	if pcfg.Dashboard.Icon != "gitea" {
		t.Errorf("dashboard icon: got %q, want %q (preset)", pcfg.Dashboard.Icon, "gitea")
	}
	if pcfg.Dashboard.Category != "Development" {
		t.Errorf("dashboard category: got %q, want %q (preset)", pcfg.Dashboard.Category, "Development")
	}
}

func TestNewProxyConfig_PresetExplicitLabelsWin(t *testing.T) {
	t.Parallel()

	c := newPresetTestContainer(t, map[string]string{
		LabelEnable:            "true",
		LabelTemplate:          "gitea",
		LabelDashboardIcon:     "my-icon",
		LabelPort + "other443": "443:3001",
	}, testPresets())
	c.ports = map[string]string{"3001": "32769"}

	pcfg, err := c.newProxyConfig(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if pcfg.Dashboard.Icon != "my-icon" {
		t.Errorf("dashboard icon: got %q, want %q (explicit label)", pcfg.Dashboard.Icon, "my-icon")
	}

	if len(pcfg.Ports) != 1 {
		t.Fatalf("ports: got %d, want 1 — preset 443 must be replaced by explicit 443 (%v)", len(pcfg.Ports), pcfg.Ports)
	}
	port, ok := pcfg.Ports[LabelPort+"other443"]
	if !ok {
		t.Fatalf("explicit port missing: %v", pcfg.Ports)
	}
	if port.GetFirstTargetString() != "http://host.docker.internal:32769" {
		t.Errorf("explicit port target: got %q, want container port 3001", port.GetFirstTargetString())
	}
}

func TestNewProxyConfig_NoTemplateUnchanged(t *testing.T) {
	t.Parallel()

	c := newPresetTestContainer(t, map[string]string{
		LabelEnable: "true",
	}, testPresets())
	c.ports = map[string]string{"3000": "32768"}

	pcfg, err := c.newProxyConfig(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Without the template label the preset must not apply: no preset port
	// key (only the legacy auto-detected port may exist) and no preset icon.
	for k := range pcfg.Ports {
		if strings.Contains(k, "443/https:3000/http") {
			t.Errorf("no template label: preset port leaked into config (%q)", k)
		}
	}
	if pcfg.Dashboard.Icon == "gitea" {
		t.Errorf("no template label: preset icon leaked into config")
	}
}
