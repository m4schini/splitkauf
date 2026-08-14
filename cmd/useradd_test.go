// SPDX-License-Identifier: CC0-1.0

package cmd

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

func TestResolvePasswordFromStdin(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"plain", "s3cret-password", "s3cret-password"},
		{"trailing newline stripped", "s3cret-password\n", "s3cret-password"},
		{"trailing crlf stripped", "s3cret-password\r\n", "s3cret-password"},
		{"internal spaces kept", "two words here", "two words here"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolvePassword(true, strings.NewReader(tt.in), io.Discard)
			if err != nil {
				t.Fatalf("resolvePassword: %v", err)
			}
			if got != tt.want {
				t.Errorf("resolvePassword(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestResolvePasswordInteractiveWithoutTTY confirms that, without --password-stdin
// and no terminal (a piped stdin), the command fails clearly rather than hanging
// or silently reading an empty password.
func TestResolvePasswordInteractiveWithoutTTY(t *testing.T) {
	_, err := resolvePassword(false, bytes.NewBufferString("irrelevant"), io.Discard)
	if err == nil {
		t.Fatal("expected an error when no terminal is available")
	}
	if !strings.Contains(err.Error(), "--password-stdin") {
		t.Errorf("error %q should point the user at --password-stdin", err)
	}
}
