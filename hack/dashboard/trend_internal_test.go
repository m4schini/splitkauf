// SPDX-License-Identifier: CC0-1.0

package main

import "testing"

// Fixture values shared across the trend tests below.
const (
	testDateNew     = "2026-08-21"
	testDateOld     = "2026-08-20"
	testCommitNew   = "abc1234"
	testCommitOld   = "fff0000"
	testCoveragePct = "61.4%"
	testFillerDate  = "any-date"
)

func TestExtractTrend(t *testing.T) {
	t.Parallel()

	body := "# Dashboard\n\n" +
		"## Trend\n" +
		trendStartMarker + "\n" +
		"| Date | Commit | Coverage | Lint debt | Tests |\n" +
		"|------|--------|----------|-----------|-------|\n" +
		"| " + testDateNew + " | `" + testCommitNew + "` | " + testCoveragePct + " | 547 | 214 |\n" +
		"| " + testDateOld + " | `" + testCommitOld + "` | 60.0% | 550 | 210 |\n" +
		trendEndMarker + "\n"

	rows := extractTrend(body)

	want := []TrendRow{
		{Date: testDateNew, Commit: testCommitNew, Coverage: testCoveragePct, LintDebt: "547", Tests: "214"},
		{Date: testDateOld, Commit: testCommitOld, Coverage: "60.0%", LintDebt: "550", Tests: "210"},
	}

	if len(rows) != len(want) {
		t.Fatalf("len(rows) = %d, want %d", len(rows), len(want))
	}

	for i, w := range want {
		if rows[i] != w {
			t.Errorf("rows[%d] = %+v, want %+v", i, rows[i], w)
		}
	}
}

func TestExtractTrendNoMarkers(t *testing.T) {
	t.Parallel()

	if rows := extractTrend("no trend here"); rows != nil {
		t.Errorf("extractTrend() = %+v, want nil", rows)
	}
}

func TestExtractTrendSkipsMalformedRows(t *testing.T) {
	t.Parallel()

	body := trendStartMarker + "\n" +
		"| Date | Commit | Coverage | Lint debt | Tests |\n" +
		"|------|--------|----------|-----------|-------|\n" +
		"a human typed something here\n" +
		"| " + testDateNew + " | `" + testCommitNew + "` | " + testCoveragePct + " | 547 | 214 |\n" +
		trendEndMarker + "\n"

	rows := extractTrend(body)

	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1", len(rows))
	}

	if rows[0].Commit != testCommitNew {
		t.Errorf("rows[0].Commit = %q, want %q", rows[0].Commit, testCommitNew)
	}
}

func TestMergeTrendPrepends(t *testing.T) {
	t.Parallel()

	existing := []TrendRow{
		{Date: testDateOld, Commit: testCommitOld, Coverage: "", LintDebt: "", Tests: ""},
	}
	newRow := TrendRow{Date: testDateNew, Commit: testCommitNew, Coverage: "", LintDebt: "", Tests: ""}

	merged := mergeTrend(existing, newRow)

	if len(merged) != 2 {
		t.Fatalf("len(merged) = %d, want 2", len(merged))
	}

	if merged[0].Commit != testCommitNew || merged[1].Commit != testCommitOld {
		t.Errorf("merged = %+v, want new row first", merged)
	}
}

func TestMergeTrendDedupesByCommit(t *testing.T) {
	t.Parallel()

	existing := []TrendRow{
		{Date: testDateNew, Commit: testCommitNew, Coverage: testCoveragePct, LintDebt: "", Tests: ""},
	}
	newRow := TrendRow{Date: testDateNew, Commit: testCommitNew, Coverage: "99.9%", LintDebt: "", Tests: ""}

	merged := mergeTrend(existing, newRow)

	if len(merged) != 1 {
		t.Fatalf("len(merged) = %d, want 1 (deduped)", len(merged))
	}

	if merged[0].Coverage != testCoveragePct {
		t.Errorf("merged[0].Coverage = %q, want the existing row unchanged", merged[0].Coverage)
	}
}

func TestMergeTrendTrimsTo20(t *testing.T) {
	t.Parallel()

	existing := make([]TrendRow, maxTrendRows)
	for i := range existing {
		existing[i] = TrendRow{Commit: hexCommit(i), Date: testFillerDate, Coverage: "", LintDebt: "", Tests: ""}
	}

	newRow := TrendRow{Commit: testCommitNew, Date: testFillerDate, Coverage: "", LintDebt: "", Tests: ""}

	merged := mergeTrend(existing, newRow)

	if len(merged) != maxTrendRows {
		t.Fatalf("len(merged) = %d, want %d", len(merged), maxTrendRows)
	}

	if merged[0].Commit != testCommitNew {
		t.Errorf("merged[0].Commit = %q, want %q", merged[0].Commit, testCommitNew)
	}
}

func TestMergeTrendTrimsEvenWhenDeduping(t *testing.T) {
	t.Parallel()

	// A hand-edited body can exceed maxTrendRows; a dedup hit (the skip path)
	// must still enforce the cap, not just the prepend path.
	existing := make([]TrendRow, maxTrendRows+1)
	for i := range existing {
		existing[i] = TrendRow{Commit: hexCommit(i), Date: testFillerDate, Coverage: "", LintDebt: "", Tests: ""}
	}

	newRow := TrendRow{Commit: existing[0].Commit, Date: testFillerDate, Coverage: "", LintDebt: "", Tests: ""}

	merged := mergeTrend(existing, newRow)

	if len(merged) != maxTrendRows {
		t.Fatalf("len(merged) = %d, want %d", len(merged), maxTrendRows)
	}
}

// hexCommit returns a distinct 7-hex-digit commit for index i (0-255), so the
// trim-to-20 tests can build maxTrendRows(+) distinct fixture rows.
func hexCommit(i int) string {
	const hexDigits = "0123456789abcdef"

	return "a" + string(hexDigits[(i/len(hexDigits))%len(hexDigits)]) + string(hexDigits[i%len(hexDigits)]) + "0000"
}
