// SPDX-License-Identifier: CC0-1.0

package main

import (
	"os"
	"strings"
	"testing"
)

func TestParseLintJSON(t *testing.T) {
	t.Parallel()

	file, err := os.Open("testdata/lint-debt.json")
	if err != nil {
		t.Fatalf("opening fixture: %v", err)
	}
	defer file.Close() //nolint:errcheck // read-only test fixture

	debt, err := parseLintJSON(file)
	if err != nil {
		t.Fatalf("parseLintJSON() error = %v", err)
	}

	if debt.Total != 3 {
		t.Errorf("Total = %d, want 3", debt.Total)
	}

	want := []LinterCount{
		{Linter: "varnamelen", Count: 2},
		{Linter: "wsl_v5", Count: 1},
	}

	if len(debt.ByLinter) != len(want) {
		t.Fatalf("len(ByLinter) = %d, want %d", len(debt.ByLinter), len(want))
	}

	for i, w := range want {
		if debt.ByLinter[i] != w {
			t.Errorf("ByLinter[%d] = %+v, want %+v", i, debt.ByLinter[i], w)
		}
	}
}

func TestParseLintJSONSortsByCountThenName(t *testing.T) {
	t.Parallel()

	input := `{"Issues":[
		{"FromLinter":"b"},
		{"FromLinter":"a"},
		{"FromLinter":"a"},
		{"FromLinter":"z"},
		{"FromLinter":"z"}
	]}`

	debt, err := parseLintJSON(strings.NewReader(input))
	if err != nil {
		t.Fatalf("parseLintJSON() error = %v", err)
	}

	want := []LinterCount{
		{Linter: "a", Count: 2},
		{Linter: "z", Count: 2},
		{Linter: "b", Count: 1},
	}

	if len(debt.ByLinter) != len(want) {
		t.Fatalf("len(ByLinter) = %d, want %d", len(debt.ByLinter), len(want))
	}

	for i, w := range want {
		if debt.ByLinter[i] != w {
			t.Errorf("ByLinter[%d] = %+v, want %+v", i, debt.ByLinter[i], w)
		}
	}
}

func TestParseLintJSONEmpty(t *testing.T) {
	t.Parallel()

	debt, err := parseLintJSON(strings.NewReader(`{"Issues":[]}`))
	if err != nil {
		t.Fatalf("parseLintJSON() error = %v", err)
	}

	if debt.Total != 0 || len(debt.ByLinter) != 0 {
		t.Errorf("parseLintJSON() = %+v, want zero", debt)
	}
}

func TestParseLintJSONMalformed(t *testing.T) {
	t.Parallel()

	_, err := parseLintJSON(strings.NewReader(`not json`))
	if err == nil {
		t.Fatal("parseLintJSON() error = nil, want an error")
	}
}
