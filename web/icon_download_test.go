// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package web

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
)

func testManifest() IconManifest {
	return IconManifest{
		"sh": {
			Repo:    "selfhst/icons",
			Version: "ecfb6aaa4a0b74ad772cab95ef13754063fa4c55",
			RefType: "commits",
			SvgDir:  "svg",
			SHA256:  "78d87edbb7c597b68aaf6ad85d8f44bfe1920fdaa6039b66199fa1f2d7e99f40",
		},
	}
}

func TestIconDownload_Success(t *testing.T) {
	// Start a local test server that serves an SVG
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/svg+xml")
		_, _ = w.Write([]byte(`<svg xmlns="http://www.w3.org/2000/svg"><circle cx="10" cy="10" r="5"/></svg>`))
	}))
	defer server.Close()

	cacheDir := t.TempDir()
	dl := NewIconDownloader(testManifest(), cacheDir, true, "sh")

	// Override the URL builder to use our test server
	dl.buildURLFn = func(set, iconName string) string {
		return server.URL + "/" + iconName + ".svg"
	}

	data, err := dl.Fetch("sh", "nginx", ".svg")
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("Fetch returned empty data")
	}

	// Verify file was written to cache
	cached, err := dl.Get("sh", "nginx", ".svg")
	if err != nil {
		t.Fatalf("Get after Fetch: %v", err)
	}
	if string(cached) != string(data) {
		t.Fatal("cached data differs from fetched data")
	}
}

func TestIconDownload_404(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	cacheDir := t.TempDir()
	dl := NewIconDownloader(testManifest(), cacheDir, true, "sh")
	dl.buildURLFn = func(set, iconName string) string {
		return server.URL + "/" + iconName + ".svg"
	}

	// First call should fail
	_, err := dl.Fetch("sh", "nonexistent", ".svg")
	if err == nil {
		t.Fatal("expected error for 404")
	}

	// Second call should not make HTTP request (recorded in failed map)
	_, err = dl.Fetch("sh", "nonexistent", ".svg")
	if err == nil {
		t.Fatal("expected error for cached 404")
	}
}

func TestIconDownload_Concurrency(t *testing.T) {
	var requestCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
		w.Header().Set("Content-Type", "image/svg+xml")
		_, _ = w.Write([]byte(`<svg xmlns="http://www.w3.org/2000/svg"><circle cx="10" cy="10" r="5"/></svg>`))
	}))
	defer server.Close()

	cacheDir := t.TempDir()
	dl := NewIconDownloader(testManifest(), cacheDir, true, "sh")
	dl.buildURLFn = func(set, iconName string) string {
		return server.URL + "/" + iconName + ".svg"
	}

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := dl.Fetch("sh", "concurrent-test", ".svg")
			if err != nil {
				t.Errorf("Fetch failed: %v", err)
			}
		}()
	}
	wg.Wait()

	if n := requestCount.Load(); n != 1 {
		t.Fatalf("expected 1 HTTP request, got %d", n)
	}

	// Verify file exists on disk
	if !dl.Cached("sh", "concurrent-test", ".svg") {
		t.Fatal("icon should be cached on disk after Fetch")
	}
}

func TestIconDownload_UnknownSet(t *testing.T) {
	cacheDir := t.TempDir()
	dl := NewIconDownloader(testManifest(), cacheDir, true, "sh")

	_, err := dl.Fetch("unknown", "foo", ".svg")
	if err == nil {
		t.Fatal("expected error for unknown set")
	}
}

func TestIconDownload_PathTraversal(t *testing.T) {
	cacheDir := t.TempDir()
	dl := NewIconDownloader(testManifest(), cacheDir, true, "sh")

	traversalNames := []string{"../../etc/passwd", "../foo", "a/b", ".."}
	for _, name := range traversalNames {
		t.Run(name, func(t *testing.T) {
			_, err := dl.Fetch("sh", name, ".svg")
			if err == nil {
				t.Fatal("expected error for traversal attempt")
			}
		})
	}
}

func TestIconDownload_Disabled(t *testing.T) {
	cacheDir := t.TempDir()
	dl := NewIconDownloader(testManifest(), cacheDir, false, "sh")

	_, err := dl.Fetch("sh", "nginx", ".svg")
	if err == nil {
		t.Fatal("expected error when download is disabled")
	}
}

func TestIconDownload_SafePath(t *testing.T) {
	cacheDir := t.TempDir()
	dl := NewIconDownloader(testManifest(), cacheDir, true, "sh")

	// Valid path
	path, err := dl.safePath("sh", "nginx", ".svg")
	if err != nil {
		t.Fatalf("safePath valid: %v", err)
	}
	expected := filepath.Join(cacheDir, "sh", "nginx.svg")
	if path != expected {
		t.Fatalf("safePath: got %q, want %q", path, expected)
	}

	// Traversal attempt
	_, err = dl.safePath("sh", "../../etc/passwd", ".svg")
	if err == nil {
		t.Fatal("safePath should reject traversal")
	}
}
