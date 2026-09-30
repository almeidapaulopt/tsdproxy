// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package web

import (
	"embed"
	"encoding/json"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/rs/zerolog/log"
	"github.com/vearutop/statigz"
	"github.com/vearutop/statigz/brotli"
)

//go:embed dist
var dist embed.FS

//go:embed default_icon.svg
var defaultIconSVG string

//go:embed scripts/icons.json
var iconManifestRaw []byte

const DefaultIcon = "tsdproxy"

// Icon file extension constants.
const (
	iconExtSVG  = ".svg"
	iconExtPNG  = ".png"
	iconExtWebP = ".webp"
)

// IconSet describes a single icon repository in the manifest.
type IconSet struct {
	Repo    string `json:"repo"`
	Version string `json:"version"`
	RefType string `json:"refType"`
	SvgDir  string `json:"svgDir"`
	SHA256  string `json:"sha256"`
}

// IconManifest maps set name to repository config.
type IconManifest map[string]IconSet

// Assets serves static frontend assets and manages icon resolution.
type Assets struct {
	handler    http.Handler
	downloader *IconDownloader
	iconsDir   string
	defaultSet string
}

// defaultAssets is set by NewAssets and used by the package-level
// convenience functions GuessIcon and IconSVG.
var defaultAssets *Assets

// ParseIconManifest parses the embedded icon manifest.
func ParseIconManifest() (IconManifest, error) {
	var m IconManifest
	if err := json.Unmarshal(iconManifestRaw, &m); err != nil {
		return nil, err
	}
	return m, nil
}

// NewAssets constructs the static file server and icon resolver.
//
//   - iconsDir: directory for cached/user-provided icons; empty -> <dataDir>/icons
//   - dataDir:  Tailscale data directory (fallback for iconsDir)
//   - defaultSet: default icon set for auto-detection (e.g. "sh")
//   - download:  enable on-demand download from GitHub
func NewAssets(iconsDir, dataDir, defaultSet string, download bool) *Assets {
	staticFS, err := fs.Sub(dist, "dist")
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to open dist directory")
	}

	if iconsDir == "" {
		iconsDir = filepath.Join(dataDir, "icons")
	}

	manifest, err := ParseIconManifest()
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to parse icon manifest")
	}

	if defaultSet == "" {
		defaultSet = "sh"
	}

	a := &Assets{
		handler:    statigz.FileServer(staticFS.(fs.ReadDirFS), brotli.AddEncoding),
		iconsDir:   iconsDir,
		defaultSet: defaultSet,
		downloader: NewIconDownloader(manifest, iconsDir, download, defaultSet),
	}

	defaultAssets = a
	return a
}

func (a *Assets) Handler() http.Handler {
	return a.handler
}

// iconExtFromPath returns the file extension from a request path, stripping it
// from the path. Returns the extension and the stripped path.
func iconExtFromPath(path string) (ext string, stripped string) {
	if strings.HasSuffix(path, iconExtSVG) {
		return iconExtSVG, strings.TrimSuffix(path, iconExtSVG)
	}
	if strings.HasSuffix(path, iconExtPNG) {
		return iconExtPNG, strings.TrimSuffix(path, iconExtPNG)
	}
	if strings.HasSuffix(path, iconExtWebP) {
		return iconExtWebP, strings.TrimSuffix(path, iconExtWebP)
	}
	return iconExtSVG, path
}

// iconContentType returns the HTTP Content-Type for a given file extension.
func iconContentType(ext string) string {
	switch ext {
	case iconExtPNG:
		return "image/png"
	case iconExtWebP:
		return "image/webp"
	default:
		return "image/svg+xml"
	}
}

// serveIconFromCacheOrDownload tries to resolve an icon through the downloader.
// If rootLevel is true, it first checks the root iconsDir for a bare file
// (no set subdirectory), then falls back to the default set.
// Returns the data, whether it was a cache hit, and whether a response was written.
func (a *Assets) serveIconFromCacheOrDownload(w http.ResponseWriter, set, iconName, ext string, rootLevel bool) (data []byte, cacheHit bool, handled bool) {
	if a.downloader == nil {
		return nil, false, false
	}

	// For root-level icons (no set prefix), try the root icons dir first
	if rootLevel {
		rootPath := filepath.Join(a.iconsDir, iconName+ext)
		if cached, err := os.ReadFile(rootPath); err == nil { //nolint:gosec // path traversal prevented by ".." check above
			return cached, true, false
		}
		set = a.defaultSet
	}

	// Validate the resolved set is in manifest
	if _, ok := a.downloader.manifest[set]; !ok && set != "" {
		http.Error(w, "unknown icon set", http.StatusNotFound)
		return nil, false, true
	}

	// Check cache first
	if cached, err := a.downloader.Get(set, iconName, ext); err == nil {
		return cached, true, false
	}

	// Download on miss for SVG
	if ext == iconExtSVG {
		if fetched, err := a.downloader.Fetch(set, iconName, ext); err == nil {
			return fetched, false, false
		}
	}

	return nil, false, false
}

// IconsHandler returns an HTTP handler mounted at /icons/.
// It serves cached icons, downloads on demand, and falls back to the default icon.
func (a *Assets) IconsHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		requestPath := strings.TrimPrefix(r.URL.Path, "/icons/")
		requestPath = strings.TrimPrefix(requestPath, "/")

		// Serve embedded defaults
		if requestPath == "tsdproxy.svg" || requestPath == "tsdproxy" {
			w.Header().Set("Content-Type", "image/svg+xml")
			w.Header().Set("Cache-Control", "public, max-age=3600")
			_, _ = w.Write([]byte(defaultIconSVG))
			return
		}

		// Reject path traversal
		if strings.Contains(requestPath, "..") {
			http.Error(w, "invalid path", http.StatusBadRequest)
			return
		}

		// Determine extension
		ext, namePath := iconExtFromPath(requestPath)

		// Split set/name
		parts := strings.SplitN(namePath, "/", 2) //nolint:mnd // SplitN count
		var set, iconName string
		var rootLevel bool   // true when no set prefix — try root icons dir first
		if len(parts) == 2 { //nolint:mnd // SplitN result check
			set, iconName = parts[0], parts[1]
		} else {
			rootLevel = true
			iconName = parts[0]
		}

		// Try to get from cache or download
		data, cacheHit, handled := a.serveIconFromCacheOrDownload(w, set, iconName, ext, rootLevel)
		if handled {
			return
		}

		// Serve from cache/download
		if data != nil {
			w.Header().Set("Content-Type", iconContentType(ext))
			if cacheHit {
				w.Header().Set("Cache-Control", "public, max-age=86400")
			}
			_, _ = w.Write(data) //nolint:gosec // static SVG content, no user input
			return
		}

		// Fallback: serve default icon for SVG, 404 for non-SVG
		if ext == iconExtSVG {
			w.Header().Set("Content-Type", "image/svg+xml")
			_, _ = w.Write([]byte(defaultIconSVG))
			return
		}
		http.Error(w, "not found", http.StatusNotFound)
	})
}

// GuessIcon auto-detects an icon name from a Docker image string.
// It extracts the basename and prefixes it with the default set.
func (a *Assets) GuessIcon(name string) string {
	if name == "" {
		return DefaultIcon
	}
	nameParts := strings.Split(name, "/")
	lastPart := nameParts[len(nameParts)-1]
	baseName := strings.SplitN(lastPart, ":", 2)[0] //nolint:mnd // SplitN count: name:tag
	baseName = strings.SplitN(baseName, "@", 2)[0]  //nolint:mnd // SplitN count: name@digest
	if baseName == "" {
		return DefaultIcon
	}
	return a.defaultSet + "/" + baseName
}

// IconSVG returns an inline SVG string with fill="currentColor" for chrome glyphs.
// It checks embedded chrome glyphs first, then reads from the icon cache directory,
// falling back to the embedded default icon. It does NOT trigger downloads;
// it is used synchronously during templ render.
func (a *Assets) IconSVG(name string) string {
	if name == "" {
		name = DefaultIcon
	}

	// Check embedded chrome glyphs first (always available, no disk I/O)
	if svg, ok := getChromeGlyph(name); ok {
		svg = strings.Replace(svg, "<svg ", `<svg fill="currentColor" `, 1)
		return svg
	}

	// Try reading from cache directory
	set, iconName, _ := a.downloader.resolveIcon(name)
	if set != "" {
		if data, err := a.downloader.Get(set, iconName, iconExtSVG); err == nil {
			svg := string(data)
			svg = strings.Replace(svg, "<svg ", `<svg fill="currentColor" `, 1)
			return svg
		}
	}

	// Fallback: return default icon with currentColor
	svg := defaultIconSVG
	svg = strings.Replace(svg, "<svg ", `<svg fill="currentColor" `, 1)
	return svg
}

// Package-level convenience functions that delegate to defaultAssets.

func IconSVG(name string) string {
	if defaultAssets == nil {
		svg := defaultIconSVG
		svg = strings.Replace(svg, "<svg ", `<svg fill="currentColor" `, 1)
		return svg
	}
	return defaultAssets.IconSVG(name)
}

func GuessIcon(name string) string {
	if defaultAssets == nil {
		return DefaultIcon
	}
	return defaultAssets.GuessIcon(name)
}
