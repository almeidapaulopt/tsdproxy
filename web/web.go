// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package web

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"

	"github.com/rs/zerolog/log"
	"github.com/vearutop/statigz"
	"github.com/vearutop/statigz/brotli"
)

//go:embed dist
var dist embed.FS

const DefaultIcon = "tsdproxy"

type Assets struct {
	handler   http.Handler
	iconIndex map[string]string
}

// defaultAssets is set by NewAssets and used by the package-level
// convenience functions GuessIcon and IconSVG.
var defaultAssets *Assets

// NewAssets constructs the static file server and icon index from the
// embedded dist/ filesystem. Must be called before GuessIcon or IconSVG.
func NewAssets() *Assets {
	staticFS, err := fs.Sub(dist, "dist")
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to open dist directory")
	}

	a := &Assets{
		handler:   statigz.FileServer(staticFS.(fs.ReadDirFS), brotli.AddEncoding),
		iconIndex: buildIconIndex(),
	}

	defaultAssets = a
	return a
}

func (a *Assets) Handler() http.Handler {
	return a.handler
}

func (a *Assets) GuessIcon(name string) string {
	nameParts := strings.Split(name, "/")
	lastPart := nameParts[len(nameParts)-1]
	baseName := strings.SplitN(lastPart, ":", 2)[0] //nolint:gosec,mnd // slice always has 2 elements from SplitN
	baseName = strings.SplitN(baseName, "@", 2)[0]  //nolint:gosec,mnd // slice always has 2 elements from SplitN

	if icon, ok := a.iconIndex[baseName]; ok {
		return icon
	}
	return DefaultIcon
}

func (a *Assets) IconSVG(name string) string {
	if name == "" {
		name = DefaultIcon
	}

	path := "dist/icons/" + name + ".svg"
	raw, err := dist.ReadFile(path)
	if err != nil {
		return ""
	}

	svg := string(raw)
	svg = strings.Replace(svg, "<svg ", `<svg fill="currentColor" `, 1)

	return svg
}

// GuessIcon delegates to the default Assets. Must be called after NewAssets.
func GuessIcon(name string) string {
	if defaultAssets == nil {
		return DefaultIcon
	}
	return defaultAssets.GuessIcon(name)
}

// IconSVG delegates to the default Assets. Must be called after NewAssets.
func IconSVG(name string) string {
	if defaultAssets == nil {
		return ""
	}
	return defaultAssets.IconSVG(name)
}

func buildIconIndex() map[string]string {
	idx := make(map[string]string)
	for _, dir := range []string{"dist/icons/mdi", "dist/icons/sh", "dist/icons/si"} {
		_ = fs.WalkDir(dist, dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(d.Name(), ".svg") {
				return nil
			}
			name := strings.TrimSuffix(d.Name(), ".svg")
			if strings.HasSuffix(name, "-dark") || strings.HasSuffix(name, "-light") {
				return nil
			}
			if _, exists := idx[name]; !exists {
				rel := strings.TrimPrefix(path, "dist/icons/")
				idx[name] = strings.TrimSuffix(rel, ".svg")
			}
			return nil
		})
	}
	return idx
}
