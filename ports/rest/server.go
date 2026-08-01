// SPDX-License-Identifier: TODO

package rest

import (
	"net/http"
	"strings"

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"
	"github.com/m4schini/splitkauf/auth"
	"github.com/m4schini/splitkauf/events"
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
func New(si v1.ServerInterface, sm *scs.SessionManager, authr auth.Authenticator, broker *events.Broker) http.Handler {
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
	// MaxBody caps every /api/v1 request body at 1 MiB (US-Q.5): a declared
	// oversized Content-Length is rejected immediately with a 413 problem, and
	// http.MaxBytesReader backstops bodies without a declared length. The
	// hand-written /api/auth/* endpoints above are mounted outside apiRouter
	// and stay uncapped by design (they carry no request body).
	apiRouter.Use(middleware.MaxBody(1 << 20))
	apiRouter.NotFound(func(w http.ResponseWriter, r *http.Request) {
		problem.Write(w, r, problem.New(problem.NotFound, "no resource exists at this path"))
	})
	apiRouter.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		problem.Write(w, r, problem.New(problem.MethodNotAllowed, "this method is not supported for this resource"))
	})

	// The SSE stream is registered by hand on the API subrouter, OUTSIDE the
	// generated ServerInterface handler and its OpenAPI request-validation
	// middleware: an event stream is not a JSON request/response operation and
	// cannot be modeled by oapi-codegen, so it must bypass the validator to
	// stream (the same precedent as the /api/auth/* endpoints above). apiRouter
	// is mounted at /api/v1, so the route path is relative ("/events" →
	// /api/v1/events). It is guarded by RequireAuth — events require a logged-in
	// user — but NOT by publicHealth: unlike health, this endpoint is never
	// public. Recover (Use'd on apiRouter above) still wraps it.
	apiRouter.With(authr.RequireAuth).Get("/events", sseHandler(broker))

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

	// Only the BFF auth endpoints and the JSON API read or write the session,
	// so only those run through LoadAndSave. Static frontend assets and docs
	// are served WITHOUT it: wrapping the embedded file server in scs's
	// response writer churns Set-Cookie / Vary: Cookie on every asset and —
	// under the service worker's parallel precache fetches — races on the
	// response header map (a fatal "concurrent map writes"). Session-scoped
	// requests still get the full load/save; everything else bypasses it.
	sessioned := sm.LoadAndSave(r)
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if needsSession(req.URL.Path) {
			sessioned.ServeHTTP(w, req)
			return
		}
		r.ServeHTTP(w, req)
	})
}

// needsSession reports whether a request path is served through the session
// middleware: the BFF auth endpoints (/api/auth/*) and the JSON API (/api/v1).
// Static frontend assets, the OpenAPI docs, and problem pages do not touch the
// session and are served without it.
func needsSession(p string) bool {
	return strings.HasPrefix(p, "/api/auth/") || p == "/api/v1" || strings.HasPrefix(p, "/api/v1/")
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
