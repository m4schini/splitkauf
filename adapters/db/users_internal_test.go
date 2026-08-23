// SPDX-License-Identifier: CC0-1.0

package db

import (
	"strings"
	"testing"
)

func TestNullEmpty(t *testing.T) {
	t.Parallel()

	longEmail := strings.Repeat("a", 10000) + "@example.com"

	tests := []struct {
		name    string
		input   string
		wantNil bool
		want    string
	}{
		{name: "empty string maps to NULL", input: "", wantNil: true},
		{name: "email passes through", input: "user@example.com", want: "user@example.com"},
		{name: "single space is not empty", input: " ", want: " "},
		{name: "whitespace only is not trimmed", input: " \t\n ", want: " \t\n "},
		{name: "value with surrounding spaces is not trimmed", input: "  user@example.com  ", want: "  user@example.com  "},
		{name: "lowercase null literal is unchanged", input: "null", want: "null"},
		{name: "uppercase NULL literal is unchanged", input: "NULL", want: "NULL"},
		{name: "NUL byte is unchanged", input: "\x00", want: "\x00"},
		{name: "unicode is unchanged", input: "üser+tag@exämple.москва", want: "üser+tag@exämple.москва"},
		{name: "emoji is unchanged", input: "🙂@example.com", want: "🙂@example.com"},
		{name: "very long string is not truncated", input: longEmail, want: longEmail},
		{name: "tab is not empty", input: "\t", want: "\t"},
		{name: "newline is not empty", input: "\n", want: "\n"},
		{name: "control characters are not empty", input: "\x01\x1f\x7f", want: "\x01\x1f\x7f"},
		{name: "zero digit is not empty", input: "0", want: "0"},
		{name: "false literal is not empty", input: "false", want: "false"},
		{name: "nil literal is not empty", input: "nil", want: "nil"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := nullEmpty(tt.input)

			if tt.wantNil {
				// Must be an untyped nil interface: drivers bind a typed nil
				// (e.g. (*string)(nil)) differently from SQL NULL.
				if got != nil {
					t.Fatalf("nullEmpty(%q) = %#v (dynamic type %T), want untyped nil", tt.input, got, got)
				}

				return
			}

			if got == nil {
				t.Fatalf("nullEmpty(%q) = nil, want %q", tt.input, tt.want)
			}

			s, ok := got.(string)
			if !ok {
				t.Fatalf("nullEmpty(%q) has dynamic type %T, want string", tt.input, got)
			}

			if s != tt.want {
				t.Errorf("nullEmpty(%q) = %q, want %q", tt.input, s, tt.want)
			}
		})
	}
}

// TestNullEmptyIsDeterministic checks that repeated calls with the same input
// yield the same result, i.e. the helper keeps no state between calls.
func TestNullEmptyIsDeterministic(t *testing.T) {
	t.Parallel()

	for _, in := range []string{"", "user@example.com", " "} {
		first := nullEmpty(in)
		second := nullEmpty(in)

		if first != second {
			t.Errorf("nullEmpty(%q) not deterministic: first %#v, second %#v", in, first, second)
		}
	}
}
