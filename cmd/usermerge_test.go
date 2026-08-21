// SPDX-License-Identifier: CC0-1.0

package cmd

import (
	"strings"
	"testing"
)

func TestParseSelector(t *testing.T) {
	tests := []struct {
		in        string
		wantKind  string
		wantValue string
		wantErr   bool
	}{
		{in: "local:alex", wantKind: "local", wantValue: "alex"},
		{in: "oidc:238941579532", wantKind: "oidc", wantValue: "238941579532"},
		{in: "uuid:0d9c1e64-0000-0000-0000-000000000000", wantKind: "uuid", wantValue: "0d9c1e64-0000-0000-0000-000000000000"},
		// A value containing colons splits only on the first one.
		{in: "oidc:urn:example:sub", wantKind: "oidc", wantValue: "urn:example:sub"},
		{in: "alex", wantErr: true},              // missing prefix
		{in: "local:", wantErr: true},            // empty value
		{in: "email:a@b.example", wantErr: true}, // unknown prefix
		{in: "", wantErr: true},
	}
	for _, tt := range tests {
		kind, value, err := parseSelector(tt.in)
		if tt.wantErr {
			if err == nil {
				t.Errorf("parseSelector(%q) = %q/%q, want error", tt.in, kind, value)
			} else if !strings.Contains(err.Error(), tt.in) && tt.in != "" {
				t.Errorf("parseSelector(%q) error %q does not name the selector", tt.in, err)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseSelector(%q): %v", tt.in, err)
			continue
		}
		if kind != tt.wantKind || value != tt.wantValue {
			t.Errorf("parseSelector(%q) = %q/%q, want %q/%q", tt.in, kind, value, tt.wantKind, tt.wantValue)
		}
	}
}

func TestConfirmMergeYesSkipsPrompt(t *testing.T) {
	var out strings.Builder
	ok, err := confirmMerge(true, strings.NewReader(""), &out)
	if err != nil || !ok {
		t.Errorf("confirmMerge(--yes) = %v, %v; want true, nil", ok, err)
	}
	if out.Len() != 0 {
		t.Errorf("confirmMerge(--yes) printed %q, want no prompt", out.String())
	}
}

func TestConfirmMergeNonTTYWithoutYesFails(t *testing.T) {
	// A strings.Reader is not a terminal, mirroring a piped stdin.
	var out strings.Builder
	ok, err := confirmMerge(false, strings.NewReader("y\n"), &out)
	if err == nil || ok {
		t.Fatalf("confirmMerge(non-TTY) = %v, %v; want error", ok, err)
	}
	if !strings.Contains(err.Error(), "--yes") {
		t.Errorf("error %q should point at --yes", err)
	}
}
