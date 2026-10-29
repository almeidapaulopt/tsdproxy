// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package docker

import (
	"maps"
	"strconv"
	"strings"

	"github.com/rs/zerolog"

	"github.com/almeidapaulopt/tsdproxy/internal/config"
	"github.com/almeidapaulopt/tsdproxy/internal/model"
)

// applyPresetLabels expands the preset name into labels and returns the
// merged map. Explicit container labels always win:
//
//   - Dashboard fields only expand into label keys the container did not set.
//   - A preset port is skipped when the container declares an explicit
//     tsdproxy.port.* label listening on the same proxy port and protocol —
//     ports on other proxy ports compose with the preset.
//
// The returned map is a copy; the input labels map is never mutated. Unknown
// preset names return the input map unchanged.
func applyPresetLabels(log zerolog.Logger, labels map[string]string, presets map[string]*config.PresetConfig, name string) map[string]string {
	preset, ok := presets[name]
	if !ok {
		log.Error().Str(LabelTemplate, name).Msg("unknown preset: define it under presets in tsdproxy.yaml")
		return labels
	}

	merged := maps.Clone(labels)

	log.Debug().Str(LabelTemplate, name).Msg("expanding preset")

	explicitProxySides := make(map[string]bool)
	for k, v := range labels {
		if !strings.HasPrefix(k, LabelPort) {
			continue
		}
		if side, ok := normalizeProxySide(v); ok {
			explicitProxySides[side] = true
		}
	}

	for spec, p := range preset.Ports {
		options := &config.PresetPort{}
		if p != nil {
			options = p
		}

		side, ok := normalizeProxySide(spec)
		if ok && explicitProxySides[side] {
			log.Info().Str("presetPort", spec).
				Msg("explicit container port overrides preset port with same proxy port")
			continue
		}

		merged[LabelPort+spec] = withPortOptions(spec, options)
	}

	setLabelIfAbsent(merged, LabelDashboardLabel, preset.Dashboard.Label)
	setLabelIfAbsent(merged, LabelDashboardIcon, preset.Dashboard.Icon)
	setLabelIfAbsent(merged, LabelDashboardCategory, preset.Dashboard.Category)
	if preset.Dashboard.Visible != nil {
		setLabelIfAbsent(merged, LabelDashboardVisible, strconv.FormatBool(*preset.Dashboard.Visible))
	}

	return merged
}

// setLabelIfAbsent sets label key to value only when the container did not
// set it explicitly (explicit labels override preset values).
func setLabelIfAbsent(labels map[string]string, key, value string) {
	if value == "" {
		return
	}
	if _, ok := labels[key]; !ok {
		labels[key] = value
	}
}

// withPortOptions appends the preset port options to the port specification
// as comma-separated port label options.
func withPortOptions(spec string, p *config.PresetPort) string {
	var options []string
	if p.NoTLSValidate {
		options = append(options, model.PortOptionNoTLSValidate)
	}
	if p.Funnel {
		options = append(options, model.PortOptionTailscaleFunnel)
	}
	if p.NoAutoDetect {
		options = append(options, model.PortOptionNoAutoDetect)
	}

	if len(options) == 0 {
		return spec
	}

	return spec + "," + strings.Join(options, ",")
}

// portSpecOptionParts is the maximum number of parts when splitting a port
// specification from its comma-separated options ("spec,opt1,opt2").
const portSpecOptionParts = 2

// normalizeProxySide reduces a port specification to its normalized proxy
// side ("<port>/<protocol>", e.g. "443/https"), used to detect an explicit
// label overriding a preset port. Port label options are stripped, the
// target side (after ":" or "->") is dropped, ranges are kept verbatim, and
// an omitted protocol defaults to https — matching model.NewPortLongLabel.
// ok is false when no proxy side can be determined.
func normalizeProxySide(spec string) (string, bool) {
	spec = strings.TrimSpace(strings.SplitN(spec, ",", portSpecOptionParts)[0])
	if spec == "" {
		return "", false
	}

	if idx := strings.Index(spec, redirectSeparator); idx >= 0 {
		spec = spec[:idx]
	} else if idx := strings.Index(spec, portSeparator); idx >= 0 {
		spec = spec[:idx]
	}

	if spec == "" {
		return "", false
	}

	if !strings.Contains(spec, protocolSeparator) {
		spec += protocolSeparator + model.ProtoHTTPS
	}

	return spec, true
}
