// SPDX-License-Identifier: CC0-1.0

// Command dashboard renders the body of the "Quality Dashboard" GitHub issue
// and prepares a debt-counting golangci-lint config. It has two subcommands:
//
//   - strip-deferred: read a golangci-lint v2 config, drop every
//     `linters.disable` entry whose head comment mentions "deferred:", and
//     write the result to stdout.
//   - render: assemble the dashboard issue body from the parser inputs below
//     and write it to stdout.
//
// hack/dashboard/update.sh drives this tool from GitHub Actions; see that
// script for the workflow-side orchestration (finding runs, downloading
// artifacts, upserting the issue).
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
)

// Process exit codes.
const (
	exitError    = 1
	exitBadUsage = 2
)

// minArgs is the minimum os.Args length once a subcommand name is present.
const minArgs = 2

func main() {
	if len(os.Args) < minArgs {
		fmt.Fprintln(os.Stderr, "usage: dashboard <strip-deferred|render> [flags]")
		os.Exit(exitBadUsage)
	}

	var err error

	switch os.Args[1] {
	case "strip-deferred":
		err = runStripDeferred(os.Args[2:])
	case "render":
		err = runRender(os.Args[2:])
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand %q\n", os.Args[1])
		os.Exit(exitBadUsage)
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(exitError)
	}
}

// runStripDeferred implements the strip-deferred subcommand: it reads a
// golangci-lint config from the given path (default .golangci.yml, or stdin
// when the path is "-") and writes the deferred-stripped config to stdout.
func runStripDeferred(args []string) error {
	flagSet := flag.NewFlagSet("strip-deferred", flag.ContinueOnError)

	err := flagSet.Parse(args)
	if err != nil {
		return fmt.Errorf("parsing flags: %w", err)
	}

	path := ".golangci.yml"
	if flagSet.NArg() > 0 {
		path = flagSet.Arg(0)
	}

	in, err := readPathOrStdin(path)
	if err != nil {
		return err
	}

	out, err := stripDeferred(in)
	if err != nil {
		return fmt.Errorf("stripping deferred linters: %w", err)
	}

	_, err = os.Stdout.Write(out)
	if err != nil {
		return fmt.Errorf("writing output: %w", err)
	}

	return nil
}

// runRender implements the render subcommand: each flag names an optional
// input file; a flag that is absent or points at a missing file renders as
// "n/a" in the corresponding section of the body.
func runRender(args []string) error {
	flagSet := flag.NewFlagSet("render", flag.ContinueOnError)

	coveragePath := flagSet.String("coverage", "", "path to a go test -coverprofile file")
	testsPath := flagSet.String("tests", "", "path to a go test -json output file")
	lintDebtPath := flagSet.String("lint-debt", "", "path to a golangci-lint JSON output file")
	securityPath := flagSet.String("security", "", "path to a security-status JSON file")
	metaPath := flagSet.String("meta", "", "path to a {commit, run_url, updated} JSON file")
	prevBodyPath := flagSet.String("prev-body", "", "path to the previous issue body")

	err := flagSet.Parse(args)
	if err != nil {
		return fmt.Errorf("parsing flags: %w", err)
	}

	inputs, err := loadRenderInputs(renderInputPaths{
		coverage: *coveragePath,
		tests:    *testsPath,
		lintDebt: *lintDebtPath,
		security: *securityPath,
		meta:     *metaPath,
		prevBody: *prevBodyPath,
	})
	if err != nil {
		return err
	}

	_, err = fmt.Fprint(os.Stdout, render(inputs))
	if err != nil {
		return fmt.Errorf("writing output: %w", err)
	}

	return nil
}

// readPathOrStdin reads path, treating "-" as a request to read stdin
// instead.
func readPathOrStdin(path string) ([]byte, error) {
	if path == "-" {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return nil, fmt.Errorf("reading stdin: %w", err)
		}

		return data, nil
	}

	// #nosec G304 -- path is an operator-supplied CLI argument, not untrusted input.
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}

	return data, nil
}
