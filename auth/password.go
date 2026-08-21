// SPDX-License-Identifier: CC0-1.0

package auth

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"

	"github.com/alexedwards/scs/v2"
	"go.uber.org/zap"

	"github.com/m4schini/splitkauf/members"
	"github.com/m4schini/splitkauf/ports/rest/problem"
	"github.com/m4schini/splitkauf/telemetry"
	"github.com/m4schini/splitkauf/users"
)

// loginBodyLimit caps the credential POST body. It is small on purpose: the
// body is only {"username","password"} and these /api/auth/* routes sit outside
// the /api/v1 MaxBody middleware, so the handler limits its own read.
const loginBodyLimit = 4 << 10 // 4 KiB

// dummyHash returns a valid bcrypt hash of a fixed non-credential string,
// compared against when a username is unknown so a login for a non-existent user
// takes the same time as one for an existing user — closing a timing side
// channel that would otherwise leak which usernames exist. It is computed once,
// lazily, on first use (not in package init), so dev/OIDC builds and test
// binaries that never log in don't pay the bcrypt cost.
var (
	dummyHashOnce  sync.Once
	dummyHashValue string
)

func dummyHash() string {
	dummyHashOnce.Do(func() {
		// Any well-formed bcrypt hash at the default cost works; it must never
		// match a real password.
		h, err := users.HashPassword("bcrypt-timing-equalizer-not-a-password")
		if err != nil {
			panic("auth: generating dummy bcrypt hash: " + err.Error())
		}
		dummyHashValue = h
	})
	return dummyHashValue
}

// passwordAuthenticator is the local username/password Authenticator. It
// validates credentials against the users repository, then establishes the same
// scs server-side session the OIDC flow uses, so RequireAuth, logout, and
// durable Postgres sessions behave identically. There
// is no self-registration: accounts are provisioned out of band via the
// `user add` CLI.
type passwordAuthenticator struct {
	users   users.Repository
	members members.Repository
	sm      *scs.SessionManager
	logger  *zap.Logger
}

// newPassword builds the password Authenticator over the users and members
// repositories and the shared session manager.
func newPassword(sm *scs.SessionManager, usersRepo users.Repository, membersRepo members.Repository) *passwordAuthenticator {
	return &passwordAuthenticator{
		users:   usersRepo,
		members: membersRepo,
		sm:      sm,
		logger:  telemetry.Logger("auth", "password"),
	}
}

// loginRequest is the credential POST body.
type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// Login handles both the credential POST (validate → establish session → 204)
// and a plain GET, which just redirects home so the SPA can render the login
// form. Any credential failure — unknown user or wrong password — returns the
// same 401 problem, and both paths perform a bcrypt comparison, so neither the
// status nor the timing reveals whether the username exists.
func (a *passwordAuthenticator) Login(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		// The SPA owns the login form; a GET here is just a navigation.
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}

	ctx := r.Context()

	r.Body = http.MaxBytesReader(w, r.Body, loginBodyLimit)
	var req loginRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		problem.Write(w, r, problem.New(problem.Validation, "invalid login request body"))
		return
	}
	req.Username = strings.TrimSpace(req.Username)

	user, hash, err := a.users.GetByUsername(ctx, req.Username)
	if err != nil {
		if !errors.Is(err, users.ErrNotFound) {
			// A real lookup failure (DB down) is not an auth decision.
			a.logger.Error("login: user lookup failed", zap.Error(err))
			problem.Write(w, r, problem.New(problem.Unavailable, "could not verify credentials"))
			return
		}
		// Unknown user: still run a bcrypt comparison against the dummy hash so
		// the response time matches the found-user path, then fail the same way.
		users.VerifyPassword(dummyHash(), req.Password)
		a.logger.Info("login: rejected", zap.String("username", req.Username), zap.String("reason", "unknown_user"))
		problem.Write(w, r, problem.New(problem.Unauthorized, "invalid username or password"))
		return
	}

	if !users.VerifyPassword(hash, req.Password) {
		a.logger.Info("login: rejected", zap.String("username", req.Username), zap.String("reason", "bad_password"))
		problem.Write(w, r, problem.New(problem.Unauthorized, "invalid username or password"))
		return
	}

	// Session-fixation prevention: issue a fresh session id before storing the
	// authenticated state.
	if err := a.sm.RenewToken(ctx); err != nil {
		a.logger.Error("login: renewing session failed", zap.Error(err))
		problem.Write(w, r, problem.New(problem.Internal, "establishing session"))
		return
	}

	// Reuse the shared SessionData shape; password sessions carry no ID token.
	data := SessionData{
		UserID:  user.ID,
		Subject: user.ID.String(),
		Email:   user.Email,
		Name:    user.Name,
	}
	if err := putSessionData(ctx, a.sm, data); err != nil {
		a.logger.Error("login: storing session failed", zap.Error(err))
		problem.Write(w, r, problem.New(problem.Internal, "storing session"))
		return
	}

	if err := a.members.Upsert(ctx, members.Member{
		Subject: user.ID.String(),
		UserID:  user.ID,
		Email:   user.Email,
		Name:    user.Name,
	}); err != nil {
		a.logger.Error("login: upserting member failed", zap.Error(err))
		problem.Write(w, r, problem.New(problem.Internal, "recording membership"))
		return
	}

	a.logger.Info("login: session established", zap.String("username", user.Username), zap.String("subject", user.ID.String()))
	w.WriteHeader(http.StatusNoContent)
}

// Callback has no meaning for password auth (there is no redirect round-trip).
func (a *passwordAuthenticator) Callback(w http.ResponseWriter, r *http.Request) {
	http.NotFound(w, r)
}

// Logout destroys the server-side session (clearing the cookie) and redirects
// home, matching the browser's top-level form-POST logout.
func (a *passwordAuthenticator) Logout(w http.ResponseWriter, r *http.Request) {
	if err := a.sm.Destroy(r.Context()); err != nil {
		a.logger.Error("logout: destroying session failed", zap.Error(err))
		problem.Write(w, r, problem.New(problem.Internal, "destroying session"))
		return
	}
	http.Redirect(w, r, "/", http.StatusFound)
}

// RequireAuth admits only requests carrying a valid session, via the shared
// requireSession middleware: it loads the SessionData and injects the
// auth.User, or returns a 401 problem. The scs session lifetime governs
// expiry.
func (a *passwordAuthenticator) RequireAuth(next http.Handler) http.Handler {
	return requireSession(a.sm, a.logger)(next)
}
