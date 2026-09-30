// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package tailscale

import (
	"testing"

	"github.com/almeidapaulopt/tsdproxy/internal/model"
)

// ---------------------------------------------------------------------------
// cleanTags
// ---------------------------------------------------------------------------

func TestCleanTagsEmptyString(t *testing.T) {
	t.Parallel()

	result := cleanTags("")
	if len(result) != 0 {
		t.Fatalf("expected empty slice, got %v", result)
	}
}

func TestCleanTagsSingleTag(t *testing.T) {
	t.Parallel()

	result := cleanTags("tag:web")
	if len(result) != 1 || result[0] != "tag:web" {
		t.Fatalf("expected [\"tag:web\"], got %v", result)
	}
}

func TestCleanTagsMultipleTags(t *testing.T) {
	t.Parallel()

	result := cleanTags("tag:web, tag:server, tag:proxy")
	expected := []string{"tag:web", "tag:server", "tag:proxy"}
	if len(result) != len(expected) {
		t.Fatalf("expected %d tags, got %d: %v", len(expected), len(result), result)
	}
	for i, tag := range expected {
		if result[i] != tag {
			t.Fatalf("expected tag[%d]=%q, got %q", i, tag, result[i])
		}
	}
}

func TestCleanTagsTrailingComma(t *testing.T) {
	t.Parallel()

	result := cleanTags("tag:a,tag:b,")
	expected := []string{"tag:a", "tag:b"}
	if len(result) != len(expected) {
		t.Fatalf("expected %d tags, got %d: %v", len(expected), len(result), result)
	}
}

func TestCleanTagsOnlyWhitespace(t *testing.T) {
	t.Parallel()

	result := cleanTags("   ,  ,  ")
	if len(result) != 0 {
		t.Fatalf("expected empty slice for whitespace-only input, got %v", result)
	}
}

func TestCleanTagsMixedEmptyAndValid(t *testing.T) {
	t.Parallel()

	result := cleanTags(",tag:alpha,,tag:beta,")
	if len(result) != 2 || result[0] != "tag:alpha" || result[1] != "tag:beta" {
		t.Fatalf("expected [\"tag:alpha\",\"tag:beta\"], got %v", result)
	}
}

// ---------------------------------------------------------------------------
// primaryScheme
// ---------------------------------------------------------------------------

func TestPrimarySchemeEmptyPorts(t *testing.T) {
	t.Parallel()

	scheme := primaryScheme(nil)
	if scheme != model.ProtoHTTPS {
		t.Fatalf("expected %q for empty ports, got %q", model.ProtoHTTPS, scheme)
	}
}

func TestPrimarySchemeEmptyPortConfigList(t *testing.T) {
	t.Parallel()

	scheme := primaryScheme(model.PortConfigList{})
	if scheme != model.ProtoHTTPS {
		t.Fatalf("expected %q for empty PortConfigList, got %q", model.ProtoHTTPS, scheme)
	}
}

func TestPrimarySchemeHTTPSPort(t *testing.T) {
	t.Parallel()

	ports := model.PortConfigList{
		"1": {ProxyProtocol: model.ProtoHTTPS},
	}
	scheme := primaryScheme(ports)
	if scheme != model.ProtoHTTPS {
		t.Fatalf("expected %q, got %q", model.ProtoHTTPS, scheme)
	}
}

func TestPrimarySchemeHTTPOnly(t *testing.T) {
	t.Parallel()

	ports := model.PortConfigList{
		"1": {ProxyProtocol: model.ProtoHTTP},
	}
	scheme := primaryScheme(ports)
	if scheme != model.ProtoHTTP {
		t.Fatalf("expected %q, got %q", model.ProtoHTTP, scheme)
	}
}

func TestPrimarySchemeTCPOnly(t *testing.T) {
	t.Parallel()

	ports := model.PortConfigList{
		"1": {ProxyProtocol: model.ProtoTCP},
	}
	scheme := primaryScheme(ports)
	if scheme != model.ProtoTCP {
		t.Fatalf("expected %q, got %q", model.ProtoTCP, scheme)
	}
}

func TestPrimarySchemeUDPOnly(t *testing.T) {
	t.Parallel()

	ports := model.PortConfigList{
		"1": {ProxyProtocol: model.ProtoUDP},
	}
	scheme := primaryScheme(ports)
	if scheme != model.ProtoUDP {
		t.Fatalf("expected %q, got %q", model.ProtoUDP, scheme)
	}
}

func TestPrimarySchemeMixedHTTPSAndHTTP(t *testing.T) {
	t.Parallel()

	ports := model.PortConfigList{
		"1": {ProxyProtocol: model.ProtoHTTP},
		"2": {ProxyProtocol: model.ProtoHTTPS},
	}
	scheme := primaryScheme(ports)
	if scheme != model.ProtoHTTPS {
		t.Fatalf("expected %q (HTTPS has priority), got %q", model.ProtoHTTPS, scheme)
	}
}

// ---------------------------------------------------------------------------
// isNilInterface
// ---------------------------------------------------------------------------

func TestIsNilInterfaceNil(t *testing.T) {
	t.Parallel()

	if !isNilInterface(nil) {
		t.Fatal("nil should be nil")
	}
}

func TestIsNilInterfaceTypedNilPointer(t *testing.T) {
	t.Parallel()

	var p *int
	if !isNilInterface(p) {
		t.Fatal("typed nil pointer should be nil")
	}
}

func TestIsNilInterfaceNonNilPointer(t *testing.T) {
	t.Parallel()

	x := 42
	if isNilInterface(&x) {
		t.Fatal("non-nil pointer should not be nil")
	}
}

func TestIsNilInterfaceNonNilString(t *testing.T) {
	t.Parallel()

	s := "hello"
	if isNilInterface(s) {
		t.Fatal("non-nil string should not be nil")
	}
}

func TestIsNilInterfaceNilSlice(t *testing.T) {
	t.Parallel()

	var s []int
	if !isNilInterface(s) {
		t.Fatal("nil slice should be nil")
	}
}

func TestIsNilInterfaceNonNilSlice(t *testing.T) {
	t.Parallel()

	s := []int{1, 2, 3}
	if isNilInterface(s) {
		t.Fatal("non-nil slice should not be nil")
	}
}

func TestIsNilInterfaceNilMap(t *testing.T) {
	t.Parallel()

	var m map[string]int
	if !isNilInterface(m) {
		t.Fatal("nil map should be nil")
	}
}

func TestIsNilInterfaceNilChan(t *testing.T) {
	t.Parallel()

	var ch chan int
	if !isNilInterface(ch) {
		t.Fatal("nil chan should be nil")
	}
}

func TestIsNilInterfaceNilFunc(t *testing.T) {
	t.Parallel()

	var f func()
	if !isNilInterface(f) {
		t.Fatal("nil func should be nil")
	}
}
