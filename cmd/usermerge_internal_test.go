// SPDX-License-Identifier: CC0-1.0

package cmd

import (
	"strings"
	"testing"
)

func TestParseSelector(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in        string
		wantKind  string
		wantValue string
		wantErr   bool
	}{
		{in: "local:alex", wantKind: "local", wantValue: "alex", wantErr: false},
		{in: "oidc:238941579532", wantKind: "oidc", wantValue: "238941579532", wantErr: false},
		{
			in: "uuid:0d9c1e64-0000-0000-0000-000000000000", wantKind: "uuid",
			wantValue: "0d9c1e64-0000-0000-0000-000000000000", wantErr: false,
		},
		// A value containing colons splits only on the first one.
		{in: "oidc:urn:example:sub", wantKind: "oidc", wantValue: "urn:example:sub", wantErr: false},
		{in: "alex", wantKind: "", wantValue: "", wantErr: true},              // missing prefix
		{in: "local:", wantKind: "", wantValue: "", wantErr: true},            // empty value
		{in: "email:a@b.example", wantKind: "", wantValue: "", wantErr: true}, // unknown prefix
		{in: "", wantKind: "", wantValue: "", wantErr: true},
	}
	for _, sel := range tests {
		kind, value, err := parseSelector(sel.in)
		if sel.wantErr {
			if err == nil {
				t.Errorf("parseSelector(%q) = %q/%q, want error", sel.in, kind, value)
			} else if !strings.Contains(err.Error(), sel.in) && sel.in != "" {
				t.Errorf("parseSelector(%q) error %q does not name the selector", sel.in, err)
			}

			continue
		}

		if err != nil {
			t.Errorf("parseSelector(%q): %v", sel.in, err)

			continue
		}

		if kind != sel.wantKind || value != sel.wantValue {
			t.Errorf("parseSelector(%q) = %q/%q, want %q/%q", sel.in, kind, value, sel.wantKind, sel.wantValue)
		}
	}
}

func TestConfirmMergeYesSkipsPrompt(t *testing.T) {
	t.Parallel()

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
	t.Parallel()

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
