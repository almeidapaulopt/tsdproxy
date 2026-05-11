// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package dom

import "testing"

func TestSafeID(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{name: "empty", input: "", expected: ""},
		{name: "alnum", input: "hello123", expected: "hello123"},
		{name: "hyphen", input: "my-app", expected: "my-app"},
		{name: "underscore encoded", input: "my_app", expected: "my_5f_app"},
		{name: "dot encoded", input: "my.app", expected: "my_2e_app"},
		{name: "colon encoded", input: "app:8080", expected: "app_3a_8080"},
		{name: "slash encoded", input: "path/to", expected: "path_2f_to"},
		{name: "space encoded", input: "hello world", expected: "hello_20_world"},
		{name: "multiple specials", input: "my.app:8080", expected: "my_2e_app_3a_8080"},
		{name: "safe only", input: "ABCxyz-012", expected: "ABCxyz-012"},
		{name: "latin-1", input: "café", expected: "caf_e9_"},
		{name: "CJK", input: "日本語", expected: "_65e5__672c__8a9e_"},
		{name: "emoji", input: "test😊", expected: "test_1f60a_"},
		{name: "at sign", input: "user@host", expected: "user_40_host"},
		{name: "consecutive dots", input: "a..b", expected: "a_2e__2e_b"},
		{name: "leading digit", input: "123abc", expected: "123abc"},
		{name: "brackets", input: "arr[0]", expected: "arr_5b_0_5d_"},
		{name: "literal _2e_ collision-free", input: "foo_2e_bar", expected: "foo_5f_2e_5f_bar"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SafeID(tt.input)
			if result != tt.expected {
				t.Errorf("SafeID(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestSafeIDUniqueness(t *testing.T) {
	pairs := []struct{ a, b string }{
		{"foo.bar", "foo_bar"},
		{"a:b", "a_b"},
		{"a.b", "a_b"},
		{"a:b", "a.b"},
		{"a b", "a_b"},
		{"foo.bar", "foo_2e_bar"},
	}
	for _, tt := range pairs {
		t.Run(tt.a+"_vs_"+tt.b, func(t *testing.T) {
			ra := SafeID(tt.a)
			rb := SafeID(tt.b)
			if ra == rb {
				t.Errorf("SafeID(%q) == SafeID(%q) == %q", tt.a, tt.b, ra)
			}
		})
	}
}
