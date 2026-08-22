// SPDX-License-Identifier: CC0-1.0

package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"path"
	"sort"
	"strconv"
	"strings"
)

// modulePrefix is trimmed from cover-profile file paths to derive package
// display names.
const modulePrefix = "github.com/m4schini/splitkauf/"

// percentScale converts a covered/total fraction to a percentage.
const percentScale = 100

// profileLineFields is the number of whitespace-separated fields a cover
// profile data line has: `file:pos numStmts hitCount`.
const profileLineFields = 3

// ErrMalformedProfileLine is wrapped by parseProfileLine when a cover profile
// data line does not have the expected shape.
var ErrMalformedProfileLine = errors.New("malformed cover profile line")

// Stat is a covered/total statement count.
type Stat struct {
	Covered int
	Total   int
}

// Percent returns the covered fraction as a percentage, or 0 when Total is 0.
func (stat Stat) Percent() float64 {
	if stat.Total == 0 {
		return 0
	}

	return float64(stat.Covered) / float64(stat.Total) * percentScale
}

// PackageCoverage is one row of the per-package coverage table.
type PackageCoverage struct {
	Package string
	Stat    Stat
}

// Coverage is the parsed result of a Go cover profile: an overall total and
// one row per package, sorted by package name.
type Coverage struct {
	Total    Stat
	Packages []PackageCoverage
}

// parseProfile aggregates covered/total statement counts per package and
// overall from a raw `go test -coverprofile` profile. The leading `mode:`
// line is ignored.
func parseProfile(profile io.Reader) (Coverage, error) {
	perPackage := map[string]Stat{}

	scanner := bufio.NewScanner(profile)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" || strings.HasPrefix(line, "mode:") {
			continue
		}

		pkg, numStmt, hitCount, err := parseProfileLine(line)
		if err != nil {
			return Coverage{}, fmt.Errorf("parsing profile line %q: %w", line, err)
		}

		stat := perPackage[pkg]
		stat.Total += numStmt

		if hitCount > 0 {
			stat.Covered += numStmt
		}

		perPackage[pkg] = stat
	}

	if err := scanner.Err(); err != nil {
		return Coverage{}, fmt.Errorf("scanning profile: %w", err)
	}

	return buildCoverage(perPackage), nil
}

// parseProfileLine splits one profile line
// (`file.go:startLine.startCol,endLine.endCol numStmts hitCount`) into its
// package, statement count, and hit count.
func parseProfileLine(line string) (string, int, int, error) {
	fields := strings.Fields(line)
	if len(fields) != profileLineFields {
		return "", 0, 0, fmt.Errorf("%w: expected %d fields, got %d", ErrMalformedProfileLine, profileLineFields, len(fields))
	}

	file, _, ok := strings.Cut(fields[0], ":")
	if !ok {
		return "", 0, 0, fmt.Errorf("%w: missing ':' in %q", ErrMalformedProfileLine, fields[0])
	}

	numStmt, err := strconv.Atoi(fields[1])
	if err != nil {
		return "", 0, 0, fmt.Errorf("parsing statement count: %w", err)
	}

	hitCount, err := strconv.Atoi(fields[2])
	if err != nil {
		return "", 0, 0, fmt.Errorf("parsing hit count: %w", err)
	}

	return packageOf(file), numStmt, hitCount, nil
}

// packageOf derives a package display name from a cover-profile file path:
// the module prefix is trimmed and the file's directory is kept; a file at
// the module root renders as ".".
func packageOf(file string) string {
	trimmed := strings.TrimPrefix(file, modulePrefix)

	return path.Dir(trimmed)
}

// buildCoverage folds per-package statement counts into a Coverage, sorted by
// package name.
func buildCoverage(perPackage map[string]Stat) Coverage {
	var cov Coverage

	cov.Packages = make([]PackageCoverage, 0, len(perPackage))

	for pkg, stat := range perPackage {
		cov.Packages = append(cov.Packages, PackageCoverage{Package: pkg, Stat: stat})
		cov.Total.Covered += stat.Covered
		cov.Total.Total += stat.Total
	}

	sort.Slice(cov.Packages, func(left, right int) bool {
		return cov.Packages[left].Package < cov.Packages[right].Package
	})

	return cov
}
