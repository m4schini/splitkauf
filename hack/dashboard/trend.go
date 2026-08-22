// SPDX-License-Identifier: CC0-1.0

package main

import (
	"regexp"
	"strings"
)

// trendStartMarker and trendEndMarker delimit the trend table's data rows in
// the issue body, so history survives a full-body rebuild.
const (
	trendStartMarker = "<!-- trend-start -->"
	trendEndMarker   = "<!-- trend-end -->"

	maxTrendRows = 20
)

// trendRowPattern matches one trend-table row, e.g.
// "| 2026-08-21 | `abc1234` | 61.4% | 547 | 214 |".
var trendRowPattern = regexp.MustCompile(
	"^\\|\\s*(\\S+)\\s*\\|\\s*`([0-9a-fA-F]+)`\\s*\\|\\s*(\\S+)\\s*\\|\\s*(\\S+)\\s*\\|\\s*(\\S+)\\s*\\|\\s*$",
)

// trendDatePattern matches an ISO date (the shape trendDate produces and
// extractTrend's row pattern expects back).
var trendDatePattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// TrendRow is one row of the trend table.
type TrendRow struct {
	Date     string
	Commit   string
	Coverage string
	LintDebt string
	Tests    string
}

// extractTrend parses the trend rows out of a previous issue body. Rows
// outside the trend-start/trend-end markers, and malformed rows within them,
// are ignored rather than failing the whole extraction.
func extractTrend(prevBody string) []TrendRow {
	between, ok := betweenMarkers(prevBody, trendStartMarker, trendEndMarker)
	if !ok {
		return nil
	}

	var rows []TrendRow

	for line := range strings.SplitSeq(between, "\n") {
		match := trendRowPattern.FindStringSubmatch(strings.TrimRight(line, "\r"))
		if match == nil {
			continue
		}

		rows = append(rows, TrendRow{
			Date:     match[1],
			Commit:   match[2],
			Coverage: match[3],
			LintDebt: match[4],
			Tests:    match[5],
		})
	}

	return rows
}

// mergeTrend prepends newRow to rows unless newRow's commit is already
// present, then trims the result to maxTrendRows either way (a previous body
// hand-edited past the cap should not persist forever).
func mergeTrend(rows []TrendRow, newRow TrendRow) []TrendRow {
	for _, row := range rows {
		if row.Commit == newRow.Commit {
			return trimTrend(rows)
		}
	}

	merged := make([]TrendRow, 0, len(rows)+1)
	merged = append(merged, newRow)
	merged = append(merged, rows...)

	return trimTrend(merged)
}

// trimTrend caps rows at maxTrendRows, keeping the newest (leading) entries.
func trimTrend(rows []TrendRow) []TrendRow {
	if len(rows) > maxTrendRows {
		return rows[:maxTrendRows]
	}

	return rows
}

// trendDate takes the leading "YYYY-MM-DD" out of an "updated" timestamp
// (expected form "YYYY-MM-DD HH:MM UTC"). Anything that is not that exact
// shape renders as "n/a" rather than risking a value trendRowPattern cannot
// parse back out of the rendered table on the next rebuild.
func trendDate(updated string) string {
	const dateLen = len("2026-08-21")

	if len(updated) < dateLen {
		return naText
	}

	date := updated[:dateLen]
	if !trendDatePattern.MatchString(date) {
		return naText
	}

	return date
}

// betweenMarkers returns the substring strictly between the first occurrence
// of start and the first occurrence of end after it. ok is false when either
// marker is absent.
func betweenMarkers(body, start, end string) (string, bool) {
	startIdx := strings.Index(body, start)
	if startIdx < 0 {
		return "", false
	}

	afterStart := startIdx + len(start)

	endIdx := strings.Index(body[afterStart:], end)
	if endIdx < 0 {
		return "", false
	}

	return body[afterStart : afterStart+endIdx], true
}
