// SPDX-License-Identifier: CC0-1.0

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
)

// LinterCount is one row of the per-linter debt table.
type LinterCount struct {
	Linter string
	Count  int
}

// LintDebt is the parsed result of a golangci-lint JSON report: the total
// finding count and one row per linter, sorted by count descending then
// linter name.
type LintDebt struct {
	Total    int
	ByLinter []LinterCount
}

// lintReport is the subset of golangci-lint's `--output.json.path` schema
// this tool needs. The JSON field names are golangci-lint's own output
// schema, not ours to camelCase.
type lintReport struct {
	Issues []struct {
		FromLinter string `json:"FromLinter"` //nolint:tagliatelle // golangci-lint's own output schema
	} `json:"Issues"` //nolint:tagliatelle // golangci-lint's own output schema
}

// parseLintJSON counts findings per linter from a golangci-lint JSON report.
func parseLintJSON(report io.Reader) (LintDebt, error) {
	var parsed lintReport

	err := json.NewDecoder(report).Decode(&parsed)
	if err != nil {
		return LintDebt{}, fmt.Errorf("decoding lint report: %w", err)
	}

	perLinter := map[string]int{}
	for _, issue := range parsed.Issues {
		perLinter[issue.FromLinter]++
	}

	debt := LintDebt{
		Total:    len(parsed.Issues),
		ByLinter: make([]LinterCount, 0, len(perLinter)),
	}

	for linter, count := range perLinter {
		debt.ByLinter = append(debt.ByLinter, LinterCount{Linter: linter, Count: count})
	}

	sort.Slice(debt.ByLinter, func(left, right int) bool {
		if debt.ByLinter[left].Count != debt.ByLinter[right].Count {
			return debt.ByLinter[left].Count > debt.ByLinter[right].Count
		}

		return debt.ByLinter[left].Linter < debt.ByLinter[right].Linter
	})

	return debt, nil
}
