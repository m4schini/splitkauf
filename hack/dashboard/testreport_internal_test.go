// SPDX-License-Identifier: CC0-1.0

package main

import (
	"os"
	"strings"
	"testing"
)

func TestParseTestJSON(t *testing.T) {
	t.Parallel()

	file, err := os.Open("testdata/test-report.jsonl")
	if err != nil {
		t.Fatalf("opening fixture: %v", err)
	}
	defer file.Close() //nolint:errcheck // read-only test fixture

	counts, err := parseTestJSON(file)
	if err != nil {
		t.Fatalf("parseTestJSON() error = %v", err)
	}

	want := TestCounts{Pass: 1, Skip: 1, Fail: 1}
	if counts != want {
		t.Errorf("parseTestJSON() = %+v, want %+v", counts, want)
	}
}

func TestParseTestJSONIgnoresEmptyTestField(t *testing.T) {
	t.Parallel()

	input := `{"Action":"pass","Package":"p"}` + "\n"

	counts, err := parseTestJSON(strings.NewReader(input))
	if err != nil {
		t.Fatalf("parseTestJSON() error = %v", err)
	}

	if counts != (TestCounts{Pass: 0, Skip: 0, Fail: 0}) {
		t.Errorf("parseTestJSON() = %+v, want zero value", counts)
	}
}

// TestParseTestJSONSkipsMalformedLine covers a job killed mid-write: the
// tee'd JSON stream can end in a truncated line. That line is skipped, not
// treated as a fatal error — the counts up to that point still render.
func TestParseTestJSONSkipsMalformedLine(t *testing.T) {
	t.Parallel()

	input := `{"Action":"pass","Package":"p","Test":"TestA"}` + "\n" +
		`{"Action":"pass","Package":"p","Test":"TestB"` // truncated, no closing brace

	counts, err := parseTestJSON(strings.NewReader(input))
	if err != nil {
		t.Fatalf("parseTestJSON() error = %v", err)
	}

	if counts != (TestCounts{Pass: 1, Skip: 0, Fail: 0}) {
		t.Errorf("parseTestJSON() = %+v, want {Pass: 1}", counts)
	}
}
