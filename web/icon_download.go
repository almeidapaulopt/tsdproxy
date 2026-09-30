// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package web

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

// safeNameRegex validates icon names against path traversal and unsafe chars.
var safeNameRegex = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)

const (
	iconDownloadTimeout = 10 * time.Second
	iconCacheDirPerm    = 0o755
	iconCacheFilePerm   = 0o600
)

// IconDownloader downloads icons from GitHub repos on demand, caching to disk.
type IconDownloader struct {
	manifest   IconManifest
	client     *http.Client
	buildURLFn func(set, iconName string) string
	sf         singleflight.Group
	failed     sync.Map
	cacheDir   string
	defaultSet string
	enabled    bool
}

// NewIconDownloader creates an IconDownloader.
func NewIconDownloader(manifest IconManifest, cacheDir string, enabled bool, defaultSet string) *IconDownloader {
	dl := &IconDownloader{
		manifest:   manifest,
		cacheDir:   cacheDir,
		enabled:    enabled,
		defaultSet: defaultSet,
		client: &http.Client{
			Timeout: iconDownloadTimeout,
		},
	}
	dl.buildURLFn = func(set, iconName string) string {
		cfg := manifest[set]
		return fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/%s/%s.svg", cfg.Repo, cfg.Version, cfg.SvgDir, iconName)
	}
	return dl
}

// resolveIcon splits a set/name string into set and name components.
// Empty name returns ("", "", "").
func (d *IconDownloader) resolveIcon(name string) (set string, iconName string, ext string) {
	if name == "" {
		return "", "", ""
	}
	name = strings.TrimSuffix(name, iconExtSVG)
	ext = iconExtSVG
	if strings.HasSuffix(name, iconExtPNG) {
		ext = iconExtPNG
		name = strings.TrimSuffix(name, iconExtPNG)
	} else if strings.HasSuffix(name, iconExtWebP) {
		ext = iconExtWebP
		name = strings.TrimSuffix(name, iconExtWebP)
	}

	parts := strings.SplitN(name, "/", 2) //nolint:mnd // SplitN count: set/name
	if len(parts) == 2 {                  //nolint:mnd // SplitN result check
		return parts[0], parts[1], ext
	}
	return d.defaultSet, parts[0], ext
}

// validateName checks that the set is in the manifest and the name is safe.
func (d *IconDownloader) validateName(set, iconName string) error {
	if _, ok := d.manifest[set]; !ok {
		return fmt.Errorf("unknown icon set %q", set)
	}
	if !safeNameRegex.MatchString(iconName) {
		return fmt.Errorf("unsafe icon name %q", iconName)
	}
	return nil
}

// safePath returns the full file path for a cached icon and verifies it
// stays within the cache directory (prevents path traversal).
func (d *IconDownloader) safePath(set, iconName, ext string) (string, error) {
	baseDir := filepath.Join(d.cacheDir, set)
	cleanBase := filepath.Clean(baseDir)
	fullPath := filepath.Join(cleanBase, iconName+ext)
	cleanPath := filepath.Clean(fullPath)
	if !strings.HasPrefix(cleanPath, cleanBase+string(filepath.Separator)) && cleanPath != cleanBase {
		return "", fmt.Errorf("path traversal detected: %s", fullPath)
	}
	return cleanPath, nil
}

// Get returns the cached icon bytes if available. It does NOT trigger a download.
func (d *IconDownloader) Get(set, iconName, ext string) ([]byte, error) {
	path, err := d.safePath(set, iconName, ext)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(path)
}

// Cached returns true if the icon exists on disk.
func (d *IconDownloader) Cached(set, iconName, ext string) bool {
	path, err := d.safePath(set, iconName, ext)
	if err != nil {
		return false
	}
	_, err = os.Stat(path)
	return err == nil
}

// Fetch downloads an icon from GitHub, caches it to disk, and returns the bytes.
// It respects the failed map (skips retries for known misses) and uses singleflight
// to deduplicate concurrent requests for the same icon.
func (d *IconDownloader) Fetch(set, iconName, ext string) ([]byte, error) {
	// Check if download is disabled
	if !d.enabled {
		return nil, errors.New("icon download disabled")
	}

	// Validate names
	if err := d.validateName(set, iconName); err != nil {
		return nil, err
	}

	// Check failed map
	key := set + "/" + iconName
	if _, ok := d.failed.Load(key); ok {
		return nil, fmt.Errorf("icon %s previously failed", key)
	}

	// Check cache first
	if data, err := d.Get(set, iconName, ext); err == nil {
		return data, nil
	}

	// Use singleflight to deduplicate
	data, err, _ := d.sf.Do(key, func() (interface{}, error) {
		// Double-check cache after acquiring singleflight lock
		if data, err := d.Get(set, iconName, ext); err == nil {
			return data, nil
		}

		// Download
		url := d.buildURLFn(set, iconName)
		resp, err := d.client.Get(url)
		if err != nil {
			return nil, fmt.Errorf("download icon: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode == http.StatusNotFound {
			d.failed.Store(key, struct{}{})
			return nil, fmt.Errorf("icon %s not found (404)", key)
		}
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("download icon: HTTP %d", resp.StatusCode)
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("read icon response: %w", err)
		}

		// Write atomically
		if err := d.writeAtomically(set, iconName, ext, body); err != nil {
			return nil, err
		}

		return body, nil
	})
	if err != nil {
		return nil, err
	}
	return data.([]byte), nil
}

// writeAtomically writes icon data to the cache directory atomically.
// It routes through safePath to satisfy gosec G703 (path traversal containment).
func (d *IconDownloader) writeAtomically(set, iconName, ext string, data []byte) error {
	path, err := d.safePath(set, iconName, ext)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if mkErr := os.MkdirAll(dir, iconCacheDirPerm); mkErr != nil {
		return fmt.Errorf("create icon cache dir: %w", mkErr)
	}

	// Write to temp file first, then rename
	tmpPath := filepath.Join(dir, "."+iconName+ext+".tmp")
	if wErr := os.WriteFile(tmpPath, data, iconCacheFilePerm); wErr != nil {
		return fmt.Errorf("write icon temp: %w", wErr)
	}
	if rErr := os.Rename(tmpPath, path); rErr != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename icon: %w", rErr)
	}
	return nil
}
