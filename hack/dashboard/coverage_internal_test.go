// SPDX-License-Identifier: CC0-1.0

package main

import (
	"errors"
	"os"
	"strings"
	"testing"
)

func TestParseProfile(t *testing.T) {
	t.Parallel()

	file, err := os.Open("testdata/coverage.out")
	if err != nil {
		t.Fatalf("opening fixture: %v", err)
	}
	defer file.Close() //nolint:errcheck // read-only test fixture

	cov, err := parseProfile(file)
	if err != nil {
		t.Fatalf("parseProfile() error = %v", err)
	}

	if len(cov.Packages) != 3 {
		t.Fatalf("len(Packages) = %d, want 3", len(cov.Packages))
	}

	wantPackages := []struct {
		name    string
		covered int
		total   int
	}{
		{".", 0, 1},
		{"lists", 1, 3},
		{"users", 3, 3},
	}

	for i, want := range wantPackages {
		got := cov.Packages[i]
		if got.Package != want.name || got.Stat.Covered != want.covered || got.Stat.Total != want.total {
			t.Errorf("Packages[%d] = %+v, want {%s %d/%d}", i, got, want.name, want.covered, want.total)
		}
	}

	if cov.Total.Covered != 4 || cov.Total.Total != 7 {
		t.Errorf("Total = %+v, want 4/7", cov.Total)
	}
}

func TestParseProfileIgnoresModeLine(t *testing.T) {
	t.Parallel()

	cov, err := parseProfile(strings.NewReader("mode: atomic\n"))
	if err != nil {
		t.Fatalf("parseProfile() error = %v", err)
	}

	if len(cov.Packages) != 0 {
		t.Errorf("len(Packages) = %d, want 0", len(cov.Packages))
	}
}

func TestParseProfileMalformedLine(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		line string
	}{
		{"too few fields", "mode: atomic\nfile.go:1.1,2.2 1\n"},
		{"missing colon", "mode: atomic\nfile.go 1 1\n"},
		{"non-numeric stmt count", "mode: atomic\nfile.go:1.1,2.2 x 1\n"},
		{"non-numeric hit count", "mode: atomic\nfile.go:1.1,2.2 1 x\n"},
	}

	for _, tst := range tests {
		t.Run(tst.name, func(t *testing.T) {
			t.Parallel()

			_, err := parseProfile(strings.NewReader(tst.line))
			if err == nil {
				t.Fatalf("parseProfile(%q) error = nil, want an error", tst.line)
			}
		})
	}
}

func TestParseProfileMalformedLineWrapsSentinel(t *testing.T) {
	t.Parallel()

	_, err := parseProfile(strings.NewReader("mode: atomic\nfile.go 1 1\n"))
	if !errors.Is(err, ErrMalformedProfileLine) {
		t.Errorf("error = %v, want it to wrap ErrMalformedProfileLine", err)
	}
}

func TestPackageOf(t *testing.T) {
	t.Parallel()

	tests := []struct {
		file string
		want string
	}{
		{"github.com/m4schini/splitkauf/lists/service.go", "lists"},
		{"github.com/m4schini/splitkauf/adapters/db/repo.go", "adapters/db"},
		{"main.go", "."},
	}

	for _, tst := range tests {
		t.Run(tst.file, func(t *testing.T) {
			t.Parallel()

			if got := packageOf(tst.file); got != tst.want {
				t.Errorf("packageOf(%q) = %q, want %q", tst.file, got, tst.want)
			}
		})
	}
}

func TestStatPercent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		stat Stat
		want float64
	}{
		{"zero total", Stat{Covered: 0, Total: 0}, 0},
		{"half covered", Stat{Covered: 1, Total: 2}, 50},
		{"fully covered", Stat{Covered: 3, Total: 3}, 100},
	}

	for _, tst := range tests {
		t.Run(tst.name, func(t *testing.T) {
			t.Parallel()

			if got := tst.stat.Percent(); got != tst.want {
				t.Errorf("Percent() = %v, want %v", got, tst.want)
			}
		})
	}
}
