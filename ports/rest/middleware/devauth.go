// SPDX-License-Identifier: TODO

package middleware

import (
	"context"
	"net/http"

	"github.com/google/uuid"
	"github.com/m4schini/splitkauf/lists"
)

// TEMPORARY (M1 / US-A.1): DevAuth injects a single hardcoded dev user into
// every request. There is no login and no credential check — every request is
// the same user. This middleware, the fixed user, and the UserFrom helper are
// removed entirely in M2 (US-A.2) when real OIDC authentication lands.

// devUserID is the stable, hardcoded identifier for the M1 dev user. It is
// fixed so that lists/items created across restarts remain attributed to the
// same principal (there is no per-user ownership in M1, but /me must be stable).
var devUserID = uuid.MustParse("00000000-0000-0000-0000-000000000001")

// devUser is the fixed user injected by DevAuth.
var devUser = lists.User{ID: devUserID, Name: "Dev User"}

// userContextKey is the private context key under which DevAuth stores the user.
type userContextKey struct{}

// DevAuth is chi middleware that places the fixed dev user into the request
// context so downstream handlers (notably GET /me) can read it via UserFrom.
// It is unconditional: there is no authentication in M1.
func DevAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := context.WithValue(r.Context(), userContextKey{}, devUser)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// UserFrom returns the authenticated user stored in the context by DevAuth and
// true, or the zero user and false when none is present.
func UserFrom(ctx context.Context) (lists.User, bool) {
	u, ok := ctx.Value(userContextKey{}).(lists.User)
	return u, ok
}
