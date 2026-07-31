// SPDX-License-Identifier: TODO

package rest

import (
	"net/http"

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"
	"github.com/m4schini/splitkauf/auth"
	"github.com/m4schini/splitkauf/ports/rest/middleware"
	"github.com/m4schini/splitkauf/ports/rest/problem"
	v1 "github.com/m4schini/splitkauf/ports/rest/v1"
	"github.com/m4schini/splitkauf/ports/web"
	"github.com/m4schini/splitkauf/telemetry/metrics"
)

// New builds the top-level HTTP handler. sm is the scs session manager whose
// LoadAndSave middleware wraps every route (so sessions load/save for the auth
// endpoints too); authr provides the browser-facing /api/auth/* handlers and
// the RequireAuth middleware guarding the JSON API.
func New(si v1.ServerInterface, sm *scs.SessionManager, authr auth.Authenticator) http.Handler {
	r := chi.NewRouter()
	r.Mount("/", ApiDocsHandler())

	// Hand-written BFF auth endpoints. They live OUTSIDE /api/v1 and its
	// request-validation middleware: they are browser-facing OAuth redirect
	// endpoints, not JSON API resources. They still run inside LoadAndSave
	// (wrapped below) so login/callback/logout can read and write the session.
	r.Get("/api/auth/login", authr.Login)
	r.Get("/api/auth/callback", authr.Callback)
	r.Post("/api/auth/logout", authr.Logout)

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
		// Validator, then RequireAuth, then the handler. Keeping metrics/Logging
		// outermost lets them observe validation and auth failures too;
		// RequireAuth is innermost so it authenticates the request (injecting the
		// current user) for every (valid) API request. In OIDC mode an
		// unauthenticated request gets a 401 problem here; in dev mode the fixed
		// dev user is injected.
		Middlewares: []v1.MiddlewareFunc{
			publicHealth(authr.RequireAuth),
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

	// LoadAndSave wraps the ENTIRE handler (outermost) so the session is loaded
	// before, and saved after, every route — including /api/auth/* and the
	// generated /api/v1 chain. It sets Vary: Cookie on responses.
	return sm.LoadAndSave(r)
}

// publicHealth wraps the RequireAuth middleware so that GET /api/v1/health stays
// publicly reachable (for liveness/readiness probes) while every other /api/v1
// route still requires a valid session. The health endpoint is matched by exact
// path and method; anything else — including other methods on /api/v1/health —
// is delegated to RequireAuth unchanged, so auth is never weakened elsewhere.
func publicHealth(requireAuth func(http.Handler) http.Handler) v1.MiddlewareFunc {
	return func(next http.Handler) http.Handler {
		guarded := requireAuth(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodGet && r.URL.Path == "/api/v1/health" {
				next.ServeHTTP(w, r)
				return
			}
			guarded.ServeHTTP(w, r)
		})
	}
}
