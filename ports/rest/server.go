// SPDX-License-Identifier: TODO

package rest

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/m4schini/splitkauf/ports/rest/middleware"
	v1 "github.com/m4schini/splitkauf/ports/rest/v1"
	"github.com/m4schini/splitkauf/telemetry/metrics"
)

func New(si v1.ServerInterface) http.Handler {
	r := chi.NewRouter()
	r.Mount("/", ApiDocsHandler())
	r.Mount("/api/v1", v1.New(si, v1.ChiServerOptions{
		Middlewares: []v1.MiddlewareFunc{
			middleware.Logging,
			metrics.Middleware,
		},
	}))

	return r
}
