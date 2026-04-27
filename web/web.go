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

//go:generate wget -nc https://github.com/selfhst/icons/archive/refs/heads/main.zip
//go:generate unzip -jo main.zip icons-main/svg/* -d public/icons/sh
//go:generate bun run build

//go:embed dist
var dist embed.FS

var Static http.Handler

const DefaultIcon = "tsdproxy"

func init() {
	staticFS, err := fs.Sub(dist, "dist")
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to open dist directory")
	}

	Static = statigz.FileServer(staticFS.(fs.ReadDirFS), brotli.AddEncoding)
}

func GuessIcon(name string) string {
	nameParts := strings.Split(name, "/")
	lastPart := nameParts[len(nameParts)-1]
	baseName := strings.SplitN(lastPart, ":", 2)[0] //nolint
	baseName = strings.SplitN(baseName, "@", 2)[0]  //nolint

	var foundFile string
	err := fs.WalkDir(dist, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(d.Name(), ".svg") {
			if strings.TrimSuffix(d.Name(), ".svg") == baseName {
				foundFile = path
				return fs.SkipDir
			}
		}
		return nil
	})
	if err != nil || foundFile == "" {
		return DefaultIcon
	}
	icon := strings.TrimPrefix(foundFile, "dist/icons/")
	return strings.TrimSuffix(icon, ".svg")
}
