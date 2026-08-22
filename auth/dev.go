// SPDX-License-Identifier: CC0-1.0

package auth

import (
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/m4schini/splitkauf/members"
)

// devUserID is the stable, hardcoded identifier for the dev user. It matches
// the M1 middleware.DevAuth user's UUID so lists/items created across the M1→M2
// transition remain attributed to the same principal.
//
//nolint:gochecknoglobals // fixed UUID constant; uuid.UUID cannot be a Go const
var devUserID = uuid.MustParse("00000000-0000-0000-0000-000000000001")

// DevUser is the fixed user injected by the dev authenticator. It is exported so
// the wiring layer can upsert it as a member at startup (dev-auth mode has no
// login through which membership would otherwise be recorded). Its Subject in
// the members table is the UUID string.
//
//nolint:gochecknoglobals // fixed dev principal; struct value cannot be a Go const
var DevUser = User{ID: devUserID, Name: "Dev User", Email: ""}

// DevMember returns the members.Member representation of the dev user, keyed by
// its UUID string, for the startup upsert in dev-auth mode.
func DevMember() members.Member {
	return members.Member{
		Subject:   DevUser.ID.String(),
		UserID:    DevUser.ID,
		Email:     DevUser.Email,
		Name:      DevUser.Name,
		CreatedAt: time.Time{}, // set by the repository on upsert
		UpdatedAt: time.Time{}, // set by the repository on upsert
	}
}

// devAuthenticator is the local-development Authenticator. It performs no
// credential check: RequireAuth unconditionally injects DevUser, and the
// login/callback/logout endpoints are inert (there is nothing to sign in to).
type devAuthenticator struct{}

// newDev builds the dev-auth Authenticator.
func newDev() *devAuthenticator { return &devAuthenticator{} }

// Login is a no-op in dev mode: there is no identity provider, so it simply
// redirects home.
func (d *devAuthenticator) Login(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/", http.StatusFound)
}

// Callback has no meaning without a login flow; it returns 404.
func (d *devAuthenticator) Callback(w http.ResponseWriter, r *http.Request) {
	http.NotFound(w, r)
}

// Logout is a no-op in dev mode: the dev user cannot be signed out. It redirects
// home.
func (d *devAuthenticator) Logout(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/", http.StatusFound)
}

// RequireAuth injects the fixed DevUser into the request context, so downstream
// handlers see the same authenticated principal on every request.
func (d *devAuthenticator) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r.WithContext(WithUser(r.Context(), DevUser)))
	})
}
