// SPDX-License-Identifier: CC0-1.0

package cmd

import (
	"strings"
	"testing"
	"time"
)

// emDash is the U+2014 placeholder orDash substitutes for an empty value.
const emDash = "—"

func TestOrDash(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty becomes em dash", "", emDash},
		{"plain value kept", "alice", "alice"},
		{"email kept", "alice@example.com", "alice@example.com"},
		{"single space not treated as empty", " ", " "},
		{"tab not treated as empty", "\t", "\t"},
		{"whitespace run not trimmed", "  \t ", "  \t "},
		{"newline kept", "\n", "\n"},
		{"em dash input passes through", emDash, emDash},
		{"ascii hyphen kept", "-", "-"},
		{"multibyte value kept", "Ünïcødé Nàme", "Ünïcødé Nàme"},
		{"emoji value kept", "🙂", "🙂"},
		{"zero width space kept", "\u200b", "\u200b"},
	}
	for _, tst := range tests {
		t.Run(tst.name, func(t *testing.T) {
			t.Parallel()

			got := orDash(tst.in)
			if got != tst.want {
				t.Errorf("orDash(%q) = %q, want %q", tst.in, got, tst.want)
			}
		})
	}
}

func TestOrDashEmptyReturnsEmDashRune(t *testing.T) {
	t.Parallel()

	got := orDash("")

	wantRunes := []rune(got)
	if len(wantRunes) != 1 || wantRunes[0] != '—' {
		t.Fatalf("orDash(%q) = %q (% x), want the single rune U+2014", "", got, []byte(got))
	}

	if len(got) != 3 {
		t.Errorf("orDash(%q) = %q, want the 3-byte UTF-8 em dash, got %d bytes", "", got, len(got))
	}

	if got == "-" {
		t.Errorf("orDash(%q) returned an ASCII hyphen, want the em dash %q", "", emDash)
	}
}

func TestOrDashEmptyIsEmDashRune(t *testing.T) {
	t.Parallel()

	got := orDash("")

	if got == "-" {
		t.Fatalf("orDash(%q) = ASCII hyphen, want the em dash U+2014", "")
	}

	runes := []rune(got)
	if len(runes) != 1 || runes[0] != '—' {
		t.Fatalf("orDash(%q) = %q (% x), want exactly one rune U+2014", "", got, []byte(got))
	}

	if len(got) != 3 {
		t.Errorf("orDash(%q) = %q with %d bytes, want the 3-byte UTF-8 em dash", "", got, len(got))
	}
}

func TestOrDashIsPure(t *testing.T) {
	t.Parallel()

	const in = "Alice"

	first := orDash(in)

	second := orDash(in)
	if first != second {
		t.Errorf("orDash(%q) = %q then %q, want the same result on repeated calls", in, first, second)
	}

	if in != "Alice" {
		t.Errorf("orDash mutated its argument: %q, want %q", in, "Alice")
	}

	if got := orDash(""); got != emDash {
		t.Errorf("orDash(%q) = %q after other calls, want %q", "", got, emDash)
	}
}

func TestFormatLastLogin(t *testing.T) {
	t.Parallel()

	// A fixed non-UTC zone, so the "own location" behaviour is pinned to an
	// offset instead of whatever TZ the test machine happens to run in.
	plusTwo := time.FixedZone("UTC+2", 2*60*60)

	tests := []struct {
		name string
		in   *time.Time
		want string
	}{
		{
			name: "nil renders never",
			in:   nil,
			want: "never",
		},
		{
			name: "zero time is not never",
			in:   new(time.Time{}),
			want: "0001-01-01 00:00",
		},
		{
			name: "minute precision in utc",
			in:   new(time.Date(2026, time.August, 22, 14, 7, 0, 0, time.UTC)),
			want: "2026-08-22 14:07",
		},
		{
			name: "seconds and nanoseconds truncated",
			in:   new(time.Date(2026, time.August, 22, 15, 4, 59, 999999999, time.UTC)),
			want: "2026-08-22 15:04",
		},
		{
			name: "single digit fields zero padded",
			in:   new(time.Date(2026, time.January, 2, 3, 4, 0, 0, time.UTC)),
			want: "2026-01-02 03:04",
		},
		{
			name: "formatted in its own zone without a zone suffix",
			in:   new(time.Date(2026, time.August, 22, 14, 7, 0, 0, time.UTC).In(plusTwo)),
			want: "2026-08-22 16:07",
		},
		{
			name: "zone shift can cross the date boundary",
			in:   new(time.Date(2026, time.August, 22, 23, 30, 0, 0, time.UTC).In(plusTwo)),
			want: "2026-08-23 01:30",
		},
	}
	for _, tst := range tests {
		t.Run(tst.name, func(t *testing.T) {
			t.Parallel()

			got := formatLastLogin(tst.in)
			if got != tst.want {
				t.Errorf("formatLastLogin(%v) = %q, want %q", tst.in, got, tst.want)
			}
		})
	}
}

// lastLoginLayout mirrors the timestamp layout formatLastLogin is
// documented to render (minute precision, no seconds, no zone).
const lastLoginLayout = "2006-01-02 15:04"

// TestFormatLastLoginMatchesLayout guards the documented layout itself:
// a non-nil timestamp must render exactly as time.Format with the
// "2006-01-02 15:04" layout in the value's own location.
func TestFormatLastLoginMatchesLayout(t *testing.T) {
	t.Parallel()

	ts := time.Date(2023, time.July, 9, 18, 45, 30, 0, time.FixedZone("UTC+5:30", 5*60*60+30*60))

	got := formatLastLogin(&ts)
	want := ts.Format(lastLoginLayout)

	if got != want {
		t.Errorf("formatLastLogin(%v) = %q, want %q", ts, got, want)
	}

	if strings.Contains(got, "Z") || strings.Contains(got, "+") {
		t.Errorf("formatLastLogin(%v) = %q, want no timezone indicator", ts, got)
	}
}
