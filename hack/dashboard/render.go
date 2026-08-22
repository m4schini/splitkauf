// SPDX-License-Identifier: CC0-1.0

package main

import (
	"fmt"
	"strconv"
	"strings"
)

// naText is the placeholder rendered for any metric whose producer input is
// absent.
const naText = "n/a"

// shortCommitLen is how many leading characters of a full commit SHA this
// tool displays and uses as the trend table's row key. Truncating here (once,
// on the way in) keeps the key stable even if a caller's SHA length changes.
const shortCommitLen = 7

// shortCommit truncates commit to shortCommitLen characters, or returns it
// unchanged when it is already that short (or shorter).
func shortCommit(commit string) string {
	if len(commit) <= shortCommitLen {
		return commit
	}

	return commit[:shortCommitLen]
}

// render assembles the full Quality Dashboard issue body.
func render(inputs Inputs) string {
	var body strings.Builder

	body.WriteString("# 📊 Quality Dashboard\n\n")
	body.WriteString("_Auto-updated on each main push + weekly scan. Do not edit — the bot overwrites._\n")
	body.WriteString(renderHeaderLine(inputs.Meta))
	body.WriteString("\n\n")

	body.WriteString(renderSummary(inputs))
	body.WriteString("\n")

	body.WriteString(renderCoverageSection(inputs.Coverage))
	body.WriteString("\n")

	body.WriteString(renderLintDebtSection(inputs.LintDebt))
	body.WriteString("\n")

	body.WriteString(renderTrendSection(inputs))

	return body.String()
}

// renderHeaderLine renders the "Last update: ... commit ... run" line.
func renderHeaderLine(meta Meta) string {
	updated := orNA(meta.Updated)
	commit := naText

	if meta.Commit != "" {
		commit = "`" + shortCommit(meta.Commit) + "`"
	}

	return fmt.Sprintf("Last update: %s · commit %s · %s", updated, commit, runLink(meta.RunURL))
}

// renderSummary renders the top-level metrics table.
func renderSummary(inputs Inputs) string {
	var table strings.Builder

	table.WriteString("## Summary\n")
	table.WriteString("| Metric              | Value |\n")
	table.WriteString("|---------------------|-------|\n")
	fmt.Fprintf(&table, "| Go coverage (total) | %s |\n", coverageSummaryValue(inputs.Coverage))
	fmt.Fprintf(&table, "| Lint debt           | %s |\n", lintDebtSummaryValue(inputs.LintDebt))
	fmt.Fprintf(&table, "| Go tests            | %s |\n", testsSummaryValue(inputs.Tests))
	fmt.Fprintf(&table, "| Security scan       | %s |\n", securitySummaryValue(inputs.Security))

	return table.String()
}

func coverageSummaryValue(cov *Coverage) string {
	if cov == nil {
		return naText
	}

	return formatPercent(cov.Total.Percent())
}

func lintDebtSummaryValue(debt *LintDebt) string {
	if debt == nil {
		return naText
	}

	return fmt.Sprintf("%d findings across %d deferred linters", debt.Total, len(debt.ByLinter))
}

func testsSummaryValue(tests *TestCounts) string {
	if tests == nil {
		return naText
	}

	value := fmt.Sprintf("%d pass · %d skip", tests.Pass, tests.Skip)
	if tests.Fail > 0 {
		value += fmt.Sprintf(" · %d fail", tests.Fail)
	}

	return value
}

func securitySummaryValue(sec *Security) string {
	if sec == nil {
		return naText
	}

	label := securityStatusLabel(sec.Status)
	if label == "" {
		return naText
	}

	return fmt.Sprintf("%s · %s · %s", label, orNA(sec.CompletedAt), runLink(sec.RunURL))
}

// securityStatusLabel maps a workflow-run conclusion to its display label.
// An unrecognised or absent status returns "".
func securityStatusLabel(status string) string {
	switch status {
	case "success":
		return "✅ pass"
	case "failure":
		return "❌ fail"
	default:
		return ""
	}
}

// renderCoverageSection renders the collapsible per-package coverage table.
func renderCoverageSection(cov *Coverage) string {
	var section strings.Builder

	section.WriteString("## Coverage by package\n")

	if cov == nil {
		section.WriteString(naText + "\n")

		return section.String()
	}

	fmt.Fprintf(&section, "<details><summary>%d packages</summary>\n\n", len(cov.Packages))
	section.WriteString("| Package | Coverage |\n")
	section.WriteString("|---------|----------|\n")

	for _, pkg := range cov.Packages {
		fmt.Fprintf(&section, "| %s | %s |\n", pkg.Package, formatPercent(pkg.Stat.Percent()))
	}

	section.WriteString("</details>\n")

	return section.String()
}

// renderLintDebtSection renders the collapsible per-linter debt table.
func renderLintDebtSection(debt *LintDebt) string {
	var section strings.Builder

	section.WriteString("## Lint debt by linter\n")

	if debt == nil {
		section.WriteString(naText + "\n")

		return section.String()
	}

	fmt.Fprintf(&section, "<details><summary>%d linters · %d findings</summary>\n\n", len(debt.ByLinter), debt.Total)
	section.WriteString("| Linter | Findings |\n")
	section.WriteString("|--------|----------|\n")

	for _, row := range debt.ByLinter {
		fmt.Fprintf(&section, "| %s | %d |\n", row.Linter, row.Count)
	}

	section.WriteString("</details>\n")

	return section.String()
}

// renderTrendSection renders the trend table: prior rows carried forward from
// PrevBody, plus a new row when Coverage and Meta.Commit are both present
// (the commit that produced the coverage data keys the row).
func renderTrendSection(inputs Inputs) string {
	rows := extractTrend(inputs.PrevBody)

	if inputs.Coverage != nil && inputs.Meta.Commit != "" {
		rows = mergeTrend(rows, newTrendRow(inputs))
	}

	var section strings.Builder

	section.WriteString("## Trend\n")
	section.WriteString(trendStartMarker + "\n")
	section.WriteString("| Date | Commit | Coverage | Lint debt | Tests |\n")
	section.WriteString("|------|--------|----------|-----------|-------|\n")

	for _, row := range rows {
		fmt.Fprintf(&section, "| %s | `%s` | %s | %s | %s |\n", row.Date, row.Commit, row.Coverage, row.LintDebt, row.Tests)
	}

	section.WriteString(trendEndMarker + "\n")

	return section.String()
}

// newTrendRow builds the trend row for the current run. It is only called
// when Coverage and Meta.Commit are present, so Coverage is safe to
// dereference.
func newTrendRow(inputs Inputs) TrendRow {
	row := TrendRow{
		Date:     trendDate(inputs.Meta.Updated),
		Commit:   shortCommit(inputs.Meta.Commit),
		Coverage: formatPercent(inputs.Coverage.Total.Percent()),
		LintDebt: naText,
		Tests:    naText,
	}

	if inputs.LintDebt != nil {
		row.LintDebt = strconv.Itoa(inputs.LintDebt.Total)
	}

	if inputs.Tests != nil {
		row.Tests = strconv.Itoa(inputs.Tests.Pass)
	}

	return row
}

// formatPercent formats a coverage fraction to one decimal place.
func formatPercent(pct float64) string {
	return fmt.Sprintf("%.1f%%", pct)
}

// runLink renders a GitHub Actions run URL as a Markdown link, or "n/a" when
// url is empty.
func runLink(url string) string {
	if url == "" {
		return naText
	}

	return fmt.Sprintf("[run](%s)", url)
}

// orNA returns s, or "n/a" when s is empty.
func orNA(s string) string {
	if s == "" {
		return naText
	}

	return s
}
