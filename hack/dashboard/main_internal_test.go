// SPDX-License-Identifier: CC0-1.0

package main

import (
	"bytes"
	"errors"
	"flag"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// runStripDeferredFixture is a minimal golangci-lint v2 config containing both
// a deferred and a non-deferred linters.disable entry.
const runStripDeferredFixture = `version: "2"
linters:
  default: none
  enable:
    - govet
  disable:
    # deferred: flaky in CI, re-enable later
    - errcheck
    # still enforced
    - ineffassign
`

// writeConfigFile writes contents into dir under name and returns the full path.
func writeConfigFile(t *testing.T, dir, name, contents string) string {
	t.Helper()

	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("os.WriteFile(%q) error = %v", path, err)
	}

	return path
}

// captureStdout swaps os.Stdout for a pipe while fn runs and returns everything
// fn wrote to it. runStripDeferred writes to the package-level os.Stdout, so
// tests using this helper must not call t.Parallel().
func captureStdout(t *testing.T, fn func()) []byte {
	t.Helper()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() error = %v", err)
	}

	orig := os.Stdout

	os.Stdout = w
	defer func() { os.Stdout = orig }()

	read := make(chan []byte, 1)

	go func() {
		b, _ := io.ReadAll(r)
		read <- b
	}()

	fn()

	if err := w.Close(); err != nil {
		t.Fatalf("closing stdout pipe writer: %v", err)
	}

	out := <-read

	if err := r.Close(); err != nil {
		t.Fatalf("closing stdout pipe reader: %v", err)
	}

	return out
}

// wantStrippedFixture is the exact output runStripDeferred must emit for
// runStripDeferredFixture.
func wantStrippedFixture(t *testing.T) []byte {
	t.Helper()

	want, err := stripDeferred([]byte(runStripDeferredFixture))
	if err != nil {
		t.Fatalf("stripDeferred() error = %v", err)
	}

	return want
}

//nolint:paralleltest // swaps the process-global os.Stdout via captureStdout for the call's duration
func TestRunStripDeferredWritesStrippedConfigToStdout(t *testing.T) {
	// No t.Parallel: os.Stdout is swapped for the duration of the call.
	path := writeConfigFile(t, t.TempDir(), "config.yml", runStripDeferredFixture)
	want := wantStrippedFixture(t)

	var runErr error

	got := captureStdout(t, func() { runErr = runStripDeferred([]string{path}) })

	if runErr != nil {
		t.Fatalf("runStripDeferred() error = %v, want nil", runErr)
	}

	if string(got) != string(want) {
		t.Errorf("runStripDeferred() stdout = %q, want %q", got, want)
	}
}

//nolint:paralleltest // chdirs the process and swaps os.Stdout via captureStdout across the table
func TestRunStripDeferredResolvesInputPath(t *testing.T) {
	// No t.Parallel: cases chdir the process and swap os.Stdout.
	tests := []struct {
		name  string
		setup func(t *testing.T) []string
	}{
		{
			name: "nil args default to .golangci.yml in the working directory",
			setup: func(t *testing.T) []string {
				t.Helper()
				dir := t.TempDir()
				writeConfigFile(t, dir, ".golangci.yml", runStripDeferredFixture)
				t.Chdir(dir)

				return nil
			},
		},
		{
			name: "empty args behave like nil args",
			setup: func(t *testing.T) []string {
				t.Helper()
				dir := t.TempDir()
				writeConfigFile(t, dir, ".golangci.yml", runStripDeferredFixture)
				t.Chdir(dir)

				return []string{}
			},
		},
		{
			name: "explicit path overrides the default",
			setup: func(t *testing.T) []string {
				t.Helper()
				dir := t.TempDir()
				// A default-named config in the CWD must be ignored.
				writeConfigFile(t, dir, ".golangci.yml", "version: \"2\"\n")
				t.Chdir(dir)

				return []string{writeConfigFile(t, t.TempDir(), "custom.yml", runStripDeferredFixture)}
			},
		},
		{
			name: "path after a bare double dash is accepted",
			setup: func(t *testing.T) []string {
				t.Helper()

				return []string{"--", writeConfigFile(t, t.TempDir(), "custom.yml", runStripDeferredFixture)}
			},
		},
		{
			name: "extra positional args are ignored",
			setup: func(t *testing.T) []string {
				t.Helper()
				dir := t.TempDir()
				path := writeConfigFile(t, dir, "custom.yml", runStripDeferredFixture)

				return []string{path, filepath.Join(dir, "does-not-exist.yml"), "junk"}
			},
		},
	}

	//nolint:paralleltest // chdirs the process and swaps os.Stdout via captureStdout across the table
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := tt.setup(t)
			want := wantStrippedFixture(t)

			var runErr error

			got := captureStdout(t, func() { runErr = runStripDeferred(args) })

			if runErr != nil {
				t.Fatalf("runStripDeferred(%q) error = %v, want nil", args, runErr)
			}

			if string(got) != string(want) {
				t.Errorf("runStripDeferred(%q) stdout = %q, want %q", args, got, want)
			}
		})
	}
}

//nolint:paralleltest // swaps both os.Stdin and os.Stdout for the call's duration
func TestRunStripDeferredReadsStdinForDashPath(t *testing.T) {
	// No t.Parallel: both os.Stdin and os.Stdout are swapped.
	path := writeConfigFile(t, t.TempDir(), "stdin.yml", runStripDeferredFixture)

	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("os.Open(%q) error = %v", path, err)
	}

	origStdin := os.Stdin
	os.Stdin = f

	t.Cleanup(func() {
		os.Stdin = origStdin

		_ = f.Close()
	})

	// Prove the default path is not consulted: the CWD has no .golangci.yml.
	t.Chdir(t.TempDir())

	want := wantStrippedFixture(t)

	var runErr error

	got := captureStdout(t, func() { runErr = runStripDeferred([]string{"-"}) })

	if runErr != nil {
		t.Fatalf("runStripDeferred([-]) error = %v, want nil", runErr)
	}

	if string(got) != string(want) {
		t.Errorf("runStripDeferred([-]) stdout = %q, want %q", got, want)
	}
}

//nolint:paralleltest // chdirs the process and swaps os.Stdout via captureStdout across the table
func TestRunStripDeferredErrors(t *testing.T) {
	// No t.Parallel: cases chdir the process and swap os.Stdout.
	tests := []struct {
		name         string
		setup        func(t *testing.T) []string
		wantContains []string
		wantErr      error
	}{
		{
			name: "unknown flag fails flag parsing",
			setup: func(t *testing.T) []string {
				t.Helper()

				return []string{"-unknown"}
			},
			wantContains: []string{"parsing flags:"},
		},
		{
			name: "help flag reports flag.ErrHelp",
			setup: func(t *testing.T) []string {
				t.Helper()

				return []string{"-h"}
			},
			wantContains: []string{"parsing flags:", "help requested"},
			wantErr:      flag.ErrHelp,
		},
		{
			name: "dash prefixed path is treated as a flag",
			setup: func(t *testing.T) []string {
				t.Helper()

				return []string{"-x.yml"}
			},
			wantContains: []string{"parsing flags:"},
		},
		{
			name: "missing explicit file",
			setup: func(t *testing.T) []string {
				t.Helper()

				return []string{filepath.Join(t.TempDir(), "missing.yml")}
			},
			wantContains: []string{"reading ", "missing.yml"},
			wantErr:      fs.ErrNotExist,
		},
		{
			name: "missing default file in the working directory",
			setup: func(t *testing.T) []string {
				t.Helper()
				t.Chdir(t.TempDir())

				return nil
			},
			wantContains: []string{"reading .golangci.yml:"},
			wantErr:      fs.ErrNotExist,
		},
		{
			name: "empty input document",
			setup: func(t *testing.T) []string {
				t.Helper()

				return []string{writeConfigFile(t, t.TempDir(), "empty.yml", "")}
			},
			wantContains: []string{"stripping deferred linters:"},
			wantErr:      ErrEmptyDocument,
		},
		{
			name: "unparseable yaml",
			setup: func(t *testing.T) []string {
				t.Helper()

				return []string{writeConfigFile(t, t.TempDir(), "broken.yml", "linters:\n\tdisable: []\n")}
			},
			wantContains: []string{"stripping deferred linters:", "parsing config:"},
		},
		{
			name: "linters key missing",
			setup: func(t *testing.T) []string {
				t.Helper()

				return []string{writeConfigFile(t, t.TempDir(), "no-linters.yml", "version: \"2\"\n")}
			},
			wantContains: []string{"stripping deferred linters:"},
			wantErr:      ErrKeyNotFound,
		},
		{
			name: "linters.disable key missing",
			setup: func(t *testing.T) []string {
				t.Helper()

				const cfg = "version: \"2\"\nlinters:\n  default: none\n  enable:\n    - govet\n"

				return []string{writeConfigFile(t, t.TempDir(), "no-disable.yml", cfg)}
			},
			wantContains: []string{"stripping deferred linters:"},
			wantErr:      ErrKeyNotFound,
		},
		{
			name: "linters is not a mapping",
			setup: func(t *testing.T) []string {
				t.Helper()

				const cfg = "version: \"2\"\nlinters: none\n"

				return []string{writeConfigFile(t, t.TempDir(), "scalar-linters.yml", cfg)}
			},
			wantContains: []string{"stripping deferred linters:"},
			wantErr:      ErrNotAMapping,
		},
		{
			name: "linters.disable is not a sequence",
			setup: func(t *testing.T) []string {
				t.Helper()

				const cfg = "version: \"2\"\nlinters:\n  disable: errcheck\n"

				return []string{writeConfigFile(t, t.TempDir(), "scalar-disable.yml", cfg)}
			},
			wantContains: []string{"stripping deferred linters:"},
			wantErr:      ErrNotASequence,
		},
	}

	//nolint:paralleltest // chdirs the process and swaps os.Stdout via captureStdout across the table
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := tt.setup(t)

			var runErr error

			out := captureStdout(t, func() { runErr = runStripDeferred(args) })

			if runErr == nil {
				t.Fatalf("runStripDeferred(%q) error = nil, want error", args)
			}

			for _, want := range tt.wantContains {
				if !strings.Contains(runErr.Error(), want) {
					t.Errorf("runStripDeferred(%q) error = %q, want it to contain %q", args, runErr, want)
				}
			}

			if tt.wantErr != nil && !errors.Is(runErr, tt.wantErr) {
				t.Errorf("runStripDeferred(%q) error = %v, want errors.Is(..., %v)", args, runErr, tt.wantErr)
			}

			if len(out) != 0 {
				t.Errorf("runStripDeferred(%q) wrote %q to stdout, want nothing", args, out)
			}
		})
	}
}

// runStripDeferredMixedFixture pairs a deferred entry with entries that must
// survive: a plainly-commented one and one whose prose merely mentions the
// word "deferred" away from the start of the comment line.
const runStripDeferredMixedFixture = `version: "2"
linters:
  default: none
  enable:
    - govet
  disable:
    # deferred: fires on pre-existing code, re-enable after cleanup.
    - varnamelen
    # reason: not deferred, permanently wrong for this repo; owner @m4schini.
    - noinlineerr
    # reason: superseded by wsl_v5; owner @m4schini.
    - wsl
`

// TestRunStripDeferredStdoutContent asserts what actually lands on stdout,
// rather than comparing it against stripDeferred's own output: deferred
// entries are gone, every other entry keeps its comment, and the re-encoded
// YAML still uses a 2-space indent.
//
//nolint:paralleltest // swaps the process-global os.Stdout via captureStdout for the call's duration
func TestRunStripDeferredStdoutContent(t *testing.T) {
	// No t.Parallel: os.Stdout is swapped for the duration of the call.
	path := writeConfigFile(t, t.TempDir(), "config.yml", runStripDeferredMixedFixture)

	var runErr error

	got := captureStdout(t, func() { runErr = runStripDeferred([]string{path}) })

	if runErr != nil {
		t.Fatalf("runStripDeferred() error = %v, want nil", runErr)
	}

	out := string(got)

	wantAbsent := []string{"deferred:", "varnamelen"}
	for _, absent := range wantAbsent {
		if strings.Contains(out, absent) {
			t.Errorf("stdout contains %q, want it dropped:\n%s", absent, out)
		}
	}

	wantPresent := []string{
		"version: \"2\"",
		"  disable:",
		"    - noinlineerr",
		"    - wsl",
		"# reason: not deferred, permanently wrong for this repo; owner @m4schini.",
		"# reason: superseded by wsl_v5; owner @m4schini.",
	}
	for _, present := range wantPresent {
		if !strings.Contains(out, present) {
			t.Errorf("stdout missing %q:\n%s", present, out)
		}
	}

	if disable := decodeDisableList(t, got); len(disable) != 2 {
		t.Errorf("disable list = %v, want [noinlineerr wsl]", disable)
	}
}

// TestRunStripDeferredEmptyDisableSequence covers the boundary between a
// misshaped linters.disable (an error) and a present-but-empty one, which is
// valid and must round-trip.
//
//nolint:paralleltest // swaps the process-global os.Stdout via captureStdout for the call's duration
func TestRunStripDeferredEmptyDisableSequence(t *testing.T) {
	// No t.Parallel: os.Stdout is swapped for the duration of the call.
	tests := []struct {
		name   string
		config string
	}{
		{"flow style empty sequence", "version: \"2\"\nlinters:\n  default: none\n  disable: []\n"},
		{"empty sequence with a comment", "version: \"2\"\nlinters:\n  # nothing disabled yet\n  disable: []\n"},
	}

	//nolint:paralleltest // swaps the process-global os.Stdout via captureStdout across the table
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeConfigFile(t, t.TempDir(), "config.yml", tt.config)

			var runErr error

			got := captureStdout(t, func() { runErr = runStripDeferred([]string{path}) })

			if runErr != nil {
				t.Fatalf("runStripDeferred() error = %v, want nil", runErr)
			}

			if len(got) == 0 {
				t.Fatal("runStripDeferred() wrote nothing to stdout, want a config")
			}

			if disable := decodeDisableList(t, got); len(disable) != 0 {
				t.Errorf("disable list = %v, want it to stay empty", disable)
			}
		})
	}
}

// TestRunStripDeferredRejectsMisshapedRoot covers the document-level shape
// errors that the per-key cases don't reach: the root itself must be a
// mapping, and a document carrying no nodes is empty.
//
//nolint:paralleltest // swaps the process-global os.Stdout via captureStdout for the call's duration
func TestRunStripDeferredRejectsMisshapedRoot(t *testing.T) {
	// No t.Parallel: os.Stdout is swapped for the duration of the call.
	tests := []struct {
		name    string
		config  string
		wantErr error
	}{
		{"root is a scalar", "just-a-string\n", ErrNotAMapping},
		{"root is a sequence", "- errcheck\n- wsl\n", ErrNotAMapping},
		{"document is only a comment", "# nothing here\n", ErrEmptyDocument},
		{"document is only whitespace", "   \n\n", ErrEmptyDocument},
	}

	//nolint:paralleltest // swaps the process-global os.Stdout via captureStdout across the table
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeConfigFile(t, t.TempDir(), "config.yml", tt.config)

			var runErr error

			out := captureStdout(t, func() { runErr = runStripDeferred([]string{path}) })

			if runErr == nil {
				t.Fatalf("runStripDeferred() error = nil, want error")
			}

			if !strings.Contains(runErr.Error(), "stripping deferred linters:") {
				t.Errorf("error = %q, want it to contain %q", runErr, "stripping deferred linters:")
			}

			if !errors.Is(runErr, tt.wantErr) {
				t.Errorf("error = %v, want errors.Is(..., %v)", runErr, tt.wantErr)
			}

			if len(out) != 0 {
				t.Errorf("wrote %q to stdout, want nothing", out)
			}
		})
	}
}

// TestRunStripDeferredLongHelpFlag pins the long form of the help flag to the
// same flag-parsing error path as "-h".
//
//nolint:paralleltest // swaps the process-global os.Stdout via captureStdout for the call's duration
func TestRunStripDeferredLongHelpFlag(t *testing.T) {
	// No t.Parallel: os.Stdout is swapped for the duration of the call.
	var runErr error

	out := captureStdout(t, func() { runErr = runStripDeferred([]string{"--help"}) })

	if !errors.Is(runErr, flag.ErrHelp) {
		t.Errorf("runStripDeferred([--help]) error = %v, want errors.Is(..., flag.ErrHelp)", runErr)
	}

	if runErr != nil && !strings.Contains(runErr.Error(), "parsing flags:") {
		t.Errorf("error = %q, want it to contain %q", runErr, "parsing flags:")
	}

	if len(out) != 0 {
		t.Errorf("wrote %q to stdout, want nothing", out)
	}
}

// runRenderCapture captures both the stdout produced by runRender and the error
// it returned. runRender writes straight to os.Stdout, so the helper swaps in a
// pipe for the duration of the call. Tests using it must not run in parallel.
func runRenderCapture(t *testing.T, args []string) (string, error) {
	t.Helper()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() error = %v", err)
	}

	orig := os.Stdout

	os.Stdout = w
	defer func() { os.Stdout = orig }()

	done := make(chan string, 1)

	go func() {
		var buf bytes.Buffer

		_, _ = io.Copy(&buf, r)
		done <- buf.String()
	}()

	runErr := runRender(args)

	if err := w.Close(); err != nil {
		t.Fatalf("closing pipe writer: %v", err)
	}

	out := <-done

	if err := r.Close(); err != nil {
		t.Fatalf("closing pipe reader: %v", err)
	}

	return out, runErr
}

func writeRenderFixture(t *testing.T, dir, name, content string) string {
	t.Helper()

	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("os.WriteFile(%q) error = %v", path, err)
	}

	return path
}

//nolint:paralleltest // swaps the process-global os.Stdout via runRenderCapture for the call's duration
func TestRunRenderWithoutInputsRendersNotAvailableSections(t *testing.T) {
	out, err := runRenderCapture(t, nil)
	if err != nil {
		t.Fatalf("runRender(nil) error = %v", err)
	}

	if out == "" {
		t.Fatal("runRender(nil) wrote nothing to stdout, want rendered body")
	}

	if !strings.Contains(out, "n/a") {
		t.Errorf("runRender(nil) output does not contain %q; got:\n%s", "n/a", out)
	}
}

// Absent flags, empty flag sets, flags pointing at files that do not exist and
// stray positional arguments must all take the zero-Inputs path and therefore
// produce byte-identical output.
//
//nolint:paralleltest // swaps the process-global os.Stdout via runRenderCapture for the call's duration
func TestRunRenderTreatsMissingInputsLikeNoFlags(t *testing.T) {
	baseline, err := runRenderCapture(t, nil)
	if err != nil {
		t.Fatalf("runRender(nil) error = %v", err)
	}

	missing := func(name string) string {
		return filepath.Join(t.TempDir(), name)
	}

	tests := []struct {
		name string
		args []string
	}{
		{name: "empty args", args: []string{}},
		{name: "missing coverage", args: []string{"-coverage", missing("coverage.out")}},
		{name: "missing tests", args: []string{"-tests", missing("tests.json")}},
		{name: "missing lint debt", args: []string{"-lint-debt", missing("lint.json")}},
		{name: "missing security", args: []string{"-security", missing("security.json")}},
		{name: "missing meta", args: []string{"-meta", missing("meta.json")}},
		{name: "missing prev body", args: []string{"-prev-body", missing("body.md")}},
		{
			name: "all flags missing",
			args: []string{
				"-coverage", missing("coverage.out"),
				"-tests", missing("tests.json"),
				"-lint-debt", missing("lint.json"),
				"-security", missing("security.json"),
				"-meta", missing("meta.json"),
				"-prev-body", missing("body.md"),
			},
		},
		{name: "empty flag values", args: []string{"-coverage", "", "-meta", ""}},
		{name: "positional args ignored", args: []string{"extra", "args"}},
	}

	//nolint:paralleltest // swaps the process-global os.Stdout via runRenderCapture across the table
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := runRenderCapture(t, tt.args)
			if err != nil {
				t.Fatalf("runRender(%q) error = %v, want nil", tt.args, err)
			}

			if out != baseline {
				t.Errorf("runRender(%q) output differs from the no-flag body\ngot:\n%s\nwant:\n%s", tt.args, out, baseline)
			}
		})
	}
}

//nolint:paralleltest // swaps the process-global os.Stdout via runRenderCapture
func TestRunRenderReturnsErrorsAndWritesNothing(t *testing.T) {
	// dirPlaceholder stands in for the per-case temp directory path, which is
	// only known at run time.
	const dirPlaceholder = "\x00tempdir\x00"

	tests := []struct {
		name string
		args func(t *testing.T, dir string) []string
		// wantErrContains are substrings that must all appear in the error.
		wantErrContains []string
		// wantErrOmits are substrings that must not appear in the error.
		wantErrOmits []string
		wantHelp     bool
	}{
		{
			name: "unknown flag",
			args: func(*testing.T, string) []string {
				return []string{"-bogus"}
			},
			wantErrContains: []string{"parsing flags"},
		},
		{
			name: "flag without value",
			args: func(*testing.T, string) []string {
				return []string{"-coverage"}
			},
			wantErrContains: []string{"parsing flags"},
		},
		{
			name: "help flag",
			args: func(*testing.T, string) []string {
				return []string{"-h"}
			},
			wantErrContains: []string{"parsing flags"},
			wantHelp:        true,
		},
		{
			name: "coverage path is a directory",
			args: func(_ *testing.T, dir string) []string {
				return []string{"-coverage", dir}
			},
			wantErrContains: []string{dirPlaceholder},
		},
		{
			name: "tests path is a directory",
			args: func(_ *testing.T, dir string) []string {
				return []string{"-tests", dir}
			},
			wantErrContains: []string{dirPlaceholder},
		},
		{
			name: "malformed meta json",
			args: func(t *testing.T, dir string) []string {
				t.Helper()

				return []string{"-meta", writeRenderFixture(t, dir, "meta.json", "{not json")}
			},
			wantErrContains: []string{"decoding", "meta.json"},
		},
		{
			name: "malformed security json",
			args: func(t *testing.T, dir string) []string {
				t.Helper()

				return []string{"-security", writeRenderFixture(t, dir, "security.json", "[[[")}
			},
			wantErrContains: []string{"decoding", "security.json"},
		},
		{
			name: "first loader error wins",
			args: func(t *testing.T, dir string) []string {
				t.Helper()

				return []string{
					"-coverage", dir,
					"-meta", writeRenderFixture(t, dir, "meta.json", "{not json"),
				}
			},
			wantErrContains: []string{dirPlaceholder},
			wantErrOmits:    []string{"meta.json"},
		},
	}

	//nolint:paralleltest // swaps the process-global os.Stdout via runRenderCapture across the table
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			args := tt.args(t, dir)

			out, err := runRenderCapture(t, args)
			if err == nil {
				t.Fatalf("runRender(%q) error = nil, want error", args)
			}

			if out != "" {
				t.Errorf("runRender(%q) wrote %q to stdout, want nothing on error", args, out)
			}

			if tt.wantHelp && !errors.Is(err, flag.ErrHelp) {
				t.Errorf("runRender(%q) error = %v, want it to wrap flag.ErrHelp", args, err)
			}

			msg := err.Error()

			for _, want := range tt.wantErrContains {
				if want == dirPlaceholder {
					want = dir
				}

				if !strings.Contains(msg, want) {
					t.Errorf("runRender(%q) error = %q, want it to contain %q", args, msg, want)
				}
			}

			for _, omit := range tt.wantErrOmits {
				if strings.Contains(msg, omit) {
					t.Errorf("runRender(%q) error = %q, want it to not mention %q", args, msg, omit)
				}
			}
		})
	}
}

//nolint:paralleltest // swaps os.Stdout via a manual os.Pipe for the call's duration
func TestRunRenderReportsStdoutWriteFailure(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() error = %v", err)
	}

	if err := r.Close(); err != nil {
		t.Fatalf("closing pipe reader: %v", err)
	}

	if err := w.Close(); err != nil {
		t.Fatalf("closing pipe writer: %v", err)
	}

	orig := os.Stdout

	os.Stdout = w
	defer func() { os.Stdout = orig }()

	err = runRender(nil)
	if err == nil {
		t.Fatal("runRender(nil) error = nil, want error when stdout is closed")
	}

	if !strings.Contains(err.Error(), "writing output") {
		t.Errorf("runRender(nil) error = %q, want it to contain %q", err, "writing output")
	}
}

// captureRunRender runs runRender with args while os.Stdout is redirected to a
// pipe, returning everything the call wrote to stdout together with its error.
// Usage output that flag.ContinueOnError writes to os.Stderr is discarded.
//
// It mutates package-level os.Stdout/os.Stderr, so tests using it must not call
// t.Parallel().
func captureRunRender(t *testing.T, args []string) (string, error) {
	t.Helper()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() error = %v", err)
	}

	devNull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("opening %s: %v", os.DevNull, err)
	}

	origStdout, origStderr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = w, devNull

	collected := make(chan string, 1)

	go func() {
		var buf bytes.Buffer

		_, _ = io.Copy(&buf, r)
		collected <- buf.String()
	}()

	runErr := runRender(args)

	os.Stdout, os.Stderr = origStdout, origStderr

	if err := w.Close(); err != nil {
		t.Fatalf("closing pipe writer: %v", err)
	}

	out := <-collected

	if err := r.Close(); err != nil {
		t.Fatalf("closing pipe reader: %v", err)
	}

	if err := devNull.Close(); err != nil {
		t.Fatalf("closing %s: %v", os.DevNull, err)
	}

	return out, runErr
}

// writeRenderInput writes content to a fresh temp file and returns its path.
func writeRenderInput(t *testing.T, name, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}

	return path
}

// missingPath returns a path inside a fresh temp dir that does not exist.
func missingPath(t *testing.T, name string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), name)
	if _, err := os.Stat(path); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("os.Stat(%q) error = %v, want fs.ErrNotExist", path, err)
	}

	return path
}

//nolint:paralleltest // swaps the process-global os.Stdout via captureRunRender across the table
func TestRunRenderAbsentInputsRenderNotAvailable(t *testing.T) {
	tests := []struct {
		name string
		args func(t *testing.T) []string
	}{
		{
			name: "nil args",
			args: func(*testing.T) []string { return nil },
		},
		{
			name: "empty args",
			args: func(*testing.T) []string { return []string{} },
		},
		{
			name: "missing coverage file",
			args: func(t *testing.T) []string {
				t.Helper()

				return []string{"-coverage", missingPath(t, "coverage.out")}
			},
		},
		{
			name: "missing tests file",
			args: func(t *testing.T) []string {
				t.Helper()

				return []string{"-tests", missingPath(t, "tests.json")}
			},
		},
		{
			name: "missing lint-debt file",
			args: func(t *testing.T) []string {
				t.Helper()

				return []string{"-lint-debt", missingPath(t, "lint.json")}
			},
		},
		{
			name: "missing security file",
			args: func(t *testing.T) []string {
				t.Helper()

				return []string{"-security", missingPath(t, "security.json")}
			},
		},
		{
			name: "missing meta file",
			args: func(t *testing.T) []string {
				t.Helper()

				return []string{"-meta", missingPath(t, "meta.json")}
			},
		},
		{
			name: "missing prev-body file",
			args: func(t *testing.T) []string {
				t.Helper()

				return []string{"-prev-body", missingPath(t, "body.md")}
			},
		},
		{
			name: "all flags missing at once",
			args: func(t *testing.T) []string {
				t.Helper()

				return []string{
					"-coverage", missingPath(t, "coverage.out"),
					"-tests", missingPath(t, "tests.json"),
					"-lint-debt", missingPath(t, "lint.json"),
					"-security", missingPath(t, "security.json"),
					"-meta", missingPath(t, "meta.json"),
					"-prev-body", missingPath(t, "body.md"),
				}
			},
		},
		{
			name: "extra positional arguments are ignored",
			args: func(t *testing.T) []string {
				t.Helper()

				return []string{"-coverage", missingPath(t, "coverage.out"), "leftover", "args"}
			},
		},
	}

	//nolint:paralleltest // swaps the process-global os.Stdout via captureRunRender across the table
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := captureRunRender(t, tt.args(t))
			if err != nil {
				t.Fatalf("runRender() error = %v, want nil", err)
			}

			if out == "" {
				t.Fatal("runRender() wrote nothing to stdout, want rendered body")
			}

			if !strings.Contains(out, "n/a") {
				t.Errorf("runRender() stdout = %q, want it to contain %q", out, "n/a")
			}
		})
	}
}

//nolint:paralleltest // swaps the process-global os.Stdout via captureRunRender across the table
func TestRunRenderMalformedInputReturnsError(t *testing.T) {
	const garbage = "this is not a valid dashboard input\n"

	tests := []struct {
		name     string
		flag     string
		file     string
		content  string
		wantVerb string
	}{
		{
			name:     "malformed coverage profile",
			flag:     "-coverage",
			file:     "coverage.out",
			content:  garbage,
			wantVerb: "parsing",
		},
		{
			name:     "malformed lint json",
			flag:     "-lint-debt",
			file:     "lint.json",
			content:  "{not json",
			wantVerb: "parsing",
		},
		{
			name:     "malformed security json",
			flag:     "-security",
			file:     "security.json",
			content:  "{not json",
			wantVerb: "decoding",
		},
		{
			name:     "malformed meta json",
			flag:     "-meta",
			file:     "meta.json",
			content:  "{not json",
			wantVerb: "decoding",
		},
		{
			name:     "empty security json",
			flag:     "-security",
			file:     "security.json",
			content:  "",
			wantVerb: "decoding",
		},
		{
			name:     "empty meta json",
			flag:     "-meta",
			file:     "meta.json",
			content:  "",
			wantVerb: "decoding",
		},
	}

	//nolint:paralleltest // swaps the process-global os.Stdout via captureRunRender across the table
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeRenderInput(t, tt.file, tt.content)

			out, err := captureRunRender(t, []string{tt.flag, path})
			if err == nil {
				t.Fatalf("runRender(%s %s) error = nil, want an error", tt.flag, path)
			}

			if !strings.Contains(err.Error(), path) {
				t.Errorf("runRender() error = %q, want it to name %q", err, path)
			}

			if !strings.Contains(err.Error(), tt.wantVerb) {
				t.Errorf("runRender() error = %q, want it to contain %q", err, tt.wantVerb)
			}

			if out != "" {
				t.Errorf("runRender() stdout = %q, want no body on error", out)
			}
		})
	}
}

//nolint:paralleltest // swaps the process-global os.Stdout via captureRunRender across the table
func TestRunRenderExistingButUnloadableInputIsNotTreatedAsAbsent(t *testing.T) {
	// A directory exists, so missing() must not swallow it: the loader runs and
	// fails instead of rendering "n/a".
	tests := []struct {
		name string
		flag string
	}{
		{name: "coverage", flag: "-coverage"},
		{name: "tests", flag: "-tests"},
		{name: "lint-debt", flag: "-lint-debt"},
		{name: "security", flag: "-security"},
		{name: "meta", flag: "-meta"},
		{name: "prev-body", flag: "-prev-body"},
	}

	//nolint:paralleltest // swaps the process-global os.Stdout via captureRunRender across the table
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()

			out, err := captureRunRender(t, []string{tt.flag, dir})
			if err == nil {
				t.Fatalf("runRender(%s %s) error = nil, want an error", tt.flag, dir)
			}

			if !strings.Contains(err.Error(), dir) {
				t.Errorf("runRender() error = %q, want it to name %q", err, dir)
			}

			if out != "" {
				t.Errorf("runRender() stdout = %q, want no body on error", out)
			}
		})
	}
}

//nolint:paralleltest // swaps the process-global os.Stdout via captureRunRender for the call's duration
func TestRunRenderEmptyPrevBodyIsNotAnError(t *testing.T) {
	path := writeRenderInput(t, "body.md", "")

	out, err := captureRunRender(t, []string{"-prev-body", path})
	if err != nil {
		t.Fatalf("runRender(-prev-body %s) error = %v, want nil", path, err)
	}

	if out == "" {
		t.Fatal("runRender() wrote nothing to stdout, want rendered body")
	}
}

//nolint:paralleltest // swaps the process-global os.Stdout via captureRunRender across the table
func TestRunRenderFlagErrors(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		wantErrHelp bool
	}{
		{
			name: "unknown flag",
			args: []string{"-bogus"},
		},
		{
			name: "flag missing its value",
			args: []string{"-coverage"},
		},
		{
			name:        "-h requests help",
			args:        []string{"-h"},
			wantErrHelp: true,
		},
		{
			name:        "-help requests help",
			args:        []string{"-help"},
			wantErrHelp: true,
		},
	}

	//nolint:paralleltest // swaps the process-global os.Stdout via captureRunRender across the table
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := captureRunRender(t, tt.args)
			if err == nil {
				t.Fatalf("runRender(%v) error = nil, want an error", tt.args)
			}

			if !strings.Contains(err.Error(), "parsing flags") {
				t.Errorf("runRender() error = %q, want it to contain %q", err, "parsing flags")
			}

			if tt.wantErrHelp && !errors.Is(err, flag.ErrHelp) {
				t.Errorf("runRender() error = %v, want it to wrap flag.ErrHelp", err)
			}

			if out != "" {
				t.Errorf("runRender() stdout = %q, want no body on error", out)
			}
		})
	}
}

// stdinFromBytes replaces os.Stdin with the read end of a pipe fed with
// content, restoring the original os.Stdin when the test finishes.
func stdinFromBytes(t *testing.T, content []byte) {
	t.Helper()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() error = %v", err)
	}

	orig := os.Stdin
	os.Stdin = r

	t.Cleanup(func() {
		os.Stdin = orig
		_ = r.Close()
	})

	go func() {
		defer func() { _ = w.Close() }()

		if len(content) > 0 {
			_, _ = w.Write(content)
		}
	}()
}

// writeTempFile writes content to name inside dir and returns the full path.
func writeTempFile(t *testing.T, dir, name string, content []byte) string {
	t.Helper()

	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("os.WriteFile(%q) error = %v", path, err)
	}

	return path
}

func TestReadPathOrStdinReadsFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	binary := []byte{0x00, 0x01, 0xff, 0xfe, '\n', 0x00, 'y', 'm', 'l'}

	regular := writeTempFile(t, dir, "config.yml", []byte("linters:\n  enable:\n    - govet\n"))
	empty := writeTempFile(t, dir, "empty.yml", nil)
	noNewline := writeTempFile(t, dir, "nonewline.yml", []byte("linters: {}"))
	binaryFile := writeTempFile(t, dir, "binary.bin", binary)
	missing := filepath.Join(dir, "does-not-exist.yml")

	tests := []struct {
		name          string
		path          string
		want          []byte
		wantErr       bool
		wantErrIs     error
		wantMsgPrefix string
	}{
		{
			name: "regular file",
			path: regular,
			want: []byte("linters:\n  enable:\n    - govet\n"),
		},
		{
			name: "empty file yields empty content and no error",
			path: empty,
			want: []byte{},
		},
		{
			name: "content without trailing newline is not normalized",
			path: noNewline,
			want: []byte("linters: {}"),
		},
		{
			name: "binary content is returned verbatim",
			path: binaryFile,
			want: binary,
		},
		{
			name:          "missing file wraps fs.ErrNotExist",
			path:          missing,
			wantErr:       true,
			wantErrIs:     fs.ErrNotExist,
			wantMsgPrefix: "reading " + missing + ": ",
		},
		{
			name:          "directory is an error, not data",
			path:          dir,
			wantErr:       true,
			wantMsgPrefix: "reading " + dir + ": ",
		},
		{
			name:          "empty path falls through to os.ReadFile",
			path:          "",
			wantErr:       true,
			wantErrIs:     fs.ErrNotExist,
			wantMsgPrefix: "reading : ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := readPathOrStdin(tt.path)

			if tt.wantErr {
				if err == nil {
					t.Fatalf("readPathOrStdin(%q) error = nil, want error", tt.path)
				}

				if got != nil {
					t.Errorf("readPathOrStdin(%q) = %q, want nil bytes on error", tt.path, got)
				}

				if tt.wantErrIs != nil && !errors.Is(err, tt.wantErrIs) {
					t.Errorf("readPathOrStdin(%q) error = %v, want errors.Is(err, %v)", tt.path, err, tt.wantErrIs)
				}

				if !strings.HasPrefix(err.Error(), tt.wantMsgPrefix) {
					t.Errorf("readPathOrStdin(%q) error = %q, want prefix %q", tt.path, err.Error(), tt.wantMsgPrefix)
				}

				return
			}

			if err != nil {
				t.Fatalf("readPathOrStdin(%q) error = %v", tt.path, err)
			}

			if got == nil {
				t.Fatalf("readPathOrStdin(%q) = nil, want non-nil bytes", tt.path)
			}

			if !bytes.Equal(got, tt.want) {
				t.Errorf("readPathOrStdin(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

//nolint:paralleltest // swaps os.Stdin via stdinFromBytes for the call's duration
func TestReadPathOrStdinReadsStdinForDashPath(t *testing.T) {
	content := []byte("linters:\n  enable:\n    - errcheck\n")
	stdinFromBytes(t, content)

	got, err := readPathOrStdin("-")
	if err != nil {
		t.Fatalf("readPathOrStdin(\"-\") error = %v", err)
	}

	if !bytes.Equal(got, content) {
		t.Errorf("readPathOrStdin(\"-\") = %q, want %q", got, content)
	}
}

//nolint:paralleltest // swaps os.Stdin via stdinFromBytes for the call's duration
func TestReadPathOrStdinEmptyStdinIsNotAnError(t *testing.T) {
	stdinFromBytes(t, nil)

	got, err := readPathOrStdin("-")
	if err != nil {
		t.Fatalf("readPathOrStdin(\"-\") error = %v", err)
	}

	if got == nil {
		t.Fatalf("readPathOrStdin(\"-\") = nil, want non-nil empty bytes")
	}

	if len(got) != 0 {
		t.Errorf("readPathOrStdin(\"-\") = %q, want empty", got)
	}
}

//nolint:paralleltest // swaps os.Stdin via a manual os.Pipe for the call's duration
func TestReadPathOrStdinWrapsStdinReadError(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() error = %v", err)
	}

	if err := w.Close(); err != nil {
		t.Fatalf("close write end: %v", err)
	}
	// Closing the read end makes every subsequent read fail with os.ErrClosed.
	if err := r.Close(); err != nil {
		t.Fatalf("close read end: %v", err)
	}

	orig := os.Stdin
	os.Stdin = r

	t.Cleanup(func() { os.Stdin = orig })

	got, err := readPathOrStdin("-")
	if err == nil {
		t.Fatalf("readPathOrStdin(\"-\") error = nil, want error")
	}

	if got != nil {
		t.Errorf("readPathOrStdin(\"-\") = %q, want nil bytes on error", got)
	}

	if !strings.HasPrefix(err.Error(), "reading stdin: ") {
		t.Errorf("readPathOrStdin(\"-\") error = %q, want prefix %q", err.Error(), "reading stdin: ")
	}

	if !errors.Is(err, os.ErrClosed) {
		t.Errorf("readPathOrStdin(\"-\") error = %v, want errors.Is(err, os.ErrClosed)", err)
	}
}

//nolint:paralleltest // swaps os.Stdin via stdinFromBytes across the table
func TestReadPathOrStdinOnlyExactDashMeansStdin(t *testing.T) {
	dir := t.TempDir()

	writeTempFile(t, dir, "-", []byte("file named dash"))
	writeTempFile(t, dir, "--", []byte("file named double dash"))
	writeTempFile(t, dir, " -", []byte("file named space dash"))

	t.Chdir(dir)

	// Any read of stdin during these cases would return this instead.
	stdinFromBytes(t, []byte("from stdin"))

	tests := []struct {
		name string
		path string
		want string
	}{
		{name: "exact dash reads stdin even though a file named - exists", path: "-", want: "from stdin"},
		{name: "double dash is a literal file name", path: "--", want: "file named double dash"},
		{name: "leading space defeats the sentinel", path: " -", want: "file named space dash"},
		{name: "dot slash dash reaches the file named -", path: "./-", want: "file named dash"},
	}

	//nolint:paralleltest // swaps os.Stdin via stdinFromBytes across the table
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := readPathOrStdin(tt.path)
			if err != nil {
				t.Fatalf("readPathOrStdin(%q) error = %v", tt.path, err)
			}

			if string(got) != tt.want {
				t.Errorf("readPathOrStdin(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

// NOTE: readPathOrStdin is already covered by TestReadPathOrStdinReadsFiles,
// TestReadPathOrStdinReadsStdinForDashPath, TestReadPathOrStdinEmptyStdinIsNotAnError,
// TestReadPathOrStdinWrapsStdinReadError and TestReadPathOrStdinOnlyExactDashMeansStdin
// in main_internal_test.go. Only the gaps those leave are added below; they reuse the
// existing stdinFromBytes helper (requires imports: bytes, errors, io/fs, os, path/filepath).

//nolint:paralleltest // swaps os.Stdin via stdinFromBytes for the call's duration
func TestReadPathOrStdinReadsStdinLargerThanPipeBuffer(t *testing.T) {
	// Comfortably larger than a pipe buffer, so the read only completes if the
	// implementation drains stdin to EOF instead of taking a single read.
	content := bytes.Repeat([]byte("linters:\n  disable:\n    - gochecknoglobals\n"), 20_000)
	stdinFromBytes(t, content)

	got, err := readPathOrStdin("-")
	if err != nil {
		t.Fatalf("readPathOrStdin(\"-\") error = %v", err)
	}

	if len(got) != len(content) {
		t.Fatalf("readPathOrStdin(\"-\") read %d bytes, want %d", len(got), len(content))
	}

	if !bytes.Equal(got, content) {
		t.Error("readPathOrStdin(\"-\") returned different bytes than were piped in")
	}
}

func TestReadPathOrStdinFileErrorKeepsPathError(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "absent.yml")

	got, err := readPathOrStdin(path)
	if err == nil {
		t.Fatalf("readPathOrStdin(%q) error = nil, want error", path)
	}

	if got != nil {
		t.Errorf("readPathOrStdin(%q) = %q, want nil bytes on error", path, got)
	}

	var pathErr *fs.PathError
	if !errors.As(err, &pathErr) {
		t.Fatalf("readPathOrStdin(%q) error = %v, want it to wrap *fs.PathError", path, err)
	}

	if pathErr.Path != path {
		t.Errorf("wrapped *fs.PathError.Path = %q, want %q", pathErr.Path, path)
	}
}

//nolint:paralleltest // swaps os.Stdin via a manual os.Pipe for the call's duration
func TestReadPathOrStdinStdinErrorKeepsPathError(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() error = %v", err)
	}

	if err := w.Close(); err != nil {
		t.Fatalf("close write end: %v", err)
	}
	// Closing the read end makes every subsequent read fail with os.ErrClosed.
	if err := r.Close(); err != nil {
		t.Fatalf("close read end: %v", err)
	}

	orig := os.Stdin
	os.Stdin = r

	t.Cleanup(func() { os.Stdin = orig })

	_, err = readPathOrStdin("-")
	if err == nil {
		t.Fatalf("readPathOrStdin(\"-\") error = nil, want error")
	}

	var pathErr *fs.PathError
	if !errors.As(err, &pathErr) {
		t.Fatalf("readPathOrStdin(\"-\") error = %v, want it to wrap *fs.PathError", err)
	}

	if pathErr.Err == nil || !errors.Is(pathErr.Err, os.ErrClosed) {
		t.Errorf("wrapped *fs.PathError.Err = %v, want os.ErrClosed", pathErr.Err)
	}
}
