// SPDX-License-Identifier: CC0-1.0

package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/alexedwards/scs/v2"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/m4schini/splitkauf/ports/rest/problem"
)

// sessionDataKey is the single scs key under which the authenticated session's
// state is stored as JSON. The browser cookie carries only the opaque session
// id; the SessionData never leaves the server.
const sessionDataKey = "auth_session"

// Pre-login scs keys. These transient values are written before redirecting to
// the identity provider and cleared on callback. They are kept out of
// SessionData because they only exist for the duration of one login round-trip.
const (
	stateKey    = "auth_state"
	nonceKey    = "auth_nonce"
	verifierKey = "auth_pkce_verifier"
	returnToKey = "auth_return_to"
)

// SessionData is the complete server-side authentication state for one
// signed-in session. Both auth modes store the same shape: the resolved user
// id (set once at login) plus the email/name claims used to build the
// request-context User. IDToken is retained solely as the id_token_hint for
// RP-initiated logout in OIDC mode (empty in password mode); Subject is kept
// for diagnostics/logging only. It is stored as JSON under a single scs key
// and is never exposed to the browser.
type SessionData struct {
	UserID  uuid.UUID `json:"user_id"`  //nolint:tagliatelle // persisted session format; renaming breaks sessions
	IDToken string    `json:"id_token"` //nolint:tagliatelle // persisted session format; renaming breaks sessions
	Subject string    `json:"subject"`
	Email   string    `json:"email"`
	Name    string    `json:"name"`
}

// getSessionData returns the SessionData stored in the current session and true,
// or the zero value and false when the session holds none (or holds an
// unparseable value). ctx must carry the scs session (via LoadAndSave).
func getSessionData(ctx context.Context, sessions *scs.SessionManager) (SessionData, bool) {
	var none SessionData

	raw := sessions.GetBytes(ctx, sessionDataKey)
	if len(raw) == 0 {
		return none, false
	}

	var data SessionData
	if err := json.Unmarshal(raw, &data); err != nil {
		return none, false
	}

	return data, true
}

// putSessionData stores d as JSON under the session-data key, replacing any
// previous value.
func putSessionData(ctx context.Context, sessions *scs.SessionManager, data SessionData) error {
	raw, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("marshaling session data: %w", err)
	}

	sessions.Put(ctx, sessionDataKey, raw)

	return nil
}

// requireSession is the shared RequireAuth implementation for the OIDC and
// password modes: it loads the SessionData, rejects requests without one (or
// with one lacking a resolved user id) with a 401 problem, and otherwise
// places the auth.User in the request context. It never contacts an identity
// provider; the scs session lifetime alone governs expiry.
func requireSession(sessions *scs.SessionManager, logger *zap.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {
			ctx := req.Context()

			data, ok := getSessionData(ctx, sessions)
			if !ok {
				// The 401 the browser sees on GET /api/v1/me. Whether the request
				// carried a session cookie tells the two failure modes apart: no
				// cookie -> the browser isn't sending it (cookie Secure/SameSite/
				// domain/path, or it was never set); cookie present but no data ->
				// the server-side session store has no matching session (e.g. an
				// in-memory store after a restart, or an expired/destroyed session).
				logger.Info("requireauth: no active session, returning 401",
					zap.String("path", req.URL.Path),
					zap.Bool("incoming_session_cookie", hasSessionCookie(sessions, req)),
				)
				problem.Write(res, req, problem.New(problem.Unauthorized, "no active session"))

				return
			}

			if data.UserID == uuid.Nil {
				// A session written before UserID existed in SessionData; the user
				// must sign in again once after the deploy.
				logger.Info("requireauth: session has no user id (pre-alignment session), returning 401",
					zap.String("path", req.URL.Path),
					zap.String("subject", data.Subject),
				)
				problem.Write(res, req, problem.New(problem.Unauthorized, "no active session"))

				return
			}

			user := User{ID: data.UserID, Name: data.Name, Email: data.Email}
			logger.Debug("requireauth: authenticated",
				zap.String("path", req.URL.Path),
				zap.String("subject", data.Subject),
			)
			next.ServeHTTP(res, req.WithContext(WithUser(ctx, user)))
		})
	}
}

// hasSessionCookie reports whether the request carries the scs session cookie.
// It only checks presence (not validity), for diagnostic logging of the login
// flow — a missing cookie on the callback or an API request is the usual cause
// of a lost session.
func hasSessionCookie(sessions *scs.SessionManager, req *http.Request) bool {
	_, err := req.Cookie(sessions.Cookie.Name)

	return err == nil
}
