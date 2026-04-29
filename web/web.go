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

var Static http.Handler

const DefaultIcon = "tsdproxy"

var iconIndex map[string]string

func init() {
	staticFS, err := fs.Sub(dist, "dist")
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to open dist directory")
	}

	Static = statigz.FileServer(staticFS.(fs.ReadDirFS), brotli.AddEncoding)

	buildIconIndex()
}

func buildIconIndex() {
	iconIndex = make(map[string]string)
	for _, dir := range []string{"dist/icons/mdi", "dist/icons/sh", "dist/icons/si"} {
		fs.WalkDir(dist, dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(d.Name(), ".svg") {
				return nil
			}
			name := strings.TrimSuffix(d.Name(), ".svg")
			if strings.HasSuffix(name, "-dark") || strings.HasSuffix(name, "-light") {
				return nil
			}
			if _, exists := iconIndex[name]; !exists {
				rel := strings.TrimPrefix(path, "dist/icons/")
				iconIndex[name] = strings.TrimSuffix(rel, ".svg")
			}
			return nil
		})
	}
}

func GuessIcon(name string) string {
	nameParts := strings.Split(name, "/")
	lastPart := nameParts[len(nameParts)-1]
	baseName := strings.SplitN(lastPart, ":", 2)[0] //nolint
	baseName = strings.SplitN(baseName, "@", 2)[0]  //nolint

	if icon, ok := iconIndex[baseName]; ok {
		return icon
	}
	return DefaultIcon
}
