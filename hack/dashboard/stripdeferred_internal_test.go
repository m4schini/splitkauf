// SPDX-License-Identifier: CC0-1.0

package main

import (
	"errors"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const fixtureConfig = `version: "2"
linters:
  default: all
  disable:
    # reason: contradicts the language's established style; owner @m4schini; review 2027-02.
    - noinlineerr

    # reason: superseded by wsl_v5; owner @m4schini; review 2027-02.
    - wsl

    # reason: superseded by gomodguard_v2; owner @m4schini; review 2027-02.
    - gomodguard

    # deferred: fires on pre-existing code, no reason to disable permanently.
    - varnamelen
`

func TestStripDeferredRemovesDeferredEntriesOnly(t *testing.T) {
	t.Parallel()

	out, err := stripDeferred([]byte(fixtureConfig))
	if err != nil {
		t.Fatalf("stripDeferred() error = %v", err)
	}

	disable := decodeDisableList(t, out)

	want := []string{"noinlineerr", "wsl", "gomodguard"}
	if len(disable) != len(want) {
		t.Fatalf("disable list = %v, want %v", disable, want)
	}

	for i, w := range want {
		if disable[i] != w {
			t.Errorf("disable[%d] = %q, want %q", i, disable[i], w)
		}
	}

	if strings.Contains(string(out), "deferred:") {
		t.Errorf("output still mentions \"deferred:\":\n%s", out)
	}
}

// TestStripDeferredDoesNotMatchProseSubstring covers a permanent disable
// whose comment happens to contain the word "deferred" outside the
// "deferred:" marker position: it must survive.
func TestStripDeferredDoesNotMatchProseSubstring(t *testing.T) {
	t.Parallel()

	config := `version: "2"
linters:
  disable:
    # reason: not deferred, permanently wrong for this repo; owner @m4schini; review 2027-02.
    - noinlineerr
`

	out, err := stripDeferred([]byte(config))
	if err != nil {
		t.Fatalf("stripDeferred() error = %v", err)
	}

	disable := decodeDisableList(t, out)

	if len(disable) != 1 || disable[0] != "noinlineerr" {
		t.Errorf("disable list = %v, want [noinlineerr] (prose \"deferred\" must not match)", disable)
	}
}

func TestStripDeferredMissingLintersKey(t *testing.T) {
	t.Parallel()

	_, err := stripDeferred([]byte("version: \"2\"\n"))
	if !errors.Is(err, ErrKeyNotFound) {
		t.Errorf("error = %v, want it to wrap ErrKeyNotFound", err)
	}
}

func TestStripDeferredMissingDisableKey(t *testing.T) {
	t.Parallel()

	_, err := stripDeferred([]byte("linters:\n  default: all\n"))
	if !errors.Is(err, ErrKeyNotFound) {
		t.Errorf("error = %v, want it to wrap ErrKeyNotFound", err)
	}
}

func TestStripDeferredEmptyDocument(t *testing.T) {
	t.Parallel()

	_, err := stripDeferred([]byte(""))
	if !errors.Is(err, ErrEmptyDocument) {
		t.Errorf("error = %v, want it to wrap ErrEmptyDocument", err)
	}
}

func TestStripDeferredDisableNotASequence(t *testing.T) {
	t.Parallel()

	_, err := stripDeferred([]byte("linters:\n  disable: not-a-list\n"))
	if !errors.Is(err, ErrNotASequence) {
		t.Errorf("error = %v, want it to wrap ErrNotASequence", err)
	}
}

// decodeDisableList decodes out as YAML and returns linters.disable as a
// plain string slice, for assertions that don't care about comments.
func decodeDisableList(t *testing.T, out []byte) []string {
	t.Helper()

	var decoded struct {
		Linters struct {
			Disable []string `yaml:"disable"`
		} `yaml:"linters"`
	}

	err := yaml.Unmarshal(out, &decoded)
	if err != nil {
		t.Fatalf("decoding stripDeferred() output: %v\noutput:\n%s", err, out)
	}

	return decoded.Linters.Disable
}
