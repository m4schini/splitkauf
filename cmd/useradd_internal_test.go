// SPDX-License-Identifier: CC0-1.0

package cmd

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/spf13/cobra"

	"github.com/m4schini/splitkauf/users"
)

// errUseraddTestBoom and errUseraddTestBrokenPipe stand in for a failing
// stdin reader in the resolvePassword tests below.
var (
	errUseraddTestBoom       = errors.New("boom")
	errUseraddTestBrokenPipe = errors.New("broken pipe")
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
		// TrimRight uses a cutset, so every trailing CR/LF is removed, not just
		// the single newline the doc comment mentions. A password genuinely
		// ending in newlines therefore cannot be supplied via stdin.
		{"all trailing newlines stripped", testPassword + "\n\n", testPassword},
		{"all trailing crlf pairs stripped", testPassword + "\r\n\r\n", testPassword},
		{"reversed line ending suffix stripped", testPassword + "\n\r", testPassword},
		{"mixed line ending suffix stripped", testPassword + "\r\r\n\n", testPassword},
		// Empty passwords are not rejected at this layer.
		{"empty input", "", ""},
		{"only newline", "\n", ""},
		{"only crlf", "\r\n", ""},
		{"only newlines", "\n\r\n\n", ""},
		// Only the trailing side is trimmed.
		{"leading newline kept", "\n" + testPassword, "\n" + testPassword},
		{"interior newline kept", "first\nsecond", "first\nsecond"},
		{"interior crlf kept, trailing stripped", "first\r\nsecond\r\n", "first\r\nsecond"},
		{"only crlf then trailing newline stripped", "\r\n\n", ""},
		{"mixed trailing cr lf run stripped", testPassword + "\r\n\r", testPassword},
		{"bare trailing cr stripped", testPassword + "\r", testPassword},
		{"trailing space before newline kept", testPassword + " \n", testPassword + " "},
		{"trailing tab kept", testPassword + "\t", testPassword + "\t"},
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

// TestResolvePasswordFromStdinReadError checks that a failing reader surfaces as
// a wrapped error instead of an empty password.
func TestResolvePasswordFromStdinReadError(t *testing.T) {
	t.Parallel()

	wantErr := errUseraddTestBoom

	got, err := resolvePassword(true, iotest.ErrReader(wantErr), io.Discard)
	if err == nil {
		t.Fatalf("resolvePassword = %q, want an error", got)
	}

	if !errors.Is(err, wantErr) {
		t.Errorf("resolvePassword error = %v, want it to wrap %v", err, wantErr)
	}

	if !strings.Contains(err.Error(), "reading password from stdin") {
		t.Errorf("error %q should mention reading password from stdin", err)
	}

	if got != "" {
		t.Errorf("resolvePassword returned %q on error, want empty string", got)
	}
}

// TestResolvePasswordStdinReadError checks that a failing stdin reader surfaces
// a wrapped error that still unwraps to the underlying cause, and that nothing
// is written to out on the stdin path.
func TestResolvePasswordStdinReadError(t *testing.T) {
	t.Parallel()

	errBroken := errUseraddTestBrokenPipe

	var out bytes.Buffer

	got, err := resolvePassword(true, iotest.ErrReader(errBroken), &out)
	if err == nil {
		t.Fatalf("resolvePassword = %q, want an error", got)
	}

	if !errors.Is(err, errBroken) {
		t.Errorf("errors.Is(%v, errBroken) = false, want true", err)
	}

	if !strings.Contains(err.Error(), "reading password from stdin") {
		t.Errorf("error %q should mention %q", err, "reading password from stdin")
	}

	if got != "" {
		t.Errorf("resolvePassword returned %q on error, want empty string", got)
	}

	if out.Len() != 0 {
		t.Errorf("out = %q, want nothing written on the stdin path", out.String())
	}
}

// TestResolvePasswordNoTerminal pins the errNoTerminal sentinel for every
// interactive input that is not a terminal: a non-*os.File reader, an *os.File
// that is not a tty (pipe read end, regular file), and a nil reader (the type
// assertion simply fails instead of panicking).
//
// Accepted gap: the interactive success path, the first/second term.ReadPassword
// failures, the "Password: " / "Confirm password: " prompt text and
// errPasswordMismatch all need a real PTY and are not reachable from a plain
// unit test without a pty helper.
func TestResolvePasswordNoTerminal(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		reader func(t *testing.T) io.Reader
		nilOut bool
	}{
		{
			name: "nil reader",
			reader: func(t *testing.T) io.Reader {
				t.Helper()

				return nil
			},
			nilOut: true,
		},
		{
			name: "pipe read end is not a terminal",
			reader: func(t *testing.T) io.Reader {
				t.Helper()

				r, w, err := os.Pipe()
				if err != nil {
					t.Fatalf("os.Pipe: %v", err)
				}

				t.Cleanup(func() {
					_ = r.Close()
					_ = w.Close()
				})

				return r
			},
		},
		{
			name: "regular file is not a terminal",
			reader: func(t *testing.T) io.Reader {
				t.Helper()

				f, err := os.CreateTemp(t.TempDir(), "password")
				if err != nil {
					t.Fatalf("os.CreateTemp: %v", err)
				}

				t.Cleanup(func() { _ = f.Close() })

				return f
			},
		},
	}
	for _, tst := range tests {
		t.Run(tst.name, func(t *testing.T) {
			t.Parallel()

			var (
				buf bytes.Buffer
				out io.Writer
			)

			if !tst.nilOut {
				out = &buf
			}

			got, err := resolvePassword(false, tst.reader(t), out)
			if err == nil {
				t.Fatalf("resolvePassword = %q, want errNoTerminal", got)
			}

			if !errors.Is(err, errNoTerminal) {
				t.Errorf("errors.Is(%v, errNoTerminal) = false, want true", err)
			}

			if got != "" {
				t.Errorf("resolvePassword returned %q, want empty string", got)
			}

			if !tst.nilOut && buf.Len() != 0 {
				t.Errorf("out = %q, want nothing written when no terminal is available", buf.String())
			}
		})
	}
}

// openNonTTYFile returns an *os.File that is a real file descriptor but not a
// terminal, so resolvePassword must take the "no terminal" path.
func openNonTTYFile(t *testing.T, kind string) *os.File {
	t.Helper()

	switch kind {
	case "pipe":
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatalf("os.Pipe: %v", err)
		}

		t.Cleanup(func() {
			_ = r.Close()
			_ = w.Close()
		})

		return r
	case "regular":
		f, err := os.CreateTemp(t.TempDir(), "password-input")
		if err != nil {
			t.Fatalf("os.CreateTemp: %v", err)
		}

		t.Cleanup(func() { _ = f.Close() })

		if _, err := f.WriteString("irrelevant\n"); err != nil {
			t.Fatalf("write temp file: %v", err)
		}

		if _, err := f.Seek(0, io.SeekStart); err != nil {
			t.Fatalf("seek temp file: %v", err)
		}

		return f
	default:
		t.Fatalf("unknown non-tty file kind %q", kind)

		return nil
	}
}

// TestResolvePasswordInteractiveNonTTYFile covers the case where input really is
// an *os.File — so the type assertion succeeds — but the descriptor is not a
// terminal. It must still fail with errNoTerminal rather than reading anything.
func TestResolvePasswordInteractiveNonTTYFile(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		kind string
	}{
		{"pipe read end", "pipe"},
		{"regular file", "regular"},
	}
	for _, tst := range tests {
		t.Run(tst.name, func(t *testing.T) {
			t.Parallel()

			in := openNonTTYFile(t, tst.kind)

			var out bytes.Buffer

			got, err := resolvePassword(false, in, &out)
			if !errors.Is(err, errNoTerminal) {
				t.Fatalf("resolvePassword error = %v, want errNoTerminal", err)
			}

			if got != "" {
				t.Errorf("resolvePassword returned %q, want empty string", got)
			}
		})
	}
}

// TestResolvePasswordInteractiveNoTerminalSentinel pins the non-*os.File branch
// to the errNoTerminal sentinel, not just to its message text.
func TestResolvePasswordInteractiveNoTerminalSentinel(t *testing.T) {
	t.Parallel()

	_, err := resolvePassword(false, bytes.NewBufferString("irrelevant"), io.Discard)
	if !errors.Is(err, errNoTerminal) {
		t.Fatalf("resolvePassword error = %v, want errNoTerminal", err)
	}
}

// newPipeReader returns the read end of an os.Pipe: a real *os.File that is
// never a terminal. This mirrors the `echo pw | splitkauf useradd` / CI case.
func newPipeReader(t *testing.T) *os.File {
	t.Helper()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}

	t.Cleanup(func() {
		_ = r.Close()
		_ = w.Close()
	})

	return r
}

// newRegularFile returns a *os.File for a regular file in the test's temp dir.
func newRegularFile(t *testing.T) *os.File {
	t.Helper()

	f, err := os.CreateTemp(t.TempDir(), "terminalfd")
	if err != nil {
		t.Fatalf("os.CreateTemp: %v", err)
	}

	t.Cleanup(func() { _ = f.Close() })

	return f
}

// newClosedFile returns a *os.File that has already been closed, so Fd yields
// an invalid descriptor.
func newClosedFile(t *testing.T) *os.File {
	t.Helper()

	f := newRegularFile(t)
	if err := f.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	return f
}

// TestTerminalFdNonTTY pins the ok=false half of the contract: every reader
// that is not a *os.File, and every *os.File that is not an interactive
// terminal, must yield (0, false) — never a non-zero fd alongside ok=false,
// and never a panic.
func TestTerminalFdNonTTY(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		setup func(t *testing.T) io.Reader
	}{
		{
			// A caller that never set an input reader.
			name: "nil reader",
			setup: func(t *testing.T) io.Reader {
				t.Helper()

				return nil
			},
		},
		{
			name: "bytes buffer",
			setup: func(t *testing.T) io.Reader {
				t.Helper()

				return new(bytes.Buffer)
			},
		},
		{
			name: "strings reader",
			setup: func(t *testing.T) io.Reader {
				t.Helper()

				return strings.NewReader(testPassword)
			},
		},
		{
			// Documents the unwrapping assumption: only the concrete
			// *os.File type is recognised, so a buffered stdin — even on a
			// real terminal — reports ok=false.
			name: "bufio reader wrapping stdin",
			setup: func(t *testing.T) io.Reader {
				t.Helper()

				return bufio.NewReader(os.Stdin)
			},
		},
		{
			// Typed nil: the type assertion succeeds, Fd() on a nil *os.File
			// returns an invalid descriptor. Must not panic.
			name: "typed nil os.File",
			setup: func(t *testing.T) io.Reader {
				t.Helper()

				var f *os.File

				return f
			},
		},
		{
			name: "pipe read end",
			setup: func(t *testing.T) io.Reader {
				t.Helper()

				return newPipeReader(t)
			},
		},
		{
			name: "regular file",
			setup: func(t *testing.T) io.Reader {
				t.Helper()

				return newRegularFile(t)
			},
		},
		{
			name: "dev null",
			setup: func(t *testing.T) io.Reader {
				t.Helper()

				f, err := os.Open(os.DevNull)
				if err != nil {
					t.Skipf("open %s: %v", os.DevNull, err)
				}

				t.Cleanup(func() { _ = f.Close() })

				return f
			},
		},
		{
			name: "closed file",
			setup: func(t *testing.T) io.Reader {
				t.Helper()

				return newClosedFile(t)
			},
		},
	}
	for _, tst := range tests {
		t.Run(tst.name, func(t *testing.T) {
			t.Parallel()

			fd, ok := terminalFd(tst.setup(t))
			if ok {
				t.Errorf("terminalFd(%s) ok = true, want false", tst.name)
			}

			if fd != 0 {
				t.Errorf("terminalFd(%s) fd = %d, want 0", tst.name, fd)
			}
		})
	}
}

// TestTerminalFdTTY covers the true-positive path using the master side of a
// pty, which the kernel reports as a terminal. It skips where /dev/ptmx is
// unavailable (non-Linux, or a sandboxed CI runner), since the repo carries no
// pty helper dependency.
func TestTerminalFdTTY(t *testing.T) {
	t.Parallel()

	ptmx, err := os.OpenFile("/dev/ptmx", os.O_RDWR, 0)
	if err != nil {
		t.Skipf("no pty available: %v", err)
	}

	t.Cleanup(func() { _ = ptmx.Close() })

	want := int(ptmx.Fd())

	fd, ok := terminalFd(ptmx)
	if !ok {
		t.Fatalf("terminalFd(pty) ok = false, want true")
	}

	if fd != want {
		t.Errorf("terminalFd(pty) fd = %d, want %d", fd, want)
	}
}

// embeddedFile satisfies io.Reader and even promotes Fd() from the embedded
// *os.File, but it is not the concrete *os.File type that terminalFd asserts
// on.
type embeddedFile struct {
	*os.File
}

// TestTerminalFdEmbeddedFile pins the no-unwrapping contract one step tighter
// than the bufio case in TestTerminalFdNonTTY: a named struct embedding a
// *os.File that is open on a real terminal still fails the concrete-type
// assertion, so the caller is pushed to --password-stdin instead of having
// echo disabled on the promoted descriptor.
func TestTerminalFdEmbeddedFile(t *testing.T) {
	t.Parallel()

	ptmx, err := os.OpenFile("/dev/ptmx", os.O_RDWR, 0)
	if err != nil {
		t.Skipf("no pty available: %v", err)
	}

	t.Cleanup(func() { _ = ptmx.Close() })

	// Guard: the embedded descriptor must really be a terminal, otherwise this
	// case degrades into a duplicate of the non-TTY table.
	if _, ok := terminalFd(ptmx); !ok {
		t.Skip("pty master is not reported as a terminal")
	}

	fd, ok := terminalFd(embeddedFile{File: ptmx})
	if ok {
		t.Errorf("terminalFd(embedded *os.File) ok = true, want false")
	}

	if fd != 0 {
		t.Errorf("terminalFd(embedded *os.File) fd = %d, want 0", fd)
	}
}

// errUseraddStdin is the sentinel returned by the failing stdin reader used
// to exercise the "reading password from stdin" branch of runUseradd.
var errUseraddStdin = errors.New("stdin exploded")

// newUseraddTestCmd builds a cobra command wired to in-memory streams so
// runUseradd never touches the real terminal, and returns the buffer that
// captures everything written to the command's output.
func newUseraddTestCmd(t *testing.T, in io.Reader) (*cobra.Command, *bytes.Buffer) {
	t.Helper()

	var out bytes.Buffer

	cmd := &cobra.Command{Use: "add"}
	cmd.SetIn(in)
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	return cmd, &out
}

// TestRunUseraddErrors covers every runUseradd failure that is reachable
// without a database: the paths that stop at username validation, at
// password resolution, or at hashing all return before db.NewSQL is called.
// The success path and the Create-error paths need a real database and are
// deliberately not exercised here.
func TestRunUseraddErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		username      string
		passwordStdin bool
		stdin         io.Reader
		displayName   string
		email         string
		wantErr       error  // matched with errors.Is
		wantMsg       string // substring the wrapped message must contain; empty means the sentinel is returned unwrapped
	}{
		{
			name:          "empty username",
			username:      "",
			passwordStdin: true,
			stdin:         strings.NewReader(testPassword),
			wantErr:       users.ErrUsernameEmpty,
		},
		{
			name:          "whitespace only username",
			username:      " \t ",
			passwordStdin: true,
			stdin:         strings.NewReader(testPassword),
			wantErr:       users.ErrUsernameEmpty,
		},
		{
			name:          "username checked before password is read",
			username:      "   ",
			passwordStdin: true,
			stdin:         iotest.ErrReader(errUseraddStdin),
			wantErr:       users.ErrUsernameEmpty,
		},
		{
			name:          "stdin read failure",
			username:      "alice",
			passwordStdin: true,
			stdin:         iotest.ErrReader(errUseraddStdin),
			wantErr:       errUseraddStdin,
			wantMsg:       "reading password from stdin",
		},
		{
			name:          "interactive prompt without a terminal",
			username:      "alice",
			passwordStdin: false,
			stdin:         strings.NewReader(testPassword + "\n" + testPassword + "\n"),
			wantErr:       errNoTerminal,
		},
		{
			name:          "empty stdin password is too short",
			username:      "alice",
			passwordStdin: true,
			stdin:         strings.NewReader(""),
			wantErr:       users.ErrPasswordTooShort,
			wantMsg:       "hashing password",
		},
		{
			name:          "short stdin password",
			username:      "alice",
			passwordStdin: true,
			stdin:         strings.NewReader(strings.Repeat("a", users.MinPasswordLen-1) + "\n"),
			wantErr:       users.ErrPasswordTooShort,
			wantMsg:       "hashing password",
		},
		{
			name:          "over long stdin password",
			username:      "  alice  ",
			passwordStdin: true,
			stdin:         strings.NewReader(strings.Repeat("a", users.MaxPasswordLen+1)),
			displayName:   "Alice Example",
			email:         " alice@example.test ",
			wantErr:       users.ErrPasswordTooLong,
			wantMsg:       "hashing password",
		},
	}

	for _, tst := range tests {
		t.Run(tst.name, func(t *testing.T) {
			t.Parallel()

			cmd, out := newUseraddTestCmd(t, tst.stdin)

			err := runUseradd(cmd, tst.username, tst.passwordStdin, tst.displayName, tst.email)
			if err == nil {
				t.Fatalf("runUseradd(%q, stdin=%t) = nil, want error", tst.username, tst.passwordStdin)
			}

			if !errors.Is(err, tst.wantErr) {
				t.Errorf("runUseradd(%q) error = %v, want errors.Is(..., %v)", tst.username, err, tst.wantErr)
			}

			if tst.wantMsg == "" {
				// Sentinels on these paths are documented as unwrapped.
				if !errors.Is(err, tst.wantErr) {
					t.Errorf("runUseradd(%q) error = %v (%T), want the bare sentinel %v", tst.username, err, err, tst.wantErr)
				}
			} else if !strings.Contains(err.Error(), tst.wantMsg) {
				t.Errorf("runUseradd(%q) error = %q, want it to contain %q", tst.username, err.Error(), tst.wantMsg)
			}

			if got := out.String(); got != "" {
				t.Errorf("runUseradd(%q) wrote %q to the command output, want nothing on the error path", tst.username, got)
			}
		})
	}
}

// TestRunUseraddFailsBeforeDatabase covers every runUseradd failure that is
// decided before the database connection is opened: username validation,
// password resolution and password hashing. The success path and the
// Create-error paths are deliberately not covered here — runUseradd builds its
// connection inline from the global config.C, so they need a real database.
//
// This overlaps TestRunUseraddErrors above but exercises the password-length
// boundary with concrete short passwords (rune-vs-byte counting, CRLF
// trimming, repeated trailing newlines) instead of relative lengths.
func TestRunUseraddFailsBeforeDatabase(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		usernameArg   string
		passwordStdin bool
		stdin         io.Reader
		wantErr       error  // matched with errors.Is
		wantMsg       string // substring of err.Error(), empty to skip
	}{
		{
			name:          "empty username",
			usernameArg:   "",
			passwordStdin: true,
			stdin:         strings.NewReader(testPassword + "\n"),
			wantErr:       users.ErrUsernameEmpty,
		},
		{
			name:          "whitespace-only username",
			usernameArg:   "   ",
			passwordStdin: true,
			stdin:         strings.NewReader(testPassword + "\n"),
			wantErr:       users.ErrUsernameEmpty,
		},
		{
			// The username check runs first, so the (exploding) reader is
			// never touched.
			name:          "blank username short-circuits password read",
			usernameArg:   "\t\n ",
			passwordStdin: true,
			stdin:         iotest.ErrReader(errUseraddStdin),
			wantErr:       users.ErrUsernameEmpty,
		},
		{
			// Not an *os.File backed by a TTY, so the interactive prompt is
			// refused outright.
			name:          "interactive entry without a terminal",
			usernameArg:   "alice",
			passwordStdin: false,
			stdin:         &bytes.Buffer{},
			wantErr:       errNoTerminal,
			wantMsg:       "--password-stdin",
		},
		{
			name:          "stdin read error",
			usernameArg:   "alice",
			passwordStdin: true,
			stdin:         iotest.ErrReader(errUseraddStdin),
			wantErr:       errUseraddStdin,
			wantMsg:       "reading password from stdin:",
		},
		{
			name:          "empty stdin password",
			usernameArg:   "alice",
			passwordStdin: true,
			stdin:         strings.NewReader(""),
			wantErr:       users.ErrPasswordTooShort,
			wantMsg:       "hashing password:",
		},
		{
			name:          "stdin password is only a newline",
			usernameArg:   "alice",
			passwordStdin: true,
			stdin:         strings.NewReader("\n"),
			wantErr:       users.ErrPasswordTooShort,
			wantMsg:       "hashing password:",
		},
		{
			name:          "seven character password",
			usernameArg:   "alice",
			passwordStdin: true,
			stdin:         strings.NewReader("s3cret1\n"),
			wantErr:       users.ErrPasswordTooShort,
			wantMsg:       "hashing password:",
		},
		{
			// Seven runes but eight bytes: the minimum is counted in runes.
			name:          "seven runes but eight bytes",
			usernameArg:   "alice",
			passwordStdin: true,
			stdin:         strings.NewReader("sécret1\n"),
			wantErr:       users.ErrPasswordTooShort,
			wantMsg:       "hashing password:",
		},
		{
			// Pins the CRLF trim: without it the value would be nine runes.
			name:          "seven character password with crlf",
			usernameArg:   "alice",
			passwordStdin: true,
			stdin:         strings.NewReader("s3cret1\r\n"),
			wantErr:       users.ErrPasswordTooShort,
			wantMsg:       "hashing password:",
		},
		{
			// Pins the TrimRight semantics: every trailing newline goes, not
			// just the last one.
			name:          "seven character password with repeated newlines",
			usernameArg:   "alice",
			passwordStdin: true,
			stdin:         strings.NewReader("s3cret1\n\n"),
			wantErr:       users.ErrPasswordTooShort,
			wantMsg:       "hashing password:",
		},
		{
			name:          "seventy-three byte password",
			usernameArg:   "alice",
			passwordStdin: true,
			stdin:         strings.NewReader(strings.Repeat("a", 73) + "\n"),
			wantErr:       users.ErrPasswordTooLong,
			wantMsg:       "hashing password:",
		},
	}
	for _, tst := range tests {
		t.Run(tst.name, func(t *testing.T) {
			t.Parallel()

			cmd, out := newUseraddTestCmd(t, tst.stdin)

			err := runUseradd(cmd, tst.usernameArg, tst.passwordStdin, "", "")
			if err == nil {
				t.Fatalf("runUseradd(%q) = nil, want error %v", tst.usernameArg, tst.wantErr)
			}

			if !errors.Is(err, tst.wantErr) {
				t.Errorf("runUseradd(%q) = %v, want errors.Is(..., %v)", tst.usernameArg, err, tst.wantErr)
			}

			if tst.wantMsg != "" && !strings.Contains(err.Error(), tst.wantMsg) {
				t.Errorf("runUseradd(%q) error = %q, want it to contain %q", tst.usernameArg, err.Error(), tst.wantMsg)
			}

			if got := out.String(); got != "" {
				t.Errorf("runUseradd(%q) wrote %q, want no output on failure", tst.usernameArg, got)
			}
		})
	}
}

// TestRunUseraddUsernameErrorIsUnwrapped pins that the empty-username sentinel
// is returned as-is rather than decorated with extra context.
func TestRunUseraddUsernameErrorIsUnwrapped(t *testing.T) {
	t.Parallel()

	cmd, _ := newUseraddTestCmd(t, strings.NewReader(testPassword+"\n"))

	err := runUseradd(cmd, "  ", true, "Alice", "alice@example.test")
	if !errors.Is(err, users.ErrUsernameEmpty) {
		t.Fatalf("runUseradd = %v, want exactly users.ErrUsernameEmpty", err)
	}
}

// TestRunUseraddNoTerminalErrorIsUnwrapped pins that resolvePassword errors
// travel out of runUseradd without an additional wrap.
func TestRunUseraddNoTerminalErrorIsUnwrapped(t *testing.T) {
	t.Parallel()

	cmd, out := newUseraddTestCmd(t, &bytes.Buffer{})

	err := runUseradd(cmd, "alice", false, "", "")
	if !errors.Is(err, errNoTerminal) {
		t.Fatalf("runUseradd = %v, want exactly errNoTerminal", err)
	}

	if got := out.String(); got != "" {
		t.Errorf("runUseradd wrote %q, want no prompt output without a terminal", got)
	}
}
