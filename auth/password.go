// SPDX-License-Identifier: CC0-1.0

package auth

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

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
//
//nolint:gochecknoglobals // lazy-init cache backing dummyHash; cannot be a const
var (
	dummyHashOnce  sync.Once
	dummyHashValue string
)

func dummyHash() string {
	dummyHashOnce.Do(func() {
		// Any well-formed bcrypt hash at the default cost works; it must never
		// match a real password.
		hash, err := users.HashPassword("bcrypt-timing-equalizer-not-a-password")
		if err != nil {
			panic("auth: generating dummy bcrypt hash: " + err.Error())
		}

		dummyHashValue = hash
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
	// ipLimiter and userLimiter throttle credential POSTs per client IP and
	// per username (see ratelimit.go); bcryptSem caps concurrent bcrypt
	// comparisons so a login flood cannot monopolise every CPU.
	ipLimiter   *keyedLimiter
	userLimiter *keyedLimiter
	bcryptSem   chan struct{}
}

// newPassword builds the password Authenticator over the users and members
// repositories and the shared session manager.
func newPassword(
	sm *scs.SessionManager, usersRepo users.Repository, membersRepo members.Repository,
) *passwordAuthenticator {
	return &passwordAuthenticator{
		users:   usersRepo,
		members: membersRepo,
		sm:      sm,
		logger:  telemetry.Logger("auth", "password"),

		ipLimiter:   newKeyedLimiter(loginIPBurst, loginIPInterval, loginLimiterMaxKeys),
		userLimiter: newKeyedLimiter(loginUserBurst, loginUserInterval, loginLimiterMaxKeys),
		bcryptSem:   make(chan struct{}, runtime.NumCPU()),
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
// status nor the timing reveals whether the username exists. Credential POSTs
// are rate limited per client IP and per username (429 with Retry-After,
// checked before bcrypt), and concurrent bcrypt comparisons are capped.
func (a *passwordAuthenticator) Login(res http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		// The SPA owns the login form; a GET here is just a navigation.
		http.Redirect(res, req, "/", http.StatusFound)

		return
	}

	// Throttle per client IP before doing any work at all.
	ip := clientIP(req)
	if ok, wait := a.ipLimiter.allow(ip); !ok {
		a.logger.Warn("login: throttled", zap.String("remote_ip", ip), zap.String("reason", "ip_rate"))
		writeTooManyRequests(res, req, wait)

		return
	}

	req.Body = http.MaxBytesReader(res, req.Body, loginBodyLimit)

	var creds loginRequest

	dec := json.NewDecoder(req.Body)
	dec.DisallowUnknownFields()

	if err := dec.Decode(&creds); err != nil {
		problem.Write(res, req, problem.New(problem.Validation, "invalid login request body"))

		return
	}

	creds.Username = strings.TrimSpace(creds.Username)

	// Throttle per username (across IPs) before the bcrypt comparison.
	if ok, wait := a.userLimiter.allow(creds.Username); !ok {
		a.logger.Warn("login: throttled", zap.String("username", creds.Username), zap.String("reason", "user_rate"))
		writeTooManyRequests(res, req, wait)

		return
	}

	user, ok := a.authenticate(res, req, creds)
	if !ok {
		return
	}

	if !a.establishSession(res, req, user) {
		return
	}

	// A correct password clears this username's throttle so earlier typos
	// don't count against the legitimate user. The IP bucket is left alone.
	a.userLimiter.reset(creds.Username)

	a.logger.Info("login: session established",
		zap.String("username", user.Username),
		zap.String("subject", user.ID.String()),
	)
	res.WriteHeader(http.StatusNoContent)
}

// writeTooManyRequests writes the 429 problem with a Retry-After header (whole
// seconds, rounded up, at least 1).
func writeTooManyRequests(res http.ResponseWriter, req *http.Request, wait time.Duration) {
	secs := max(int(math.Ceil(wait.Seconds())), 1)

	res.Header().Set("Retry-After", strconv.Itoa(secs))
	problem.Write(res, req, problem.New(problem.TooManyRequests, "too many login attempts; retry later"))
}

// Callback has no meaning for password auth (there is no redirect round-trip).
func (a *passwordAuthenticator) Callback(res http.ResponseWriter, req *http.Request) {
	http.NotFound(res, req)
}

// Logout destroys the server-side session (clearing the cookie) and redirects
// home, matching the browser's top-level form-POST logout.
func (a *passwordAuthenticator) Logout(res http.ResponseWriter, req *http.Request) {
	if err := a.sm.Destroy(req.Context()); err != nil {
		a.logger.Error("logout: destroying session failed", zap.Error(err))
		problem.Write(res, req, problem.New(problem.Internal, "destroying session"))

		return
	}

	http.Redirect(res, req, "/", http.StatusFound)
}

// RequireAuth admits only requests carrying a valid session, via the shared
// requireSession middleware: it loads the SessionData and injects the
// auth.User, or returns a 401 problem. The scs session lifetime governs
// expiry.
func (a *passwordAuthenticator) RequireAuth(next http.Handler) http.Handler {
	return requireSession(a.sm, a.logger)(next)
}

// establishSession renews the session token (session-fixation prevention),
// stores the account's SessionData, and upserts the member. On any failure it
// writes the problem response and returns false.
func (a *passwordAuthenticator) establishSession(res http.ResponseWriter, req *http.Request, user users.User) bool {
	ctx := req.Context()

	// Session-fixation prevention: issue a fresh session id before storing the
	// authenticated state.
	if err := a.sm.RenewToken(ctx); err != nil {
		a.logger.Error("login: renewing session failed", zap.Error(err))
		problem.Write(res, req, problem.New(problem.Internal, "establishing session"))

		return false
	}

	// Reuse the shared SessionData shape; password sessions carry no ID token.
	data := SessionData{
		UserID:  user.ID,
		IDToken: "",
		Subject: user.ID.String(),
		Email:   user.Email,
		Name:    user.Name,
	}
	if err := putSessionData(ctx, a.sm, data); err != nil {
		a.logger.Error("login: storing session failed", zap.Error(err))
		problem.Write(res, req, problem.New(problem.Internal, "storing session"))

		return false
	}

	if err := a.members.Upsert(ctx, members.Member{
		Subject:   user.ID.String(),
		UserID:    user.ID,
		Email:     user.Email,
		Name:      user.Name,
		CreatedAt: time.Time{}, // set by the repository on upsert
		UpdatedAt: time.Time{}, // set by the repository on upsert
	}); err != nil {
		a.logger.Error("login: upserting member failed", zap.Error(err))
		problem.Write(res, req, problem.New(problem.Internal, "recording membership"))

		return false
	}

	return true
}

// authenticate looks up the account and checks the password. Any credential
// failure — unknown user or wrong password — writes the same 401 problem, and
// both paths perform a bcrypt comparison (an unknown user is compared against
// dummyHash), so neither the status nor the timing reveals whether the username
// exists. On failure it writes the response and returns false.
func (a *passwordAuthenticator) authenticate(
	res http.ResponseWriter, req *http.Request, creds loginRequest,
) (users.User, bool) {
	user, hash, err := a.users.GetByUsername(req.Context(), creds.Username)

	hashFn, reason := func() string { return hash }, "bad_password"

	if err != nil {
		if !errors.Is(err, users.ErrNotFound) {
			// A real lookup failure (DB down) is not an auth decision.
			a.logger.Error("login: user lookup failed", zap.Error(err))
			problem.Write(res, req, problem.New(problem.Unavailable, "could not verify credentials"))

			return users.User{}, false
		}

		hashFn, reason = dummyHash, "unknown_user"
	}

	match, acquired := a.verifyPassword(req.Context(), hashFn, creds.Password)
	if !acquired {
		// The request context ended while waiting for a bcrypt slot.
		problem.Write(res, req, problem.New(problem.Unavailable, "could not verify credentials"))

		return users.User{}, false
	}

	if err != nil || !match {
		a.logger.Info("login: rejected", zap.String("username", creds.Username), zap.String("reason", reason))
		problem.Write(res, req, problem.New(problem.Unauthorized, "invalid username or password"))

		return users.User{}, false
	}

	return user, true
}

// verifyPassword runs the bcrypt comparison while holding a bcryptSem slot, so
// at most NumCPU comparisons run at once. hash is resolved only after the slot
// is acquired (dummyHash may itself run bcrypt on first use). acquired is false
// when the request context ended while waiting; no comparison ran then.
func (a *passwordAuthenticator) verifyPassword(
	ctx context.Context, hash func() string, password string,
) (match, acquired bool) {
	select {
	case a.bcryptSem <- struct{}{}:
	case <-ctx.Done():
		return false, false
	}

	defer func() { <-a.bcryptSem }()

	return users.VerifyPassword(hash(), password), true
}
