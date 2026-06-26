// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/almeidapaulopt/tsdproxy/web"
)

const (
	iconsDownloadTimeout = 5 * time.Minute
	maxIconTarballSize   = 100 << 20 // 100 MB decompressed limit (G110 mitigation)
)

// isIconsSubcommand checks if the CLI arg is the icons subcommand.
func isIconsSubcommand() bool {
	return len(os.Args) > 1 && os.Args[1] == "icons"
}

// runIconsDownload handles `tsdproxy icons download`.
func runIconsDownload() int {
	// Parse --icons-dir flag
	var iconsDir string
	args := os.Args[2:]
	for i := 0; i < len(args); i++ {
		if args[i] == "--icons-dir" && i+1 < len(args) {
			iconsDir = args[i+1]
			i++
		}
	}
	if iconsDir == "" {
		dataDir := os.Getenv("TSDPROXY_DATADIR")
		if dataDir == "" {
			dataDir = "/data"
		}
		iconsDir = filepath.Join(dataDir, "icons")
	}

	// Parse embedded manifest
	manifest, err := web.ParseIconManifest()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: parse manifest: %v\n", err)
		return 1
	}

	client := &http.Client{
		Timeout: iconsDownloadTimeout,
	}

	allOK := true
	for setName, setCfg := range manifest {
		if err := downloadSet(client, setName, setCfg, iconsDir); err != nil {
			fmt.Fprintf(os.Stderr, "error: %s: %v\n", setName, err)
			allOK = false
		}
	}

	if allOK {
		fmt.Println("All icon sets downloaded successfully.")
		return 0
	}
	return 1
}

// downloadSet downloads a single icon set tarball, verifies SHA256, and extracts SVGs.
func downloadSet(client *http.Client, setName string, setCfg web.IconSet, iconsDir string) error {
	dest := filepath.Join(iconsDir, setName)

	// Check if already cached
	existingFiles := countSVGFiles(dest)
	if existingFiles > 0 { //nolint:mnd // mnd: SVG file count check
		fmt.Printf("✓ %s/ (%d icons, cached)\n", setName, existingFiles)
		return nil
	}

	// Build tarball URL
	ref := setCfg.Version
	var url string
	if setCfg.RefType == "commits" {
		url = fmt.Sprintf("https://github.com/%s/archive/%s.tar.gz", setCfg.Repo, ref)
	} else {
		url = fmt.Sprintf("https://github.com/%s/archive/refs/%s/%s.tar.gz", setCfg.Repo, setCfg.RefType, ref)
	}

	fmt.Printf("Downloading %s/ (%s @ %s) ...\n", setName, setCfg.Repo, ref)
	resp, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	// Read body and verify SHA256
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}
	hash := sha256.Sum256(body)
	gotHash := hex.EncodeToString(hash[:])
	if gotHash != setCfg.SHA256 {
		return fmt.Errorf("SHA256 mismatch: expected %s, got %s", setCfg.SHA256, gotHash)
	}

	// Extract tarball
	if mkErr := os.MkdirAll(dest, 0o755); mkErr != nil { //nolint:gosec,mnd // dest is under --icons-dir, 0o755 standard dir perm
		return fmt.Errorf("create dir: %w", mkErr)
	}

	archivePrefix := fmt.Sprintf("%s-%s", repoShortName(setCfg.Repo), strings.TrimPrefix(ref, "v"))
	count, err := extractSVGTarball(body, dest, archivePrefix, setCfg.SvgDir)
	if err != nil {
		return fmt.Errorf("extract: %w", err)
	}

	fmt.Printf("  → %d icons saved to %s/ (sha256 verified)\n", count, setName)
	return nil
}

// repoShortName extracts the repo name from "org/repo".
func repoShortName(repo string) string {
	parts := strings.SplitN(repo, "/", 2) //nolint:mnd // SplitN count
	if len(parts) == 2 {                  //nolint:mnd // SplitN result check
		return parts[1]
	}
	return parts[0]
}

// extractSVGTarball extracts SVG files from a gzipped tarball, stripping 2 path components.
func extractSVGTarball(data []byte, dest, archivePrefix, svgDir string) (int, error) {
	gr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return 0, fmt.Errorf("gzip: %w", err)
	}
	defer gr.Close()

	tr := tar.NewReader(gr)
	count := 0
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return count, fmt.Errorf("tar: %w", err)
		}
		if header.Typeflag != tar.TypeReg {
			continue
		}

		// Expected path: {archivePrefix}/{svgDir}/{name}.svg
		// Strip the first 2 components
		parts := strings.SplitN(header.Name, "/", 3) //nolint:mnd // SplitN count: set/svgDir/name
		if len(parts) < 3 {                          //nolint:mnd // SplitN result check
			continue
		}
		if parts[0] != archivePrefix || parts[1] != svgDir {
			continue
		}
		if !strings.HasSuffix(parts[2], ".svg") {
			continue
		}

		//nolint:gosec // paths from tarball are validated (must match archivePrefix/svgDir/name.svg pattern)
		outPath := filepath.Join(dest, parts[2])
		if mkErr := os.MkdirAll(filepath.Dir(outPath), 0o755); mkErr != nil { //nolint:gosec,mnd // 0o755 standard dir perm
			return count, fmt.Errorf("mkdir: %w", mkErr)
		}
		outFile, crErr := os.Create(outPath) //nolint:gosec
		if crErr != nil {
			return count, fmt.Errorf("create %s: %w", parts[2], crErr)
		}
		if _, cpErr := io.Copy(outFile, io.LimitReader(tr, maxIconTarballSize)); cpErr != nil {
			outFile.Close()
			return count, fmt.Errorf("write %s: %w", parts[2], cpErr)
		}
		outFile.Close()
		count++
	}
	return count, nil
}

// countSVGFiles counts .svg files in a directory.
func countSVGFiles(dir string) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	count := 0
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".svg") {
			count++
		}
	}
	return count
}

// parseIconsFlags checks for icons subcommand and routes to the right handler.
func handleIconsCommand() int {
	if len(os.Args) < 3 { //nolint:mnd // CLI arg count check
		fmt.Fprintf(os.Stderr, "usage: tsdproxy icons download [--icons-dir <path>]\n")
		return 1
	}
	switch os.Args[2] {
	case "download":
		return runIconsDownload()
	default:
		fmt.Fprintf(os.Stderr, "unknown icons subcommand: %s\n", os.Args[2])
		return 1
	}
}
