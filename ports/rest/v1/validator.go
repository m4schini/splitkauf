// SPDX-License-Identifier: CC0-1.0

package v1

import (
	"context"
	"fmt"
	"net/http"

	nethttpmiddleware "github.com/oapi-codegen/nethttp-middleware"

	"github.com/m4schini/splitkauf/ports/rest/problem"
)

// Validator returns chi middleware that validates incoming requests against the
// embedded OpenAPI spec (via GetSwagger). Validation failures are written as
// RFC 9457 problem responses. It panics if the embedded spec cannot be loaded —
// that is a build-time invariant, not a runtime condition.
//
// The spec's Servers ("/api/v1") are kept intact: the kin-openapi router
// resolves operations against the full request path, so clearing Servers would
// break matching. SilenceServersWarning suppresses the resulting host-warning
// log without disabling matching.
func Validator() func(http.Handler) http.Handler {
	spec, err := GetSwagger()
	if err != nil {
		panic(fmt.Sprintf("v1.Validator: loading embedded OpenAPI spec: %v", err))
	}

	// Built as a zero value plus field assignments (rather than a literal) so
	// the untouched options keep their documented defaults without spelling out
	// every field of the middleware's option surface here.
	var options nethttpmiddleware.Options

	options.SilenceServersWarning = true
	options.ErrorHandlerWithOpts = func(
		_ context.Context,
		handlerErr error,
		writer http.ResponseWriter,
		req *http.Request,
		opts nethttpmiddleware.ErrorHandlerOpts,
	) {
		problem.Write(writer, req, problem.New(problem.FromStatus(opts.StatusCode), handlerErr.Error()))
	}

	return nethttpmiddleware.OapiRequestValidatorWithOptions(spec, &options)
}
