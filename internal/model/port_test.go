// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package model

import (
	"fmt"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestNewPortLongLabel_TCPProxyPort(t *testing.T) {
	cfg, err := NewPortLongLabel("8222/tcp:22/tcp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.ProxyPort != 8222 {
		t.Errorf("ProxyPort: got %d, want 8222", cfg.ProxyPort)
	}
	if cfg.ProxyProtocol != "tcp" {
		t.Errorf("ProxyProtocol: got %q, want \"tcp\"", cfg.ProxyProtocol)
	}

	target := cfg.GetFirstTarget()
	if target.Scheme != "tcp" {
		t.Errorf("target scheme: got %q, want \"tcp\"", target.Scheme)
	}
	if target.Host != "0.0.0.0:22" {
		t.Errorf("target host: got %q, want \"0.0.0.0:22\"", target.Host)
	}
}

func TestNewPortLongLabel_HTTPSProxyWithHTTPTarget(t *testing.T) {
	cfg, err := NewPortLongLabel("443/https:3000/http")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.ProxyPort != 443 {
		t.Errorf("ProxyPort: got %d, want 443", cfg.ProxyPort)
	}
	if cfg.ProxyProtocol != "https" {
		t.Errorf("ProxyProtocol: got %q, want \"https\"", cfg.ProxyProtocol)
	}

	target := cfg.GetFirstTarget()
	if target.Scheme != "http" {
		t.Errorf("target scheme: got %q, want \"http\"", target.Scheme)
	}
	if target.Host != "0.0.0.0:3000" {
		t.Errorf("target host: got %q, want \"0.0.0.0:3000\"", target.Host)
	}
}

func TestNewPortLongLabel_ShortForm_DefaultsToHTTPS_HTTP(t *testing.T) {
	cfg, err := NewPortLongLabel("443:80")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.ProxyPort != 443 {
		t.Errorf("ProxyPort: got %d, want 443", cfg.ProxyPort)
	}
	if cfg.ProxyProtocol != "https" {
		t.Errorf("ProxyProtocol: got %q, want \"https\" (default)", cfg.ProxyProtocol)
	}

	target := cfg.GetFirstTarget()
	if target.Scheme != "http" {
		t.Errorf("target scheme: got %q, want \"http\" (default)", target.Scheme)
	}
	if target.Host != "0.0.0.0:80" {
		t.Errorf("target host: got %q, want \"0.0.0.0:80\"", target.Host)
	}
}

func TestNewPortLongLabel_TCPDifferentPorts(t *testing.T) {
	cfg, err := NewPortLongLabel("2222/tcp:22/tcp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.ProxyPort != 2222 {
		t.Errorf("ProxyPort: got %d, want 2222", cfg.ProxyPort)
	}
	if cfg.ProxyProtocol != "tcp" {
		t.Errorf("ProxyProtocol: got %q, want \"tcp\"", cfg.ProxyProtocol)
	}

	target := cfg.GetFirstTarget()
	if target.Scheme != "tcp" {
		t.Errorf("target scheme: got %q, want \"tcp\"", target.Scheme)
	}
	if target.Host != "0.0.0.0:22" {
		t.Errorf("target host: got %q, want \"0.0.0.0:22\"", target.Host)
	}
}

func TestNewPortLongLabel_TCPDefaultTargetProtocol(t *testing.T) {
	cfg, err := NewPortLongLabel("8222/tcp:22")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	target := cfg.GetFirstTarget()

	// When no target protocol is specified after the port,
	// parseTargetSegment now defaults to the proxy protocol for TCP proxies.
	// This fixes the issue where "8222/tcp:22" would produce an "http" target
	// scheme, causing incorrect routing through Docker's HTTP port mapping.
	if target.Scheme != "tcp" {
		t.Errorf("target scheme: got %q, want \"tcp\" (should match proxy protocol)", target.Scheme)
	}
	if target.Host != "0.0.0.0:22" {
		t.Errorf("target host: got %q, want \"0.0.0.0:22\"", target.Host)
	}
}

func TestNewPortLongLabel_Redirect(t *testing.T) {
	cfg, err := NewPortLongLabel("443/https->https://example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !cfg.IsRedirect {
		t.Error("IsRedirect: got false, want true")
	}
	if cfg.ProxyPort != 443 {
		t.Errorf("ProxyPort: got %d, want 443", cfg.ProxyPort)
	}

	target := cfg.GetFirstTarget()
	if target.Scheme != "https" {
		t.Errorf("target scheme: got %q, want \"https\"", target.Scheme)
	}
	if target.Host != "example.com" {
		t.Errorf("target host: got %q, want \"example.com\"", target.Host)
	}
}

func TestNewPortLongLabel_TargetPortWithoutProtocol(t *testing.T) {
	cfg, err := NewPortLongLabel("443/https:8080")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	target := cfg.GetFirstTarget()
	if target.Scheme != "http" {
		t.Errorf("target scheme: got %q, want \"http\" (default)", target.Scheme)
	}
	if target.Host != "0.0.0.0:8080" {
		t.Errorf("target host: got %q, want \"0.0.0.0:8080\"", target.Host)
	}
}

func TestNewPortLongLabel_InvalidFormat(t *testing.T) {
	_, err := NewPortLongLabel("invalid")
	if err == nil {
		t.Error("expected error for format without separator")
	}
}

func TestNewPortLongLabel_InvalidProxyPort(t *testing.T) {
	_, err := NewPortLongLabel("abc/tcp:22/tcp")
	if err == nil {
		t.Error("expected error for non-numeric proxy port")
	}
}

func TestNewPortLongLabel_InvalidTargetPort(t *testing.T) {
	_, err := NewPortLongLabel("8222/tcp:abc/tcp")
	if err == nil {
		t.Error("expected error for non-numeric target port")
	}
}

func TestNewPortShortLabel_HTTPS(t *testing.T) {
	cfg, err := NewPortShortLabel("443/https")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.ProxyPort != 443 {
		t.Errorf("ProxyPort: got %d, want 443", cfg.ProxyPort)
	}
	if cfg.ProxyProtocol != "https" {
		t.Errorf("ProxyProtocol: got %q, want \"https\"", cfg.ProxyProtocol)
	}
}

func TestNewPortShortLabel_TCP(t *testing.T) {
	cfg, err := NewPortShortLabel("22/tcp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.ProxyPort != 22 {
		t.Errorf("ProxyPort: got %d, want 22", cfg.ProxyPort)
	}
	if cfg.ProxyProtocol != "tcp" {
		t.Errorf("ProxyProtocol: got %q, want \"tcp\"", cfg.ProxyProtocol)
	}
}

func TestNewPortLongLabel_ProxyPortOutOfRange(t *testing.T) {
	_, err := NewPortLongLabel("0/tcp:22/tcp")
	if err == nil {
		t.Error("expected error for proxy port 0")
	}

	_, err = NewPortLongLabel("65536/tcp:22/tcp")
	if err == nil {
		t.Error("expected error for proxy port 65536")
	}

	_, err = NewPortLongLabel("-1/tcp:22/tcp")
	if err == nil {
		t.Error("expected error for negative proxy port")
	}
}

func TestNewPortLongLabel_TargetPortOutOfRange(t *testing.T) {
	_, err := NewPortLongLabel("8222/tcp:0/tcp")
	if err == nil {
		t.Error("expected error for target port 0")
	}

	_, err = NewPortLongLabel("8222/tcp:65536/tcp")
	if err == nil {
		t.Error("expected error for target port 65536")
	}
}

func TestNewPortLongLabel_PortBoundaryValues(t *testing.T) {
	cfg, err := NewPortLongLabel("443/https:1/http")
	if err != nil {
		t.Fatalf("unexpected error for port 1: %v", err)
	}
	target := cfg.GetFirstTarget()
	if target.Host != "0.0.0.0:1" {
		t.Errorf("target host: got %q, want \"0.0.0.0:1\"", target.Host)
	}

	cfg, err = NewPortLongLabel("443/https:65535/http")
	if err != nil {
		t.Fatalf("unexpected error for port 65535: %v", err)
	}
	target = cfg.GetFirstTarget()
	if target.Host != "0.0.0.0:65535" {
		t.Errorf("target host: got %q, want \"0.0.0.0:65535\"", target.Host)
	}
}

func TestNewPortShortLabel_ProxyPortOutOfRange(t *testing.T) {
	_, err := NewPortShortLabel("0/tcp")
	if err == nil {
		t.Error("expected error for proxy port 0")
	}

	_, err = NewPortShortLabel("65536/https")
	if err == nil {
		t.Error("expected error for proxy port 65536")
	}
}

func TestPortConfig_GetFirstTarget_Empty(t *testing.T) {
	cfg := PortConfig{}
	target := cfg.GetFirstTarget()
	if target != nil {
		t.Errorf("empty config: got %v, want nil", target)
	}
}

func TestPortConfig_GetFirstTargetString_Empty(t *testing.T) {
	cfg := PortConfig{}
	if got := cfg.GetFirstTargetString(); got != "" {
		t.Errorf("empty config: got %q, want empty string", got)
	}
}

func TestPortConfig_ReplaceTarget(t *testing.T) {
	cfg, _ := NewPortLongLabel("443/https:80/http")

	original := cfg.GetFirstTarget()
	replacement := urlMustParse("http://mycontainer:8080")

	cfg.ReplaceTarget(original, replacement)

	result := cfg.GetFirstTarget()
	if result.Host != "mycontainer:8080" {
		t.Errorf("after ReplaceTarget: got host %q, want \"mycontainer:8080\"", result.Host)
	}
}

func urlMustParse(s string) *url.URL {
	u, err := url.Parse(s)
	if err != nil {
		panic(err)
	}
	return u
}

func TestIsPortRange(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"56000-56100", true},
		{"1-10", true},
		{"443", false},
		{"abc-def", false},
		{"56000", false},
		{"", false},
		{"56000-", false},
		{"-56100", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := isPortRange(tt.input); got != tt.want {
				t.Errorf("isPortRange(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestParsePortRange(t *testing.T) {
	t.Run("valid range", func(t *testing.T) {
		start, end, err := parsePortRange("56000-56100")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if start != 56000 || end != 56100 {
			t.Errorf("got start=%d end=%d, want start=56000 end=56100", start, end)
		}
	})

	t.Run("single port range", func(t *testing.T) {
		start, end, err := parsePortRange("8080-8080")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if start != 8080 || end != 8080 {
			t.Errorf("got start=%d end=%d, want start=8080 end=8080", start, end)
		}
	})

	t.Run("inverted range", func(t *testing.T) {
		_, _, err := parsePortRange("100-50")
		if err == nil {
			t.Error("expected error for inverted range")
		}
	})

	t.Run("out of range", func(t *testing.T) {
		_, _, err := parsePortRange("1-70000")
		if err == nil {
			t.Error("expected error for port > 65535")
		}
	})

	t.Run("zero port", func(t *testing.T) {
		_, _, err := parsePortRange("0-10")
		if err == nil {
			t.Error("expected error for port 0")
		}
	})

	t.Run("invalid format", func(t *testing.T) {
		_, _, err := parsePortRange("abc")
		if err == nil {
			t.Error("expected error for non-numeric")
		}
	})
}

func TestIsPortRangeLabel(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"range both sides", "56000-56100/udp:56000-56100/udp", true},
		{"range proxy only", "56000-56100/udp:8080/udp", true},
		{"range target only", "443/https:8080-8090/http", true},
		{"no range", "443/https:80/http", false},
		{"single port", "443:80", false},
		{"redirect", "443/https->https://example.com", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsPortRangeLabel(tt.input); got != tt.want {
				t.Errorf("IsPortRangeLabel(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestExpandPortRangeLabel_BothRanges(t *testing.T) {
	result, err := ExpandPortRangeLabel("56000-56002/udp:56000-56002/udp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result) != 3 {
		t.Fatalf("expected 3 expanded ports, got %d", len(result))
	}

	expected := []struct {
		targetHost string
		proxyPort  int
	}{
		{"0.0.0.0:56000", 56000},
		{"0.0.0.0:56001", 56001},
		{"0.0.0.0:56002", 56002},
	}

	for i, exp := range expected {
		key := "range_0"
		switch i {
		case 1:
			key = "range_1"
		case 2:
			key = "range_2"
		}
		cfg, ok := result[key]
		if !ok {
			t.Fatalf("missing key %q", key)
		}
		if cfg.ProxyPort != exp.proxyPort {
			t.Errorf("port %d: ProxyPort = %d, want %d", i, cfg.ProxyPort, exp.proxyPort)
		}
		if cfg.ProxyProtocol != "udp" {
			t.Errorf("port %d: ProxyProtocol = %q, want \"udp\"", i, cfg.ProxyProtocol)
		}
		target := cfg.GetFirstTarget()
		if target.Scheme != "udp" {
			t.Errorf("port %d: target scheme = %q, want \"udp\"", i, target.Scheme)
		}
		if target.Host != exp.targetHost {
			t.Errorf("port %d: target host = %q, want %q", i, target.Host, exp.targetHost)
		}
	}
}

func TestExpandPortRangeLabel_ProxyRangeSingleTarget(t *testing.T) {
	result, err := ExpandPortRangeLabel("56000-56002/udp:8080/udp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result) != 3 {
		t.Fatalf("expected 3 expanded ports, got %d", len(result))
	}

	for i, key := range []string{"range_0", "range_1", "range_2"} {
		cfg := result[key]
		target := cfg.GetFirstTarget()
		if target.Host != "0.0.0.0:8080" {
			t.Errorf("port %d: target host = %q, want \"0.0.0.0:8080\"", i, target.Host)
		}
	}
}

func TestExpandPortRangeLabel_SingleProxyRangeTarget(t *testing.T) {
	result, err := ExpandPortRangeLabel("8080/udp:56000-56002/udp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result) != 3 {
		t.Fatalf("expected 3 expanded ports, got %d", len(result))
	}

	for i, key := range []string{"range_0", "range_1", "range_2"} {
		cfg := result[key]
		if cfg.ProxyPort != 8080 {
			t.Errorf("port %d: ProxyPort = %d, want 8080", i, cfg.ProxyPort)
		}
	}
}

func TestExpandPortRangeLabel_MismatchedRanges(t *testing.T) {
	_, err := ExpandPortRangeLabel("56000-56002/udp:60000-60005/udp")
	if err == nil {
		t.Error("expected error for mismatched range lengths")
	}
}

func TestExpandPortRangeLabel_RedirectRejected(t *testing.T) {
	_, err := ExpandPortRangeLabel("56000-56002/udp->https://example.com")
	if err == nil {
		t.Error("expected error for redirect with range")
	}
}

func TestExpandPortRangeLabel_InvalidProxyRange(t *testing.T) {
	_, err := ExpandPortRangeLabel("abc-def/udp:8080/udp")
	if err == nil {
		t.Error("expected error for invalid proxy range")
	}
}

func TestExpandPortRangeLabel_InvalidTargetRange(t *testing.T) {
	_, err := ExpandPortRangeLabel("8080/udp:abc-def/udp")
	if err == nil {
		t.Error("expected error for invalid target range")
	}
}

func TestExpandPortRangeLabel_TCPRange(t *testing.T) {
	result, err := ExpandPortRangeLabel("5000-5002/tcp:4000-4002/tcp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result) != 3 {
		t.Fatalf("expected 3 expanded ports, got %d", len(result))
	}

	cfg := result["range_0"]
	if cfg.ProxyProtocol != "tcp" {
		t.Errorf("ProxyProtocol = %q, want \"tcp\"", cfg.ProxyProtocol)
	}
	target := cfg.GetFirstTarget()
	if target.Scheme != "tcp" {
		t.Errorf("target scheme = %q, want \"tcp\"", target.Scheme)
	}
	if cfg.ProxyPort != 5000 {
		t.Errorf("ProxyPort = %d, want 5000", cfg.ProxyPort)
	}
	if target.Host != "0.0.0.0:4000" {
		t.Errorf("target host = %q, want \"0.0.0.0:4000\"", target.Host)
	}
}

func TestExpandPortRangeLabel_DefaultProtocol(t *testing.T) {
	result, err := ExpandPortRangeLabel("56000-56001:56000-56001")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cfg := result["range_0"]
	if cfg.ProxyProtocol != "https" {
		t.Errorf("ProxyProtocol = %q, want \"https\" (default)", cfg.ProxyProtocol)
	}
	target := cfg.GetFirstTarget()
	if target.Scheme != "http" {
		t.Errorf("target scheme = %q, want \"http\" (default)", target.Scheme)
	}
}

func TestIsPortRangeShortLabel(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"56000-56100/udp", true},
		{"56000-56100", true},
		{"443/https", false},
		{"443", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := IsPortRangeShortLabel(tt.input); got != tt.want {
				t.Errorf("IsPortRangeShortLabel(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestExpandPortRangeShortLabel(t *testing.T) {
	result, err := ExpandPortRangeShortLabel("56000-56002/udp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result) != 3 {
		t.Fatalf("expected 3 expanded ports, got %d", len(result))
	}

	expected := []int{56000, 56001, 56002}
	for i, port := range expected {
		key := fmt.Sprintf("range_%d", i)
		cfg, ok := result[key]
		if !ok {
			t.Fatalf("missing key %q", key)
		}
		if cfg.ProxyPort != port {
			t.Errorf("entry %d: ProxyPort = %d, want %d", i, cfg.ProxyPort, port)
		}
		if cfg.ProxyProtocol != "udp" {
			t.Errorf("entry %d: ProxyProtocol = %q, want \"udp\"", i, cfg.ProxyProtocol)
		}
	}
}

func TestExpandPortRangeShortLabel_NotRange(t *testing.T) {
	result, err := ExpandPortRangeShortLabel("443/https")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result) != 1 {
		t.Fatalf("expected 1 port, got %d", len(result))
	}

	cfg := result["range_0"]
	if cfg.ProxyPort != 443 {
		t.Errorf("ProxyPort = %d, want 443", cfg.ProxyPort)
	}
	if cfg.ProxyProtocol != "https" {
		t.Errorf("ProxyProtocol = %q, want \"https\"", cfg.ProxyProtocol)
	}
}

func TestNewPortLongLabel_TLSValidateDefault(t *testing.T) {
	cfg, err := NewPortLongLabel("443/https:80/http")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !cfg.TLSValidate {
		t.Errorf("TLSValidate: got %v, want true (DefaultTLSValidate)", cfg.TLSValidate)
	}
}

func TestNewPortShortLabel_TLSValidateDefault(t *testing.T) {
	cfg, err := NewPortShortLabel("443/https")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !cfg.TLSValidate {
		t.Errorf("TLSValidate: got %v, want true (DefaultTLSValidate)", cfg.TLSValidate)
	}
}

func TestDefaultPortConfig_TLSValidateDefault(t *testing.T) {
	cfg := defaultPortConfig("test")

	if !cfg.TLSValidate {
		t.Errorf("defaultPortConfig TLSValidate: got %v, want true (DefaultTLSValidate)", cfg.TLSValidate)
	}
}

func TestExpandPortConfigs_TLSValidateDefault(t *testing.T) {
	result, err := ExpandPortRangeLabel("5000-5002/tcp:4000-4002/tcp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for key, cfg := range result {
		if !cfg.TLSValidate {
			t.Errorf("expandPortConfigs %q TLSValidate: got %v, want true (DefaultTLSValidate)", key, cfg.TLSValidate)
		}
	}
}

func TestExpandPortRangeShortLabel_TLSValidateDefault(t *testing.T) {
	result, err := ExpandPortRangeShortLabel("56000-56002/udp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for key, cfg := range result {
		if !cfg.TLSValidate {
			t.Errorf("ExpandPortRangeShortLabel %q TLSValidate: got %v, want true (DefaultTLSValidate)", key, cfg.TLSValidate)
		}
	}
}

func TestPortConfig_ConcurrentGetAndReplace(t *testing.T) {
	cfg, _ := NewPortLongLabel("443/https:80/http")

	original := cfg.GetFirstTarget()

	var stop int32
	var wg sync.WaitGroup

	// Readers
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for atomic.LoadInt32(&stop) == 0 {
				target := cfg.GetFirstTarget()
				if target == nil {
					t.Error("GetFirstTarget returned nil")
					return
				}
			}
		}()
	}

	// Writers
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for atomic.LoadInt32(&stop) == 0 {
				replacement := urlMustParse(fmt.Sprintf("http://backend-%d:8080", id))
				cfg.ReplaceTarget(original, replacement)
				// Swap back so readers always have a valid target
				cfg.ReplaceTarget(replacement, original)
			}
		}(i)
	}

	// Let them run for a bit
	time.Sleep(100 * time.Millisecond)
	atomic.StoreInt32(&stop, 1)
	wg.Wait()
}

func TestPortConfig_ConcurrentAddAndGet(t *testing.T) {
	cfg := PortConfig{
		targets: &targetState{},
	}

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			u := urlMustParse(fmt.Sprintf("http://backend-%d:8080", id))
			cfg.AddTarget(u)
		}(i)
	}
	wg.Wait()

	targets := cfg.GetTargets()
	if len(targets) != 10 {
		t.Errorf("expected 10 targets, got %d", len(targets))
	}
}
