// SPDX-License-Identifier: CC0-1.0

// Command splitkauf is a shared shopping-list service; see package cmd for
// the CLI it exposes.
package main

import (
	_ "embed"

	"github.com/m4schini/splitkauf/cmd"
	"github.com/m4schini/splitkauf/ports/rest"
)

//go:embed splitkauf.openapi.yaml
var openAPISpec []byte

func main() {
	rest.SetOpenAPISpec(openAPISpec)
	cmd.Execute()
}
