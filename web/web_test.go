// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package web

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// -- Default Icon -----------------------------------------------------------

func TestDefaultIcon(t *testing.T) {
	if defaultIconSVG == "" {
		t.Fatal("defaultIconSVG should not be empty")
	}
	if !strings.Contains(defaultIconSVG, "<svg") {
		t.Fatal("defaultIconSVG should contain <svg")
	}
}

// -- Icon Manifest ----------------------------------------------------------

func TestIconManifestParsing(t *testing.T) {
	m, err := ParseIconManifest()
	if err != nil {
		t.Fatalf("ParseIconManifest: %v", err)
	}
	for _, set := range []string{"sh", "si", "mdi"} {
		cfg, ok := m[set]
		if !ok {
			t.Fatalf("manifest missing set %q", set)
		}
		if cfg.Repo == "" {
			t.Fatalf("set %q has empty Repo", set)
		}
		if cfg.SvgDir == "" {
			t.Fatalf("set %q has empty SvgDir", set)
		}
	}
}

// -- NewAssets --------------------------------------------------------------

func TestNewAssets(t *testing.T) {
	dataDir := t.TempDir()
	assets := NewAssets("", dataDir, "sh", false)
	if assets == nil {
		t.Fatal("NewAssets returned nil")
	}
	if assets.defaultSet != "sh" {
		t.Fatalf("defaultSet: got %q, want %q", assets.defaultSet, "sh")
	}
	if assets.downloader == nil {
		t.Fatal("downloader should not be nil")
	}
}

func TestNewAssets_WithCustomIconsDir(t *testing.T) {
	iconsDir := t.TempDir()
	assets := NewAssets(iconsDir, "/tmp", "mdi", true)
	if assets.iconsDir != iconsDir {
		t.Fatalf("iconsDir: got %q, want %q", assets.iconsDir, iconsDir)
	}
	if !assets.downloader.enabled {
		t.Fatal("downloader should be enabled")
	}
}

// -- GuessIcon --------------------------------------------------------------

func TestGuessIcon(t *testing.T) {
	dataDir := t.TempDir()
	assets := NewAssets("", dataDir, "sh", false)

	tests := []struct {
		input string
		want  string
	}{
		{"nginx", "sh/nginx"},
		{"nginx:latest", "sh/nginx"},
		{"nginx@sha256:abc", "sh/nginx"},
		{"registry.io/org/plex:latest", "sh/plex"},
		{"library/alpine:3.18", "sh/alpine"},
		{"", DefaultIcon},
	}
	for _, tt := range tests {
		got := assets.GuessIcon(tt.input)
		if got != tt.want {
			t.Errorf("GuessIcon(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestGuessIcon_DefaultSet(t *testing.T) {
	dataDir := t.TempDir()
	assets := NewAssets("", dataDir, "si", false)

	got := assets.GuessIcon("nginx")
	if got != "si/nginx" {
		t.Fatalf("GuessIcon with defaultSet=si: got %q, want %q", got, "si/nginx")
	}
}

// -- IconSVG ----------------------------------------------------------------

func TestIconSVG_CacheHit(t *testing.T) {
	dataDir := t.TempDir()
	assets := NewAssets("", dataDir, "sh", false)

	// Pre-write an SVG to the cache directory
	setDir := filepath.Join(assets.iconsDir, "sh")
	if err := os.MkdirAll(setDir, 0o755); err != nil {
		t.Fatal(err)
	}
	testSVG := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><path d="M0 0h24v24H0z"/></svg>`
	if err := os.WriteFile(filepath.Join(setDir, "nginx.svg"), []byte(testSVG), 0o600); err != nil {
		t.Fatal(err)
	}

	svg := assets.IconSVG("sh/nginx")
	if svg == "" {
		t.Fatal("IconSVG returned empty")
	}
	if !strings.Contains(svg, `fill="currentColor"`) {
		t.Fatal("IconSVG should inject fill=\"currentColor\"")
	}
}

func TestIconSVG_MissReturnsDefault(t *testing.T) {
	dataDir := t.TempDir()
	assets := NewAssets("", dataDir, "sh", false)

	// No file exists - should return default icon SVG
	svg := assets.IconSVG("sh/nonexistent")
	if svg == "" {
		t.Fatal("IconSVG should return default icon on miss")
	}
	if !strings.Contains(svg, "<svg") {
		t.Fatal("default icon SVG should contain <svg")
	}
}

// -- IconsHandler -----------------------------------------------------------

func TestIconsHandler_ServesDefaultLogo(t *testing.T) {
	dataDir := t.TempDir()
	assets := NewAssets("", dataDir, "sh", false)
	handler := assets.IconsHandler()

	req := httptest.NewRequest(http.MethodGet, "/icons/tsdproxy.svg", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d", rec.Code, http.StatusOK)
	}
	ct := rec.Header().Get("Content-Type")
	if ct != "image/svg+xml" {
		t.Fatalf("Content-Type: got %q, want %q", ct, "image/svg+xml")
	}
	if !strings.Contains(rec.Body.String(), "<svg") {
		t.Fatal("body should contain <svg")
	}
}

func TestIconsHandler_ServesFromCache(t *testing.T) {
	dataDir := t.TempDir()
	assets := NewAssets("", dataDir, "sh", false)
	handler := assets.IconsHandler()

	// Pre-write an SVG
	setDir := filepath.Join(dataDir, "icons", "sh")
	if err := os.MkdirAll(setDir, 0o755); err != nil {
		t.Fatal(err)
	}
	testSVG := `<svg xmlns="http://www.w3.org/2000/svg"><circle cx="10" cy="10" r="5"/></svg>`
	if err := os.WriteFile(filepath.Join(setDir, "nginx.svg"), []byte(testSVG), 0o600); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/icons/sh/nginx.svg", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d", rec.Code, http.StatusOK)
	}
	if rec.Body.String() != testSVG {
		t.Fatalf("body: got %q, want %q", rec.Body.String(), testSVG)
	}
}

func TestIconsHandler_ServesPNG(t *testing.T) {
	dataDir := t.TempDir()
	assets := NewAssets("", dataDir, "sh", false)
	handler := assets.IconsHandler()

	// Pre-write a PNG
	setDir := filepath.Join(dataDir, "icons", "sh")
	if err := os.MkdirAll(setDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(setDir, "usememos.png"), []byte("fake-png"), 0o600); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/icons/sh/usememos.png", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d", rec.Code, http.StatusOK)
	}
	ct := rec.Header().Get("Content-Type")
	if ct != "image/png" {
		t.Fatalf("Content-Type: got %q, want %q", ct, "image/png")
	}
	if rec.Body.String() != "fake-png" {
		t.Fatalf("body: got %q, want %q", rec.Body.String(), "fake-png")
	}
}

func TestIconsHandler_MissReturnsDefault(t *testing.T) {
	dataDir := t.TempDir()
	assets := NewAssets("", dataDir, "sh", false)
	handler := assets.IconsHandler()

	// Missing .svg file → default icon
	req := httptest.NewRequest(http.MethodGet, "/icons/sh/nonexistent.svg", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d (should serve default)", rec.Code, http.StatusOK)
	}
	if !strings.Contains(rec.Body.String(), "<svg") {
		t.Fatal("body should contain default SVG")
	}
}

func TestIconsHandler_MissingPNG_404(t *testing.T) {
	dataDir := t.TempDir()
	assets := NewAssets("", dataDir, "sh", false)
	handler := assets.IconsHandler()

	req := httptest.NewRequest(http.MethodGet, "/icons/sh/nonexistent.png", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status: got %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestIconsHandler_Traversal(t *testing.T) {
	dataDir := t.TempDir()
	assets := NewAssets("", dataDir, "sh", false)
	handler := assets.IconsHandler()

	tests := []string{
		"/icons/../../etc/passwd",
		"/icons/..%2f..%2fetc/passwd",
		"/icons/foo/../../bar",
	}
	for _, path := range tests {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusBadRequest && rec.Code != http.StatusNotFound {
				t.Fatalf("status: got %d, want 400 or 404", rec.Code)
			}
		})
	}
}

func TestIconsHandler_MethodNotAllowed(t *testing.T) {
	dataDir := t.TempDir()
	assets := NewAssets("", dataDir, "sh", false)
	handler := assets.IconsHandler()

	req := httptest.NewRequest(http.MethodPost, "/icons/tsdproxy.svg", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status: got %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
}
