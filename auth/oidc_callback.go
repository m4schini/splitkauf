// SPDX-License-Identifier: CC0-1.0

package auth

import (
	"crypto/subtle"
	"net/http"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"go.uber.org/zap"

	"github.com/m4schini/splitkauf/members"
	"github.com/m4schini/splitkauf/ports/rest/problem"
)

// idTokenClaims is the subset of ID-token claims the login flow reads.
type idTokenClaims struct {
	Email             string `json:"email"`
	Name              string `json:"name"`
	PreferredUsername string `json:"preferred_username"` //nolint:tagliatelle // OIDC spec claim name
}

// validCallbackState compares the state query parameter against the session's
// pre-login state in constant time, writing a validation problem and returning
// false on mismatch.
func (a *oidcAuthenticator) validCallbackState(res http.ResponseWriter, req *http.Request) bool {
	wantState := a.sm.GetString(req.Context(), stateKey)

	gotState := req.URL.Query().Get("state")
	if wantState == "" || subtle.ConstantTimeCompare([]byte(wantState), []byte(gotState)) != 1 {
		// A missing session-side state almost always means the pre-login session
		// cookie set by Login was not sent back on the callback (cookie/domain/
		// SameSite/Secure problem, or a session store that lost the entry).
		a.logger.Warn("callback: state validation failed",
			zap.Bool("session_state_present", wantState != ""),
			zap.Bool("query_state_present", gotState != ""),
			zap.Bool("incoming_session_cookie", hasSessionCookie(a.sm, req)),
		)
		problem.Write(res, req, problem.New(problem.Validation, "invalid or missing state parameter"))

		return false
	}

	return true
}

// exchangeAndVerify exchanges the authorization code (with the PKCE verifier)
// for tokens, extracts and verifies the ID token, and checks its nonce in
// constant time. On any failure it writes the problem response and returns
// ok=false.
func (a *oidcAuthenticator) exchangeAndVerify(
	res http.ResponseWriter, req *http.Request, code, verifier, nonce string,
) (string, *oidc.IDToken, bool) {
	ctx := req.Context()

	token, err := a.oauth2Config.Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		a.logger.Warn("code exchange failed", zap.Error(err))
		problem.Write(res, req, problem.New(problem.Unavailable, "token exchange with the identity provider failed"))

		return "", nil, false
	}

	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok || rawIDToken == "" {
		problem.Write(res, req, problem.New(problem.Unavailable, "identity provider returned no ID token"))

		return "", nil, false
	}

	idToken, err := a.verifier.Verify(ctx, rawIDToken)
	if err != nil {
		a.logger.Warn("ID token verification failed", zap.Error(err))
		problem.Write(res, req, problem.New(problem.Unauthorized, "ID token verification failed"))

		return "", nil, false
	}

	if nonce == "" || subtle.ConstantTimeCompare([]byte(idToken.Nonce), []byte(nonce)) != 1 {
		a.logger.Warn("callback: ID token nonce mismatch",
			zap.Bool("session_nonce_present", nonce != ""),
			zap.Bool("token_nonce_present", idToken.Nonce != ""),
		)
		problem.Write(res, req, problem.New(problem.Unauthorized, "ID token nonce mismatch"))

		return "", nil, false
	}

	return rawIDToken, idToken, true
}

// readClaims decodes the claims used to build the session from the verified ID
// token, writing an internal problem and returning ok=false when decoding
// fails.
func (a *oidcAuthenticator) readClaims(
	res http.ResponseWriter, req *http.Request, idToken *oidc.IDToken,
) (idTokenClaims, bool) {
	var claims idTokenClaims
	if err := idToken.Claims(&claims); err != nil {
		problem.Write(res, req, problem.New(problem.Internal, "reading ID token claims"))

		return claims, false
	}

	return claims, true
}

// buildSessionData assembles the SessionData to store for a completed OIDC
// login: the resolved user id, the raw ID token (kept solely as the RP-
// initiated-logout hint), the subject, and the claims — preferring the name
// claim and falling back to preferred_username when it is empty.
func buildSessionData(rawIDToken string, idToken *oidc.IDToken, claims idTokenClaims) SessionData {
	name := claims.Name
	if name == "" {
		name = claims.PreferredUsername
	}

	return SessionData{
		UserID:  subjectUUID(idToken.Subject),
		IDToken: rawIDToken,
		Subject: idToken.Subject,
		Email:   claims.Email,
		Name:    name,
	}
}

// establishSession renews the session token (session-fixation prevention),
// stores data as the authenticated session state, and upserts the member. On
// any failure it writes the problem response and returns false.
func (a *oidcAuthenticator) establishSession(res http.ResponseWriter, req *http.Request, data SessionData) bool {
	ctx := req.Context()

	// Session-fixation prevention: issue a new session id before storing the
	// authenticated state.
	if err := a.sm.RenewToken(ctx); err != nil {
		problem.Write(res, req, problem.New(problem.Internal, "renewing session"))

		return false
	}

	if err := putSessionData(ctx, a.sm, data); err != nil {
		problem.Write(res, req, problem.New(problem.Internal, "storing session"))

		return false
	}

	if err := a.members.Upsert(ctx, members.Member{
		Subject:   data.Subject,
		UserID:    data.UserID,
		Email:     data.Email,
		Name:      data.Name,
		CreatedAt: time.Time{}, // set by the repository on upsert
		UpdatedAt: time.Time{}, // set by the repository on upsert
	}); err != nil {
		a.logger.Error("upserting member", zap.Error(err))
		problem.Write(res, req, problem.New(problem.Internal, "recording membership"))

		return false
	}

	return true
}
