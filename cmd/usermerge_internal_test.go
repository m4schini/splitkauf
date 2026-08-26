// SPDX-License-Identifier: CC0-1.0

package cmd

import (
	"bytes"
	"errors"
	"os"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"github.com/google/uuid"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/m4schini/splitkauf/adapters/db"
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

// newMergePlanCmd returns a bare cobra command with both streams redirected to
// buffers, so printMergePlan's stdout and stderr output can be inspected
// separately.
func newMergePlanCmd(t *testing.T) (*cobra.Command, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()

	cmd := &cobra.Command{Use: "merge"}

	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)

	return cmd, &stdout, &stderr
}

// mergePlanUUID parses a fixed UUID for use in printMergePlan test fixtures.
func mergePlanUUID(t *testing.T, s string) uuid.UUID {
	t.Helper()

	id, err := uuid.Parse(s)
	if err != nil {
		t.Fatalf("uuid.Parse(%q) = error %v", s, err)
	}

	return id
}

// countNumberLines reports how many lines of s contain want as a standalone
// whitespace-separated token (ignoring surrounding punctuation). It is used to
// assert that each of the three attribution counts got its own output line
// without depending on the exact label wording.
func countNumberLines(s, want string) int {
	n := 0

	for line := range strings.SplitSeq(s, "\n") {
		for field := range strings.FieldsSeq(line) {
			if strings.Trim(field, ",.:;()[]") == want {
				n++

				break
			}
		}
	}

	return n
}

// cleanupLineCount counts the indented cleanup lines ("will delete"/"will
// seed") in s and fails if any of them is not indented.
func cleanupLineCount(t *testing.T, s string) int {
	t.Helper()

	n := 0

	for line := range strings.SplitSeq(s, "\n") {
		if !strings.Contains(line, "will delete") && !strings.Contains(line, "will seed") {
			continue
		}

		n++

		if line == strings.TrimLeft(line, " \t") {
			t.Errorf("cleanup line is not indented: %q", line)
		}
	}

	return n
}

func assertMergePlanContains(t *testing.T, got string, want ...string) {
	t.Helper()

	for _, w := range want {
		if !strings.Contains(got, w) {
			t.Errorf("stdout does not contain %q\nstdout = %q", w, got)
		}
	}
}

func assertMergePlanNotContains(t *testing.T, stream, got string, unwanted ...string) {
	t.Helper()

	for _, u := range unwanted {
		if strings.Contains(got, u) {
			t.Errorf("%s unexpectedly contains %q\n%s = %q", stream, u, stream, got)
		}
	}
}

// lineContaining returns the first line of out that contains needle, failing
// the test when no line does.
func lineContaining(t *testing.T, out, needle string) string {
	t.Helper()

	for line := range strings.SplitSeq(out, "\n") {
		if strings.Contains(line, needle) {
			return line
		}
	}

	t.Fatalf("no line containing %q in output:\n%s", needle, out)

	return ""
}

func TestPrintMergePlan(t *testing.T) {
	t.Parallel()

	const (
		sourceUUID = "11111111-1111-1111-1111-111111111111"
		targetUUID = "22222222-2222-2222-2222-222222222222"
		nilUUID    = "00000000-0000-0000-0000-000000000000"
	)

	tests := []struct {
		name       string
		args       []string
		sourceKind string
		targetKind string
		sourceID   string
		targetID   string
		listCount  int
		added      int
		bought     int
		check      func(t *testing.T, stdout, stderr string)
	}{
		{
			name:       "local source and local target",
			args:       []string{"alice", "bob"},
			sourceKind: db.IdentityKindLocal,
			targetKind: db.IdentityKindLocal,
			sourceID:   "alice",
			targetID:   "bob",
			listCount:  7,
			added:      11,
			bought:     13,
			check: func(t *testing.T, stdout, stderr string) {
				t.Helper()

				assertMergePlanContains(t, stdout,
					"Merge plan:",
					"alice",
					"bob",
					sourceUUID,
					targetUUID,
					`"Alice Source"`,
					`"Bob Target"`,
					"alice@example.test",
					"bob@example.test",
				)

				for _, n := range []string{"7", "11", "13"} {
					if got := countNumberLines(stdout, n); got != 1 {
						t.Errorf("count %s appears on %d lines, want 1\nstdout = %q", n, got, stdout)
					}
				}

				// local source -> delete line naming the local account,
				// local target -> seed line: two cleanup lines total.
				assertMergePlanContains(t, stdout, "will delete", "will seed")

				if got := cleanupLineCount(t, stdout); got != 2 {
					t.Errorf("cleanup lines = %d, want 2\nstdout = %q", got, stdout)
				}

				if stderr != "" {
					t.Errorf("stderr = %q, want empty for a local source", stderr)
				}
			},
		},
		{
			name:       "oidc source warns and seeds local target",
			args:       []string{"oidc:sub-123", "bob"},
			sourceKind: "oidc",
			targetKind: db.IdentityKindLocal,
			sourceID:   "sub-123",
			targetID:   "bob",
			listCount:  1,
			added:      2,
			bought:     3,
			check: func(t *testing.T, stdout, stderr string) {
				t.Helper()

				assertMergePlanContains(t, stdout, "Merge plan:", "oidc:sub-123", "bob")

				// Non-local source: plain members-row cleanup, no local account
				// deletion and no "(if any)" hedge.
				assertMergePlanNotContains(t, "stdout", stdout, "local account", "(if any)")
				assertMergePlanContains(t, stdout, "will delete", "will seed")

				if got := cleanupLineCount(t, stdout); got != 2 {
					t.Errorf("cleanup lines = %d, want 2\nstdout = %q", got, stdout)
				}

				if strings.TrimSpace(stderr) == "" {
					t.Fatalf("stderr is empty, want the IdP re-login warning for a non-local source")
				}

				// The warning must not leak into stdout.
				assertMergePlanNotContains(t, "stdout", stdout, strings.TrimSpace(stderr))
			},
		},
		{
			name:       "unknown source and non-local target",
			args:       []string{"ghost", "oidc:sub-999"},
			sourceKind: db.IdentityKindUnknown,
			targetKind: "oidc",
			sourceID:   "ghost",
			targetID:   "sub-999",
			listCount:  4,
			added:      5,
			bought:     6,
			check: func(t *testing.T, stdout, stderr string) {
				t.Helper()

				assertMergePlanContains(t, stdout, "Merge plan:", "ghost", "oidc:sub-999", "(if any)")
				assertMergePlanNotContains(t, "stdout", stdout, "will seed", "local account")

				if got := cleanupLineCount(t, stdout); got != 1 {
					t.Errorf("cleanup lines = %d, want 1\nstdout = %q", got, stdout)
				}

				if strings.TrimSpace(stderr) == "" {
					t.Errorf("stderr is empty, want the IdP re-login warning for kind %q", db.IdentityKindUnknown)
				}
			},
		},
		{
			name:       "dev source warns",
			args:       []string{"dev", "bob"},
			sourceKind: "dev",
			targetKind: db.IdentityKindLocal,
			sourceID:   "dev",
			targetID:   "bob",
			listCount:  0,
			added:      0,
			bought:     0,
			check: func(t *testing.T, stdout, stderr string) {
				t.Helper()

				if strings.TrimSpace(stderr) == "" {
					t.Errorf("stderr is empty, want the IdP re-login warning for kind \"dev\"")
				}

				// Zero counts still get their own three lines.
				if got := countNumberLines(stdout, "0"); got != 3 {
					t.Errorf("lines carrying the count 0 = %d, want 3\nstdout = %q", got, stdout)
				}
			},
		},
		{
			name:       "negative counts are printed verbatim",
			args:       []string{"alice", "bob"},
			sourceKind: db.IdentityKindLocal,
			targetKind: db.IdentityKindLocal,
			sourceID:   "alice",
			targetID:   "bob",
			listCount:  -1,
			added:      -2,
			bought:     -3,
			check: func(t *testing.T, stdout, stderr string) {
				t.Helper()

				for _, n := range []string{"-1", "-2", "-3"} {
					if got := countNumberLines(stdout, n); got != 1 {
						t.Errorf("count %s appears on %d lines, want 1\nstdout = %q", n, got, stdout)
					}
				}

				if stderr != "" {
					t.Errorf("stderr = %q, want empty for a local source", stderr)
				}
			},
		},
		{
			name:       "selectors with percent signs and spaces are echoed verbatim",
			args:       []string{"100% sure", "user %s %d"},
			sourceKind: db.IdentityKindLocal,
			targetKind: db.IdentityKindLocal,
			sourceID:   "100% sure",
			targetID:   "user %s %d",
			listCount:  1,
			added:      1,
			bought:     1,
			check: func(t *testing.T, stdout, stderr string) {
				t.Helper()

				assertMergePlanContains(t, stdout, "100% sure", "user %s %d")
				assertMergePlanNotContains(t, "stdout", stdout, "%!", "MISSING", "EXTRA")

				if stderr != "" {
					t.Errorf("stderr = %q, want empty for a local source", stderr)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cmd, stdout, stderr := newMergePlanCmd(t)

			source := db.Identity{
				Kind:       tt.sourceKind,
				Identifier: tt.sourceID,
				UserID:     mergePlanUUID(t, sourceUUID),
				Name:       "Alice Source",
				Email:      "alice@example.test",
			}
			target := db.Identity{
				Kind:       tt.targetKind,
				Identifier: tt.targetID,
				UserID:     mergePlanUUID(t, targetUUID),
				Name:       "Bob Target",
				Email:      "bob@example.test",
			}

			printMergePlan(cmd, tt.args, source, target, tt.listCount, tt.added, tt.bought)

			// The plan body never belongs on stderr.
			assertMergePlanNotContains(t, "stderr", stderr.String(), "Merge plan:")

			tt.check(t, stdout.String(), stderr.String())
		})
	}
}

func TestPrintMergePlanEmptyEmailRendersDash(t *testing.T) {
	t.Parallel()

	cmd, stdout, stderr := newMergePlanCmd(t)

	source := db.Identity{
		Kind:       db.IdentityKindLocal,
		Identifier: "alice",
		UserID:     mergePlanUUID(t, "11111111-1111-1111-1111-111111111111"),
		Name:       "Alice",
	}
	target := db.Identity{
		Kind:       db.IdentityKindLocal,
		Identifier: "bob",
		UserID:     mergePlanUUID(t, "22222222-2222-2222-2222-222222222222"),
		Name:       "",
	}

	printMergePlan(cmd, []string{"alice", "bob"}, source, target, 0, 0, 0)

	out := stdout.String()

	// Both emails are empty, so orDash substitutes an em dash on both lines.
	if got := strings.Count(out, "—"); got != 2 {
		t.Errorf("em dashes = %d, want 2 (one per empty email)\nstdout = %q", got, out)
	}

	// An empty Name is quoted, not dash-substituted.
	assertMergePlanContains(t, out, `""`)

	if stderr.String() != "" {
		t.Errorf("stderr = %q, want empty for a local source", stderr.String())
	}
}

func TestPrintMergePlanZeroValueIdentities(t *testing.T) {
	t.Parallel()

	cmd, stdout, stderr := newMergePlanCmd(t)

	printMergePlan(cmd, []string{"src", "dst"}, db.Identity{}, db.Identity{}, 0, 0, 0)

	out := stdout.String()

	assertMergePlanContains(t, out,
		"Merge plan:",
		"src",
		"dst",
		"00000000-0000-0000-0000-000000000000",
		`""`,
		"—",
	)

	// Zero-value target Kind is not local, so nothing is seeded; zero-value
	// source Kind is not local either, so the plain members-row line is used.
	assertMergePlanNotContains(t, "stdout", out, "will seed", "local account")

	if got := cleanupLineCount(t, out); got != 1 {
		t.Errorf("cleanup lines = %d, want 1\nstdout = %q", got, out)
	}

	// The zero-value Kind "" is non-local, so the warning is emitted.
	if strings.TrimSpace(stderr.String()) == "" {
		t.Errorf("stderr is empty, want the IdP re-login warning for the zero-value Kind")
	}
}

func TestPrintMergePlanEchoesSelectorsNotIdentifiers(t *testing.T) {
	t.Parallel()

	cmd, stdout, _ := newMergePlanCmd(t)

	source := db.Identity{
		Kind:       db.IdentityKindLocal,
		Identifier: "alice",
		UserID:     uuid.New(),
		Name:       "Alice",
		Email:      "alice@example.test",
	}
	target := db.Identity{
		Kind:       "oidc",
		Identifier: "target-sub",
		UserID:     uuid.New(),
		Name:       "Bob",
		Email:      "bob@example.test",
	}

	// The selectors deliberately differ from the resolved identifiers.
	printMergePlan(cmd, []string{"email:alice@example.test", "sub:target-sub"}, source, target, 0, 0, 0)

	out := stdout.String()

	sourceLine := lineContaining(t, out, "source: ")
	if !strings.Contains(sourceLine, "email:alice@example.test") {
		t.Errorf("source line %q does not echo the raw selector verbatim", sourceLine)
	}

	targetLine := lineContaining(t, out, "target: ")
	if !strings.Contains(targetLine, "sub:target-sub") {
		t.Errorf("target line %q does not echo the raw selector verbatim", targetLine)
	}
}

// errUsermergeTestWriteFailed is what errWriter reports on every write.
var errUsermergeTestWriteFailed = errors.New("write failed")

// errWriter fails every write, standing in for a broken output stream.
type errWriter struct{}

func (errWriter) Write([]byte) (int, error) {
	return 0, errUsermergeTestWriteFailed
}

func TestPrintMergePlanIgnoresWriteErrors(t *testing.T) {
	t.Parallel()

	cmd := &cobra.Command{Use: "merge"}
	cmd.SetOut(errWriter{})
	cmd.SetErr(errWriter{})

	source := db.Identity{Kind: "oidc", Identifier: "source-sub", UserID: uuid.New(), Name: "Alice"}
	target := db.Identity{Kind: db.IdentityKindLocal, Identifier: "bob", UserID: uuid.New(), Name: "Bob"}

	// All Fprint results are discarded, so a failing writer must not panic.
	printMergePlan(cmd, []string{"sub:source-sub", "bob"}, source, target, 1, 2, 3)
}

func TestPrintMergePlanPanicsWithFewerThanTwoArgs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
	}{
		{name: "nil args", args: nil},
		{name: "no args", args: []string{}},
		{name: "one arg", args: []string{"alice"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cmd, _, _ := newMergePlanCmd(t)

			defer func() {
				if r := recover(); r == nil {
					t.Errorf("printMergePlan(%v) did not panic, want an index out of range panic", tt.args)
				}
			}()

			printMergePlan(cmd, tt.args, db.Identity{}, db.Identity{}, 0, 0, 0)
		})
	}
}

func TestCleanupLines(t *testing.T) {
	t.Parallel()

	lastLogin := time.Date(2024, time.March, 1, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name   string
		source db.Identity
		target db.Identity
		want   []string
	}{
		{
			name:   "local source, oidc target",
			source: db.Identity{Kind: db.IdentityKindLocal, Identifier: "alice"},
			target: db.Identity{Kind: "oidc", Identifier: "sub-123"},
			want: []string{
				`will delete: local account "alice", members row of source`,
			},
		},
		{
			name:   "unknown source, oidc target",
			source: db.Identity{Kind: db.IdentityKindUnknown, Identifier: "sub-gone"},
			target: db.Identity{Kind: "oidc", Identifier: "sub-123"},
			want: []string{
				"will delete: members row of source (if any)",
			},
		},
		{
			name:   "oidc source, oidc target",
			source: db.Identity{Kind: "oidc", Identifier: "sub-abc"},
			target: db.Identity{Kind: "oidc", Identifier: "sub-123"},
			want: []string{
				"will delete: members row of source",
			},
		},
		{
			name:   "dev source falls into default branch",
			source: db.Identity{Kind: "dev", Identifier: "dev"},
			target: db.Identity{Kind: "dev", Identifier: "dev"},
			want: []string{
				"will delete: members row of source",
			},
		},
		{
			name:   "zero-value source and target yield one generic line",
			source: db.Identity{},
			target: db.Identity{},
			want: []string{
				"will delete: members row of source",
			},
		},
		{
			name:   "local target appends seed line",
			source: db.Identity{Kind: "oidc", Identifier: "sub-abc"},
			target: db.Identity{Kind: db.IdentityKindLocal, Identifier: "bob"},
			want: []string{
				"will delete: members row of source",
				`will seed: members row for target "bob" (so display names resolve)`,
			},
		},
		{
			name:   "both local yields delete line before seed line",
			source: db.Identity{Kind: db.IdentityKindLocal, Identifier: "alice"},
			target: db.Identity{Kind: db.IdentityKindLocal, Identifier: "bob"},
			want: []string{
				`will delete: local account "alice", members row of source`,
				`will seed: members row for target "bob" (so display names resolve)`,
			},
		},
		{
			name:   "unknown source with local target",
			source: db.Identity{Kind: db.IdentityKindUnknown},
			target: db.Identity{Kind: db.IdentityKindLocal, Identifier: "bob"},
			want: []string{
				"will delete: members row of source (if any)",
				`will seed: members row for target "bob" (so display names resolve)`,
			},
		},
		{
			name:   "local identifiers are quoted with %q escaping",
			source: db.Identity{Kind: db.IdentityKindLocal, Identifier: `a"b`},
			target: db.Identity{Kind: db.IdentityKindLocal, Identifier: "user with spaces und Ümläute"},
			want: []string{
				`will delete: local account "a\"b", members row of source`,
				`will seed: members row for target "user with spaces und Ümläute" (so display names resolve)`,
			},
		},
		{
			name:   "empty identifiers render as empty quoted strings",
			source: db.Identity{Kind: db.IdentityKindLocal, Identifier: ""},
			target: db.Identity{Kind: db.IdentityKindLocal, Identifier: ""},
			want: []string{
				`will delete: local account "", members row of source`,
				`will seed: members row for target "" (so display names resolve)`,
			},
		},
		{
			name: "other identity fields are ignored",
			source: db.Identity{
				Kind:       db.IdentityKindLocal,
				Identifier: "alice",
				UserID:     uuid.MustParse("11111111-1111-1111-1111-111111111111"),
				Name:       "Alice Source",
				Email:      "alice@example.test",
				LastLogin:  &lastLogin,
			},
			target: db.Identity{
				Kind:       "oidc",
				Identifier: "sub-123",
				UserID:     uuid.MustParse("22222222-2222-2222-2222-222222222222"),
				Name:       "Bob Target",
				Email:      "bob@example.test",
				LastLogin:  &lastLogin,
			},
			want: []string{
				`will delete: local account "alice", members row of source`,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := cleanupLines(tt.source, tt.target)
			if got == nil {
				t.Fatal("cleanupLines() = nil, want non-nil slice")
			}

			if !slices.Equal(got, tt.want) {
				t.Errorf("cleanupLines() =\n%#v\nwant\n%#v", got, tt.want)
			}
		})
	}
}

// Linux pty ioctls, spelled out here so the fixture does not depend on
// platform-specific syscall constants: unlock the slave, then fetch its number.
const (
	ioctlSetPTLock = 0x40045431 // TIOCSPTLCK
	ioctlGetPTN    = 0x80045430 // TIOCGPTN
)

// ioctl issues a terminal ioctl; there is no stdlib wrapper for the two pty
// requests below, so they go through syscall directly.
func ioctl(file *os.File, request uintptr, arg unsafe.Pointer) error {
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, file.Fd(), request, uintptr(arg)); errno != 0 {
		return errno
	}

	return nil
}

// newPTY opens a fresh pseudo-terminal and returns its master and slave ends.
// The slave is a real terminal, so it satisfies confirmMerge's term.IsTerminal
// guard — something no in-memory reader can do, which is why every interactive
// branch of confirmMerge needs this fixture to be reachable at all.
func newPTY(t *testing.T, slaveFlags int) (*os.File, *os.File) {
	t.Helper()

	if runtime.GOOS != "linux" {
		t.Skip("pty fixture is Linux-specific")
	}

	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR, 0)
	if err != nil {
		t.Skipf("open /dev/ptmx: %v", err)
	}

	t.Cleanup(func() { _ = master.Close() })

	var unlock int32
	if err := ioctl(master, ioctlSetPTLock, unsafe.Pointer(&unlock)); err != nil {
		t.Skipf("ioctl TIOCSPTLCK: %v", err)
	}

	var ptn uint32
	if err := ioctl(master, ioctlGetPTN, unsafe.Pointer(&ptn)); err != nil {
		t.Skipf("ioctl TIOCGPTN: %v", err)
	}

	name := "/dev/pts/" + strconv.FormatUint(uint64(ptn), 10)

	slave, err := os.OpenFile(name, slaveFlags|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Skipf("open %s: %v", name, err)
	}

	t.Cleanup(func() { _ = slave.Close() })

	return master, slave
}

// newTerminalInput returns a terminal file preloaded with input, ready to be
// handed to confirmMerge as stdin. In raw mode the bytes arrive verbatim (so a
// "\r\n" ending survives the line discipline); otherwise the terminal stays
// canonical, where a 0x04 (Ctrl-D) byte ends a read the way a real operator's
// EOF would — 0x04 mid-line yields the partial line, 0x04 alone yields EOF.
func newTerminalInput(t *testing.T, input string, raw bool) *os.File {
	t.Helper()

	master, slave := newPTY(t, os.O_RDWR)

	if raw {
		if _, err := term.MakeRaw(int(slave.Fd())); err != nil {
			t.Skipf("term.MakeRaw: %v", err)
		}
	}

	if _, err := master.WriteString(input); err != nil {
		t.Fatalf("write %q to pty master: %v", input, err)
	}

	return slave
}

func TestConfirmMergeYesIgnoresNilReaderAndWriter(t *testing.T) {
	t.Parallel()

	// The --yes check must short-circuit before the reader or writer is touched.
	confirmed, err := confirmMerge(true, nil, nil)
	if err != nil || !confirmed {
		t.Errorf("confirmMerge(--yes, nil, nil) = %v, %v; want true, nil", confirmed, err)
	}
}

func TestConfirmMergeNonTerminalFileWithoutYesFails(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input func(t *testing.T) *os.File
	}{
		{
			name: "pipe read end",
			input: func(t *testing.T) *os.File {
				t.Helper()

				reader, writer, err := os.Pipe()
				if err != nil {
					t.Fatalf("os.Pipe(): %v", err)
				}

				t.Cleanup(func() { _ = reader.Close() })

				if _, err := writer.WriteString("y\n"); err != nil {
					t.Fatalf("write to pipe: %v", err)
				}

				if err := writer.Close(); err != nil {
					t.Fatalf("close pipe writer: %v", err)
				}

				return reader
			},
		},
		{
			name: "regular file",
			input: func(t *testing.T) *os.File {
				t.Helper()

				path := t.TempDir() + "/stdin"
				if err := os.WriteFile(path, []byte("y\n"), 0o600); err != nil {
					t.Fatalf("write temp file: %v", err)
				}

				file, err := os.Open(path)
				if err != nil {
					t.Fatalf("open temp file: %v", err)
				}

				t.Cleanup(func() { _ = file.Close() })

				return file
			},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var out strings.Builder

			confirmed, err := confirmMerge(false, testCase.input(t), &out)
			if confirmed {
				t.Errorf("confirmMerge(non-TTY *os.File) = true; want false")
			}

			if !errors.Is(err, errConfirmationRequired) {
				t.Errorf("error = %v; want errConfirmationRequired", err)
			}

			if out.Len() != 0 {
				t.Errorf("confirmMerge(non-TTY *os.File) printed %q, want no prompt", out.String())
			}
		})
	}
}

func TestConfirmMergeInteractiveAnswers(t *testing.T) {
	t.Parallel()

	const wantPrompt = "Proceed? [y/N]: "

	tests := []struct {
		name  string
		input string
		raw   bool
		want  bool
	}{
		{name: "lowercase y accepts", input: "y\n", raw: false, want: true},
		{name: "uppercase Y accepts", input: "Y\n", raw: false, want: true},
		{name: "surrounding whitespace trimmed", input: "  y  \n", raw: false, want: true},
		{name: "crlf line ending trimmed", input: "y\r\n", raw: true, want: true},
		{name: "partial line before eof accepts", input: "y\x04\x04", raw: false, want: true},
		{name: "n declines", input: "n\n", raw: false, want: false},
		{name: "uppercase N declines", input: "N\n", raw: false, want: false},
		{name: "empty line declines", input: "\n", raw: false, want: false},
		{name: "spelled out yes declines", input: "yes\n", raw: false, want: false},
		{name: "immediate eof declines", input: "\x04", raw: false, want: false},
		// A fresh bufio.Reader is built per call, so the buffered remainder is
		// discarded rather than treated as an error; the caller only ever asks once.
		{name: "only the first line is consumed", input: "y\nextra\n", raw: false, want: true},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var out strings.Builder

			terminal := newTerminalInput(t, testCase.input, testCase.raw)

			// A decline is (false, nil), never an error: runUsermerge tells the
			// two apart to print "Aborted" instead of failing the command.
			confirmed, err := confirmMerge(false, terminal, &out)
			if err != nil {
				t.Fatalf("confirmMerge(%q) returned error %v; want nil", testCase.input, err)
			}

			if confirmed != testCase.want {
				t.Errorf("confirmMerge(%q) = %v; want %v", testCase.input, confirmed, testCase.want)
			}

			if got := out.String(); got != wantPrompt {
				t.Errorf("prompt = %q; want %q", got, wantPrompt)
			}
		})
	}
}

func TestConfirmMergeReadErrorIsWrapped(t *testing.T) {
	t.Parallel()

	// A write-only pty slave is still a terminal, so the guard passes, but
	// reading it fails — the "terminal went away mid-prompt" case.
	_, slave := newPTY(t, os.O_WRONLY)

	var out strings.Builder

	confirmed, err := confirmMerge(false, slave, &out)
	if confirmed {
		t.Errorf("confirmMerge(unreadable terminal) = true; want false")
	}

	if err == nil {
		t.Fatalf("confirmMerge(unreadable terminal) returned nil error; want the read failure")
	}

	if !strings.Contains(err.Error(), "reading confirmation: ") {
		t.Errorf("error %q should be wrapped with %q", err, "reading confirmation: ")
	}

	if errors.Is(err, errConfirmationRequired) {
		t.Errorf("error = %v; want the read failure, not errConfirmationRequired", err)
	}
}
