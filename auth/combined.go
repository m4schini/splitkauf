// SPDX-License-Identifier: CC0-1.0

package auth

import (
	"net/http"
)

// combinedAuthenticator offers both sign-in methods at once: the OIDC
// redirect flow and local username/password credentials, active when the OIDC
// provider is configured AND password auth is enabled. Both methods establish
// the same scs session (SessionData with a resolved UserID), so RequireAuth
// and the API behave identically regardless of how the user signed in; only
// login and logout dispatch on the method.
type combinedAuthenticator struct {
	oidc     *oidcAuthenticator
	password *passwordAuthenticator
}

// newCombined builds the combined Authenticator over the two mode
// implementations, which share the same session manager.
func newCombined(o *oidcAuthenticator, p *passwordAuthenticator) *combinedAuthenticator {
	return &combinedAuthenticator{oidc: o, password: p}
}

// Login dispatches on the method: a POST carries password credentials, any
// other method starts the OIDC redirect flow (the SPA's "Sign in with SSO"
// button navigates to GET /api/auth/login).
func (a *combinedAuthenticator) Login(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		a.password.Login(w, r)

		return
	}

	a.oidc.Login(w, r)
}

// Callback completes an OIDC sign-in; password logins have no redirect
// round-trip.
func (a *combinedAuthenticator) Callback(w http.ResponseWriter, r *http.Request) {
	a.oidc.Callback(w, r)
}

// Logout dispatches on how the session was established: a session holding an
// ID token came from the OIDC flow and gets RP-initiated logout (so the IdP's
// SSO session ends too); a password session is destroyed locally and
// redirected home — sending it to the IdP would end an IdP session the user
// never started here.
func (a *combinedAuthenticator) Logout(w http.ResponseWriter, r *http.Request) {
	data, _ := getSessionData(r.Context(), a.oidc.sm)
	if data.IDToken != "" {
		a.oidc.Logout(w, r)

		return
	}

	a.password.Logout(w, r)
}

// RequireAuth is the shared session-based middleware; both underlying modes
// already delegate to requireSession, so either delegate is equivalent.
func (a *combinedAuthenticator) RequireAuth(next http.Handler) http.Handler {
	return a.oidc.RequireAuth(next)
}
