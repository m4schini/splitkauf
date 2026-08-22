// SPDX-License-Identifier: CC0-1.0

package cmd

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

// testPassword is the stand-in password value reused across the
// TestResolvePasswordFromStdin cases below.
const testPassword = "s3cret-password"

func TestResolvePasswordFromStdin(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{"plain", testPassword, testPassword},
		{"trailing newline stripped", testPassword + "\n", testPassword},
		{"trailing crlf stripped", testPassword + "\r\n", testPassword},
		{"internal spaces kept", "two words here", "two words here"},
	}
	for _, tst := range tests {
		t.Run(tst.name, func(t *testing.T) {
			t.Parallel()

			got, err := resolvePassword(true, strings.NewReader(tst.in), io.Discard)
			if err != nil {
				t.Fatalf("resolvePassword: %v", err)
			}

			if got != tst.want {
				t.Errorf("resolvePassword(%q) = %q, want %q", tst.in, got, tst.want)
			}
		})
	}
}

// TestResolvePasswordInteractiveWithoutTTY confirms that, without --password-stdin
// and no terminal (a piped stdin), the command fails clearly rather than hanging
// or silently reading an empty password.
func TestResolvePasswordInteractiveWithoutTTY(t *testing.T) {
	t.Parallel()

	_, err := resolvePassword(false, bytes.NewBufferString("irrelevant"), io.Discard)
	if err == nil {
		t.Fatal("expected an error when no terminal is available")
	}

	if !strings.Contains(err.Error(), "--password-stdin") {
		t.Errorf("error %q should point the user at --password-stdin", err)
	}
}
