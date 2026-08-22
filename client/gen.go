// SPDX-License-Identifier: CC0-1.0

// This file only carries the go:generate directive for the generated Go
// client for the Splitkauf REST API (client.gen.go, which owns the
// package's godoc). It is generated from splitkauf.openapi.yaml using
// oapi-codegen and is intended to be used by other services to talk to
// this service.
//
// Regenerate:
//
//	go generate ./client/...

//go:generate go tool oapi-codegen -config config.yaml ../splitkauf.openapi.yaml
package client
