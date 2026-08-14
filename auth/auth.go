// SPDX-License-Identifier: CC0-1.0

// Package auth is the authentication port for the splitkauf HTTP API. It
// exposes an Authenticator with two implementations selected by config: a
// Backend-for-Frontend OIDC flow (Authorization Code + PKCE, server-side
// sessions, transparent token refresh) for deployment, and a dev-auth
// implementation that injects a single fixed user for local development.
//
// Both implementations share the same surface — Login/Callback/Logout HTTP
// handlers and a RequireAuth middleware — plus the WithUser/UserFrom context
// helpers, so ports/rest reads the current user uniformly regardless of mode.
// In the OIDC flow the browser only ever holds an opaque, HttpOnly session
// cookie; access, refresh, and ID tokens never leave the server.
package auth

import (
	"context"
	"net/http"

	"github.com/alexedwards/scs/v2"
	"github.com/google/uuid"

	"github.com/m4schini/splitkauf/config"
	"github.com/m4schini/splitkauf/members"
	"github.com/m4schini/splitkauf/users"
)

// User is the authenticated principal placed in the request context by an
// Authenticator's RequireAuth middleware. ID is a stable UUID: the fixed dev
// user's UUID in dev-auth mode, or a UUIDv5 derived deterministically from the
// OIDC subject otherwise. Email may be empty when the provider omits it.
type User struct {
	ID    uuid.UUID
	Name  string
	Email string
}

// Authenticator abstracts the login flow and request authentication. The dev
// and OIDC implementations satisfy it; New selects one from config.
type Authenticator interface {
	// Login starts a sign-in. In OIDC mode it redirects to the identity
	// provider; in dev mode it is a no-op redirect home.
	Login(w http.ResponseWriter, r *http.Request)
	// Callback completes a sign-in (OIDC only); in dev mode it 404s.
	Callback(w http.ResponseWriter, r *http.Request)
	// Logout ends the session. In OIDC mode it destroys the server-side
	// session and, when supported, performs RP-initiated logout; in dev mode
	// it is a no-op redirect home.
	Logout(w http.ResponseWriter, r *http.Request)
	// RequireAuth wraps next so that only authenticated requests reach it,
	// placing the current User in the request context. Unauthenticated
	// requests receive a 401 problem response.
	RequireAuth(next http.Handler) http.Handler
}

// userContextKey is the private context key under which RequireAuth stores the
// authenticated User.
type userContextKey struct{}

// WithUser returns a copy of ctx carrying u as the authenticated user.
func WithUser(ctx context.Context, u User) context.Context {
	return context.WithValue(ctx, userContextKey{}, u)
}

// UserFrom returns the authenticated user stored in ctx and true, or the zero
// User and false when none is present. It is the uniform accessor used by the
// REST handlers in both auth modes.
func UserFrom(ctx context.Context) (User, bool) {
	u, ok := ctx.Value(userContextKey{}).(User)
	return u, ok
}

// New builds the Authenticator selected by cfg, applying the precedence
// OIDC → password → dev-auth: the OIDC BFF authenticator when
// cfg.IsOIDCEnabled() (discovering the provider via ctx), else the local
// username/password authenticator when cfg.IsPasswordEnabled(), else dev-auth.
// sm is the shared session manager (used by OIDC and password modes, ignored in
// dev mode); membersRepo receives the upsert of every account that signs in;
// usersRepo backs credential lookup in password mode (may be nil otherwise).
func New(ctx context.Context, cfg *config.Config, sm *scs.SessionManager, membersRepo members.Repository, usersRepo users.Repository) (Authenticator, error) {
	switch {
	case cfg.IsOIDCEnabled():
		return newOIDC(ctx, cfg, sm, membersRepo)
	case cfg.IsPasswordEnabled():
		return newPassword(sm, usersRepo, membersRepo), nil
	default:
		return newDev(), nil
	}
}
