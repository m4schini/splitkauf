// SPDX-License-Identifier: CC0-1.0

package auth

import (
	"context"
	"crypto/subtle"
	"fmt"
	"net/http"
	"net/url"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/google/uuid"
	"golang.org/x/oauth2"

	"github.com/alexedwards/scs/v2"
	"github.com/m4schini/splitkauf/config"
	"github.com/m4schini/splitkauf/members"
	"github.com/m4schini/splitkauf/ports/rest/problem"
	"github.com/m4schini/splitkauf/telemetry"
	"go.uber.org/zap"
)

// subjectNamespace is a fixed, arbitrary UUID namespace used to derive a stable
// UUIDv5 for each OIDC subject. It only needs to be constant, not secret: the
// same subject always maps to the same User.ID.
var subjectNamespace = uuid.MustParse("6f9619ff-8b86-d011-b42d-00c04fc964ff")

// oidcAuthenticator is the Backend-for-Frontend OIDC Authenticator: it runs the
// Authorization Code + PKCE flow as a confidential client, using the identity
// provider only to authenticate the user at login. The resulting server-side
// scs session carries no OAuth tokens — only the resolved user id, the claims,
// and the ID token retained as the RP-initiated-logout hint. The browser only
// ever holds the opaque session cookie.
type oidcAuthenticator struct {
	oauth2Config *oauth2.Config
	verifier     *oidc.IDTokenVerifier
	sm           *scs.SessionManager
	members      members.Repository

	clientID              string
	endSessionEndpoint    string
	postLogoutRedirectURL string
	logger                *zap.Logger
}

// newOIDC discovers the provider at cfg's issuer, builds the confidential-client
// oauth2.Config and an ID-token verifier, and returns the OIDC Authenticator.
// ctx is used for provider discovery only.
func newOIDC(ctx context.Context, cfg *config.Config, sm *scs.SessionManager, repo members.Repository) (*oidcAuthenticator, error) {
	provider, err := oidc.NewProvider(ctx, cfg.Auth.OIDC.Issuer)
	if err != nil {
		return nil, fmt.Errorf("discovering OIDC provider: %w", err)
	}

	oauth2Config := &oauth2.Config{
		ClientID:     cfg.Auth.OIDC.ClientID,
		ClientSecret: cfg.Auth.OIDC.ClientSecret,
		RedirectURL:  cfg.Auth.OIDC.RedirectURL,
		Endpoint:     provider.Endpoint(),
		// Authentication-only scopes: the IdP's job ends at sign-in, so no
		// scope that would keep tokens alive beyond login is requested.
		Scopes: []string{oidc.ScopeOpenID, "profile", "email"},
	}

	// Pull the RP-initiated-logout endpoint from the discovery document, if the
	// provider advertises one.
	var meta struct {
		EndSessionEndpoint string `json:"end_session_endpoint"`
	}
	if err := provider.Claims(&meta); err != nil {
		return nil, fmt.Errorf("reading provider metadata: %w", err)
	}

	logger := telemetry.Logger("auth", "oidc")
	logger.Info("OIDC authenticator initialized",
		zap.String("issuer", cfg.Auth.OIDC.Issuer),
		zap.String("client_id", cfg.Auth.OIDC.ClientID),
		zap.String("redirect_url", cfg.Auth.OIDC.RedirectURL),
		zap.String("auth_endpoint", provider.Endpoint().AuthURL),
		zap.String("token_endpoint", provider.Endpoint().TokenURL),
		zap.Bool("end_session_endpoint_advertised", meta.EndSessionEndpoint != ""),
		zap.String("session_cookie_name", sm.Cookie.Name),
		zap.Bool("session_cookie_secure", sm.Cookie.Secure),
		zap.String("session_cookie_samesite", sameSiteString(sm.Cookie.SameSite)),
	)

	return &oidcAuthenticator{
		oauth2Config:          oauth2Config,
		verifier:              provider.Verifier(&oidc.Config{ClientID: cfg.Auth.OIDC.ClientID}),
		sm:                    sm,
		members:               repo,
		clientID:              cfg.Auth.OIDC.ClientID,
		endSessionEndpoint:    meta.EndSessionEndpoint,
		postLogoutRedirectURL: cfg.Auth.OIDC.PostLogoutRedirectURL,
		logger:                logger,
	}, nil
}

// Login starts an Authorization Code + PKCE (S256) flow: it generates a fresh
// PKCE verifier, state, and nonce, stores them plus an open-redirect-safe
// return_to in the session, and redirects to the identity provider's
// authorization endpoint.
func (a *oidcAuthenticator) Login(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	a.logger.Info("login: starting authorization code flow",
		zap.Bool("incoming_session_cookie", hasSessionCookie(a.sm, r)),
		zap.String("raw_return_to", r.URL.Query().Get("return_to")),
	)

	state, err := randomToken()
	if err != nil {
		a.logger.Error("login: generating state failed", zap.Error(err))
		problem.Write(w, r, problem.New(problem.Internal, "generating login state"))
		return
	}
	nonce, err := randomToken()
	if err != nil {
		a.logger.Error("login: generating nonce failed", zap.Error(err))
		problem.Write(w, r, problem.New(problem.Internal, "generating login nonce"))
		return
	}
	verifier := oauth2.GenerateVerifier()
	returnTo := safeReturnTo(r.URL.Query().Get("return_to"))

	a.sm.Put(ctx, stateKey, state)
	a.sm.Put(ctx, nonceKey, nonce)
	a.sm.Put(ctx, verifierKey, verifier)
	a.sm.Put(ctx, returnToKey, returnTo)

	authURL := a.oauth2Config.AuthCodeURL(state,
		oauth2.S256ChallengeOption(verifier),
		oidc.Nonce(nonce),
	)
	a.logger.Info("login: session primed, redirecting to identity provider",
		zap.String("return_to", returnTo),
		zap.String("redirect_uri", a.oauth2Config.RedirectURL),
	)
	// The full authorization URL (carries state/nonce/PKCE challenge, all
	// single-use and non-secret) is logged at debug for deep troubleshooting.
	a.logger.Debug("login: authorization URL", zap.String("auth_url", authURL))
	http.Redirect(w, r, authURL, http.StatusFound)
}

// Callback validates state (constant-time), exchanges the code with the PKCE
// verifier, verifies the ID token and nonce, renews the session token (session
// fixation), stores the SessionData, upserts the member, clears the pre-login
// values, and redirects to the validated return_to.
func (a *oidcAuthenticator) Callback(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	a.logger.Info("callback: received from identity provider",
		zap.Bool("incoming_session_cookie", hasSessionCookie(a.sm, r)),
		zap.Bool("has_state_param", r.URL.Query().Get("state") != ""),
		zap.Bool("has_code_param", r.URL.Query().Get("code") != ""),
		zap.String("provider_error", r.URL.Query().Get("error")),
	)

	wantState := a.sm.GetString(ctx, stateKey)
	gotState := r.URL.Query().Get("state")
	if wantState == "" || subtle.ConstantTimeCompare([]byte(wantState), []byte(gotState)) != 1 {
		// A missing session-side state almost always means the pre-login session
		// cookie set by Login was not sent back on the callback (cookie/domain/
		// SameSite/Secure problem, or a session store that lost the entry).
		a.logger.Warn("callback: state validation failed",
			zap.Bool("session_state_present", wantState != ""),
			zap.Bool("query_state_present", gotState != ""),
			zap.Bool("incoming_session_cookie", hasSessionCookie(a.sm, r)),
		)
		problem.Write(w, r, problem.New(problem.Validation, "invalid or missing state parameter"))
		return
	}

	verifier := a.sm.GetString(ctx, verifierKey)
	nonce := a.sm.GetString(ctx, nonceKey)
	returnTo := safeReturnTo(a.sm.GetString(ctx, returnToKey))

	// The pre-login values are single-use and have now been read into locals.
	// Clear them immediately so that every subsequent path — a failed exchange
	// or verification just as much as success — leaves no state/nonce/verifier/
	// return_to lingering in the session.
	a.sm.Remove(ctx, stateKey)
	a.sm.Remove(ctx, nonceKey)
	a.sm.Remove(ctx, verifierKey)
	a.sm.Remove(ctx, returnToKey)

	code := r.URL.Query().Get("code")
	if code == "" {
		a.logger.Warn("callback: missing authorization code",
			zap.String("provider_error", r.URL.Query().Get("error")),
			zap.String("provider_error_description", r.URL.Query().Get("error_description")),
		)
		problem.Write(w, r, problem.New(problem.Validation, "missing authorization code"))
		return
	}

	token, err := a.oauth2Config.Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		a.logger.Warn("code exchange failed", zap.Error(err))
		problem.Write(w, r, problem.New(problem.Unavailable, "token exchange with the identity provider failed"))
		return
	}

	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok || rawIDToken == "" {
		problem.Write(w, r, problem.New(problem.Unavailable, "identity provider returned no ID token"))
		return
	}
	idToken, err := a.verifier.Verify(ctx, rawIDToken)
	if err != nil {
		a.logger.Warn("ID token verification failed", zap.Error(err))
		problem.Write(w, r, problem.New(problem.Unauthorized, "ID token verification failed"))
		return
	}
	if nonce == "" || subtle.ConstantTimeCompare([]byte(idToken.Nonce), []byte(nonce)) != 1 {
		a.logger.Warn("callback: ID token nonce mismatch",
			zap.Bool("session_nonce_present", nonce != ""),
			zap.Bool("token_nonce_present", idToken.Nonce != ""),
		)
		problem.Write(w, r, problem.New(problem.Unauthorized, "ID token nonce mismatch"))
		return
	}

	var claims struct {
		Email             string `json:"email"`
		Name              string `json:"name"`
		PreferredUsername string `json:"preferred_username"`
	}
	if err := idToken.Claims(&claims); err != nil {
		problem.Write(w, r, problem.New(problem.Internal, "reading ID token claims"))
		return
	}
	name := claims.Name
	if name == "" {
		name = claims.PreferredUsername
	}

	// Session-fixation prevention: issue a new session id before storing the
	// authenticated state.
	if err := a.sm.RenewToken(ctx); err != nil {
		problem.Write(w, r, problem.New(problem.Internal, "renewing session"))
		return
	}

	data := SessionData{
		UserID:  subjectUUID(idToken.Subject),
		IDToken: rawIDToken,
		Subject: idToken.Subject,
		Email:   claims.Email,
		Name:    name,
	}
	if err := putSessionData(ctx, a.sm, data); err != nil {
		problem.Write(w, r, problem.New(problem.Internal, "storing session"))
		return
	}

	if err := a.members.Upsert(ctx, members.Member{
		Subject: idToken.Subject,
		UserID:  subjectUUID(idToken.Subject),
		Email:   claims.Email,
		Name:    name,
	}); err != nil {
		a.logger.Error("upserting member", zap.Error(err))
		problem.Write(w, r, problem.New(problem.Internal, "recording membership"))
		return
	}

	a.logger.Info("callback: login complete, session established",
		zap.String("subject", idToken.Subject),
		zap.String("email", claims.Email),
		zap.String("name", name),
		zap.String("return_to", returnTo),
	)
	http.Redirect(w, r, returnTo, http.StatusFound)
}

// Logout destroys the server-side session (which clears the cookie) and, when
// the provider advertises an end_session_endpoint, redirects to RP-initiated
// logout with the ID token hint and post-logout redirect; otherwise it
// redirects home.
func (a *oidcAuthenticator) Logout(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	data, _ := getSessionData(ctx, a.sm)
	idTokenHint := data.IDToken

	if err := a.sm.Destroy(ctx); err != nil {
		problem.Write(w, r, problem.New(problem.Internal, "destroying session"))
		return
	}

	if a.endSessionEndpoint == "" {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}

	u, err := url.Parse(a.endSessionEndpoint)
	if err != nil {
		a.logger.Warn("parsing end_session_endpoint", zap.Error(err))
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}
	q := u.Query()
	if idTokenHint != "" {
		q.Set("id_token_hint", idTokenHint)
	}
	q.Set("client_id", a.clientID)
	if a.postLogoutRedirectURL != "" {
		q.Set("post_logout_redirect_uri", a.postLogoutRedirectURL)
	}
	u.RawQuery = q.Encode()
	http.Redirect(w, r, u.String(), http.StatusFound)
}

// RequireAuth admits only requests carrying a valid session, via the shared
// requireSession middleware: it loads the SessionData and injects the
// auth.User, or returns a 401 problem. It never contacts the identity
// provider; the scs session lifetime alone governs expiry.
func (a *oidcAuthenticator) RequireAuth(next http.Handler) http.Handler {
	return requireSession(a.sm, a.logger)(next)
}

// subjectUUID derives a stable UUIDv5 from an OIDC subject so that the same
// account always maps to the same User.ID (used as the API's user id).
func subjectUUID(subject string) uuid.UUID {
	return uuid.NewSHA1(subjectNamespace, []byte(subject))
}

// sameSiteString renders an http.SameSite mode for logging (the type has no
// String method of its own).
func sameSiteString(s http.SameSite) string {
	switch s {
	case http.SameSiteDefaultMode:
		return "default"
	case http.SameSiteLaxMode:
		return "lax"
	case http.SameSiteStrictMode:
		return "strict"
	case http.SameSiteNoneMode:
		return "none"
	default:
		return "unknown"
	}
}
