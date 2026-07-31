// SPDX-License-Identifier: TODO

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

	return nethttpmiddleware.OapiRequestValidatorWithOptions(spec, &nethttpmiddleware.Options{
		SilenceServersWarning: true,
		ErrorHandlerWithOpts: func(_ context.Context, err error, w http.ResponseWriter, r *http.Request, opts nethttpmiddleware.ErrorHandlerOpts) {
			problem.Write(w, r, problem.New(problem.FromStatus(opts.StatusCode), err.Error()))
		},
	})
}
