// SPDX-License-Identifier: CC0-1.0

package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// Scanner buffer sizes for go test -json output: lines can be long (large
// subtest names, output events), so grow well past bufio.Scanner's 64KiB
// default.
const (
	initialScanBufferSize = 64 * 1024
	maxScanBufferSize     = 10 * 1024 * 1024
)

// TestCounts is the number of individual tests (not subtests' parent groups
// only — every event with a non-empty Test field counts) that ended pass,
// skip, or fail.
type TestCounts struct {
	Pass int
	Skip int
	Fail int
}

// testEvent is the subset of a `go test -json` event this tool needs. The
// JSON field names are `go test`'s own output schema, not ours to camelCase.
type testEvent struct {
	Action string `json:"Action"` //nolint:tagliatelle // go test's own output schema
	Test   string `json:"Test"`   //nolint:tagliatelle // go test's own output schema
}

// parseTestJSON counts final pass/skip/fail actions from a `go test -json`
// stream. A line that is not a well-formed JSON event is skipped rather than
// failing the whole parse, so the input may be interleaved with plain-text
// output (make's command echo ahead of the JSON stream) or end in a line
// truncated by a killed job.
func parseTestJSON(report io.Reader) (TestCounts, error) {
	var counts TestCounts

	scanner := bufio.NewScanner(report)
	scanner.Buffer(make([]byte, 0, initialScanBufferSize), maxScanBufferSize)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "{") {
			continue
		}

		var event testEvent

		err := json.Unmarshal([]byte(line), &event)
		if err != nil {
			continue
		}

		if event.Test == "" {
			continue
		}

		switch event.Action {
		case "pass":
			counts.Pass++
		case "skip":
			counts.Skip++
		case "fail":
			counts.Fail++
		}
	}

	if err := scanner.Err(); err != nil {
		return TestCounts{}, fmt.Errorf("scanning test report: %w", err)
	}

	return counts, nil
}
