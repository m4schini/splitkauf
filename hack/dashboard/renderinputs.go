// SPDX-License-Identifier: CC0-1.0

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
)

// Meta is the {commit, run_url, updated} triple the caller assembles for the
// header line. A zero Meta renders every field as "n/a". The JSON field
// names are the wire contract update.sh and this tool agree on, not Go's
// preferred camelCase.
type Meta struct {
	Commit  string `json:"commit"`
	RunURL  string `json:"run_url"` //nolint:tagliatelle // wire contract with update.sh, not ours to camelCase
	Updated string `json:"updated"`
}

// Security is the status of the latest quality-workflow security scan. A nil
// *Security, or one whose Status is neither "success" nor "failure", renders
// as "n/a". The JSON field names are the wire contract update.sh and this
// tool agree on, not Go's preferred camelCase.
type Security struct {
	Status      string `json:"status"`
	CompletedAt string `json:"completed_at"` //nolint:tagliatelle // wire contract with update.sh, not ours to camelCase
	RunURL      string `json:"run_url"`      //nolint:tagliatelle // wire contract with update.sh, not ours to camelCase
}

// Inputs bundles every render() input. Every field except Meta and PrevBody
// is a pointer so an absent producer artifact renders its section as "n/a"
// rather than failing the render.
type Inputs struct {
	Coverage *Coverage
	Tests    *TestCounts
	LintDebt *LintDebt
	Security *Security
	Meta     Meta
	PrevBody string
}

// renderInputPaths is the set of optional file paths loadRenderInputs reads;
// an empty path leaves the corresponding Inputs field at its zero value.
type renderInputPaths struct {
	coverage string
	tests    string
	lintDebt string
	security string
	meta     string
	prevBody string
}

// loadRenderInputs reads each optional path into Inputs. An empty path
// leaves the corresponding field nil (or, for PrevBody, empty).
func loadRenderInputs(paths renderInputPaths) (Inputs, error) {
	var inputs Inputs

	loaders := []func() error{
		func() error { return loadCoverage(&inputs, paths.coverage) },
		func() error { return loadTests(&inputs, paths.tests) },
		func() error { return loadLintDebt(&inputs, paths.lintDebt) },
		func() error { return loadSecurity(&inputs, paths.security) },
		func() error { return loadMeta(&inputs, paths.meta) },
		func() error { return loadPrevBody(&inputs, paths.prevBody) },
	}

	for _, load := range loaders {
		err := load()
		if err != nil {
			return Inputs{}, err
		}
	}

	return inputs, nil
}

// missing reports whether path is unset or does not exist. A producer
// artifact that never showed up (a first run, a failed job) is not an error
// here — the caller renders "n/a" instead.
func missing(path string) bool {
	if path == "" {
		return true
	}

	_, err := os.Stat(path)

	return errors.Is(err, fs.ErrNotExist)
}

// loadCoverage parses a cover profile into inputs.Coverage when path is set
// and exists.
func loadCoverage(inputs *Inputs, path string) error {
	if missing(path) {
		return nil
	}

	file, err := openFile(path)
	if err != nil {
		return err
	}
	defer file.Close() //nolint:errcheck // read-only file, close error carries no useful signal here

	cov, err := parseProfile(file)
	if err != nil {
		return fmt.Errorf("parsing %s: %w", path, err)
	}

	inputs.Coverage = &cov

	return nil
}

// loadTests parses a `go test -json` report into inputs.Tests when path is
// set and exists.
func loadTests(inputs *Inputs, path string) error {
	if missing(path) {
		return nil
	}

	file, err := openFile(path)
	if err != nil {
		return err
	}
	defer file.Close() //nolint:errcheck // read-only file, close error carries no useful signal here

	tests, err := parseTestJSON(file)
	if err != nil {
		return fmt.Errorf("parsing %s: %w", path, err)
	}

	inputs.Tests = &tests

	return nil
}

// loadLintDebt parses a golangci-lint JSON report into inputs.LintDebt when
// path is set and exists.
func loadLintDebt(inputs *Inputs, path string) error {
	if missing(path) {
		return nil
	}

	file, err := openFile(path)
	if err != nil {
		return err
	}
	defer file.Close() //nolint:errcheck // read-only file, close error carries no useful signal here

	debt, err := parseLintJSON(file)
	if err != nil {
		return fmt.Errorf("parsing %s: %w", path, err)
	}

	inputs.LintDebt = &debt

	return nil
}

// loadSecurity decodes a security-status JSON file into inputs.Security when
// path is set and exists.
func loadSecurity(inputs *Inputs, path string) error {
	if missing(path) {
		return nil
	}

	var sec Security

	err := readJSONFile(path, &sec)
	if err != nil {
		return err
	}

	inputs.Security = &sec

	return nil
}

// loadMeta decodes a {commit, run_url, updated} JSON file into inputs.Meta
// when path is set and exists. A literal "n/a" commit (a caller's explicit
// "unknown" placeholder) is normalised to "" so it is treated as absent
// everywhere else in this tool.
func loadMeta(inputs *Inputs, path string) error {
	if missing(path) {
		return nil
	}

	err := readJSONFile(path, &inputs.Meta)
	if err != nil {
		return err
	}

	if inputs.Meta.Commit == naText {
		inputs.Meta.Commit = ""
	}

	return nil
}

// loadPrevBody reads the previous issue body into inputs.PrevBody when path
// is set and exists.
func loadPrevBody(inputs *Inputs, path string) error {
	if missing(path) {
		return nil
	}

	// #nosec G304 -- operator-supplied CLI argument, not untrusted input
	body, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading %s: %w", path, err)
	}

	inputs.PrevBody = string(body)

	return nil
}

// openFile opens path for reading; path is an operator-supplied CLI
// argument, not untrusted input.
func openFile(path string) (*os.File, error) {
	file, err := os.Open(path) // #nosec G304 -- operator-supplied CLI argument, not untrusted input
	if err != nil {
		return nil, fmt.Errorf("opening %s: %w", path, err)
	}

	return file, nil
}

// readJSONFile decodes the JSON file at path into dst.
func readJSONFile(path string, dst any) error {
	// #nosec G304 -- operator-supplied CLI argument, not untrusted input
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading %s: %w", path, err)
	}

	err = json.Unmarshal(data, dst)
	if err != nil {
		return fmt.Errorf("decoding %s: %w", path, err)
	}

	return nil
}
