// SPDX-License-Identifier: CC0-1.0

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
