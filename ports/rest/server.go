// SPDX-License-Identifier: TODO

package rest

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/m4schini/splitkauf/ports/rest/middleware"
	"github.com/m4schini/splitkauf/ports/rest/problem"
	v1 "github.com/m4schini/splitkauf/ports/rest/v1"
	"github.com/m4schini/splitkauf/ports/web"
	"github.com/m4schini/splitkauf/telemetry/metrics"
)

func New(si v1.ServerInterface) http.Handler {
	r := chi.NewRouter()
	r.Mount("/", ApiDocsHandler())

	// The API subrouter is passed to the generated handler as its BaseRouter so
	// that its NotFound/MethodNotAllowed handlers — which emit RFC 9457 problem
	// responses — cover unknown routes and methods under /api/v1. Recover is
	// applied here (not via ChiServerOptions.Middlewares) because those are
	// applied per-route inside the generated r.Group and do NOT wrap the
	// NotFound/MethodNotAllowed handlers; Use'ing it on the BaseRouter covers
	// every surface.
	apiRouter := chi.NewRouter()
	apiRouter.Use(middleware.Recover)
	apiRouter.NotFound(func(w http.ResponseWriter, r *http.Request) {
		problem.Write(w, r, problem.New(problem.NotFound, "no resource exists at this path"))
	})
	apiRouter.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		problem.Write(w, r, problem.New(problem.MethodNotAllowed, "this method is not supported for this resource"))
	})

	r.Mount("/api/v1", v1.New(si, v1.ChiServerOptions{
		BaseRouter: apiRouter,
		// The generated wrapper applies these in slice order so the LAST entry is
		// outermost: on a request, metrics runs first, then Logging, then the
		// Validator, then DevAuth, then the handler. Keeping metrics/Logging
		// outermost lets them observe validation failures too; DevAuth is
		// innermost so it injects the dev user for every (valid) API request.
		// DevAuth is temporary (M1); see middleware.DevAuth.
		Middlewares: []v1.MiddlewareFunc{
			middleware.DevAuth,
			v1.Validator(),
			middleware.Logging,
			metrics.Middleware,
		},
		ErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
			problem.Write(w, r, problem.New(problem.Validation, err.Error()))
		},
	}))

	// The embedded frontend is the root catch-all: it serves the SPA for any
	// path not matched above (api-catalog, openapi spec, docs, or /api/v1).
	r.NotFound(web.Handler().ServeHTTP)

	return r
}
