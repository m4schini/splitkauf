// SPDX-License-Identifier: CC0-1.0

package auth

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/google/uuid"
	"golang.org/x/oauth2"

	"github.com/alexedwards/scs/v2"
	"go.uber.org/zap"

	"github.com/m4schini/splitkauf/config"
	"github.com/m4schini/splitkauf/members"
	"github.com/m4schini/splitkauf/ports/rest/problem"
	"github.com/m4schini/splitkauf/telemetry"
)

// subjectNamespace is a fixed, arbitrary UUID namespace used to derive a stable
// UUIDv5 for each OIDC subject. It only needs to be constant, not secret: the
// same subject always maps to the same User.ID.
//
//nolint:gochecknoglobals // fixed UUID namespace; uuid.UUID cannot be a Go const
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
func newOIDC(
	ctx context.Context, cfg *config.Config, sessionManager *scs.SessionManager, repo members.Repository,
) (*oidcAuthenticator, error) {
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
		EndSessionEndpoint string `json:"end_session_endpoint"` //nolint:tagliatelle // OIDC spec wire name
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
		zap.String("session_cookie_name", sessionManager.Cookie.Name),
		zap.Bool("session_cookie_secure", sessionManager.Cookie.Secure),
		zap.String("session_cookie_samesite", sameSiteString(sessionManager.Cookie.SameSite)),
	)

	verifierConfig := &oidc.Config{
		ClientID:                   cfg.Auth.OIDC.ClientID,
		SupportedSigningAlgs:       nil,   // library default (RS256)
		SkipClientIDCheck:          false, // full verification: audience,
		SkipExpiryCheck:            false, // expiry, and issuer are all checked
		SkipIssuerCheck:            false,
		Now:                        nil, // time.Now
		InsecureSkipSignatureCheck: false,
	}

	return &oidcAuthenticator{
		oauth2Config:          oauth2Config,
		verifier:              provider.Verifier(verifierConfig),
		sm:                    sessionManager,
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
func (a *oidcAuthenticator) Login(res http.ResponseWriter, req *http.Request) {
	ctx := req.Context()

	a.logger.Info("login: starting authorization code flow",
		zap.Bool("incoming_session_cookie", hasSessionCookie(a.sm, req)),
		zap.String("raw_return_to", req.URL.Query().Get("return_to")),
	)

	state, err := randomToken()
	if err != nil {
		a.logger.Error("login: generating state failed", zap.Error(err))
		problem.Write(res, req, problem.New(problem.Internal, "generating login state"))

		return
	}

	nonce, err := randomToken()
	if err != nil {
		a.logger.Error("login: generating nonce failed", zap.Error(err))
		problem.Write(res, req, problem.New(problem.Internal, "generating login nonce"))

		return
	}

	verifier := oauth2.GenerateVerifier()
	returnTo := safeReturnTo(req.URL.Query().Get("return_to"))

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
	http.Redirect(res, req, authURL, http.StatusFound)
}

// Callback validates state (constant-time), exchanges the code with the PKCE
// verifier, verifies the ID token and nonce, renews the session token (session
// fixation), stores the SessionData, upserts the member, clears the pre-login
// values, and redirects to the validated return_to. Each helper writes its own
// problem response and returns false when the flow must stop.
func (a *oidcAuthenticator) Callback(res http.ResponseWriter, req *http.Request) {
	ctx := req.Context()

	a.logger.Info("callback: received from identity provider",
		zap.Bool("incoming_session_cookie", hasSessionCookie(a.sm, req)),
		zap.Bool("has_state_param", req.URL.Query().Get("state") != ""),
		zap.Bool("has_code_param", req.URL.Query().Get("code") != ""),
		zap.String("provider_error", req.URL.Query().Get("error")),
	)

	if !a.validCallbackState(res, req) {
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

	code := req.URL.Query().Get("code")
	if code == "" {
		a.logger.Warn("callback: missing authorization code",
			zap.String("provider_error", req.URL.Query().Get("error")),
			zap.String("provider_error_description", req.URL.Query().Get("error_description")),
		)
		problem.Write(res, req, problem.New(problem.Validation, "missing authorization code"))

		return
	}

	rawIDToken, idToken, verified := a.exchangeAndVerify(res, req, code, verifier, nonce)
	if !verified {
		return
	}

	claims, ok := a.readClaims(res, req, idToken)
	if !ok {
		return
	}

	data := buildSessionData(rawIDToken, idToken, claims)
	if !a.establishSession(res, req, data) {
		return
	}

	a.logger.Info("callback: login complete, session established",
		zap.String("subject", idToken.Subject),
		zap.String("email", claims.Email),
		zap.String("name", data.Name),
		zap.String("return_to", returnTo),
	)
	http.Redirect(res, req, returnTo, http.StatusFound)
}

// Logout destroys the server-side session (which clears the cookie) and, when
// the provider advertises an end_session_endpoint, redirects to RP-initiated
// logout with the ID token hint and post-logout redirect; otherwise it
// redirects home.
func (a *oidcAuthenticator) Logout(res http.ResponseWriter, req *http.Request) {
	ctx := req.Context()

	data, _ := getSessionData(ctx, a.sm)
	idTokenHint := data.IDToken

	if err := a.sm.Destroy(ctx); err != nil {
		problem.Write(res, req, problem.New(problem.Internal, "destroying session"))

		return
	}

	if a.endSessionEndpoint == "" {
		http.Redirect(res, req, "/", http.StatusFound)

		return
	}

	endSession, err := url.Parse(a.endSessionEndpoint)
	if err != nil {
		a.logger.Warn("parsing end_session_endpoint", zap.Error(err))
		http.Redirect(res, req, "/", http.StatusFound)

		return
	}

	query := endSession.Query()
	if idTokenHint != "" {
		query.Set("id_token_hint", idTokenHint)
	}

	query.Set("client_id", a.clientID)

	if a.postLogoutRedirectURL != "" {
		query.Set("post_logout_redirect_uri", a.postLogoutRedirectURL)
	}

	endSession.RawQuery = query.Encode()
	http.Redirect(res, req, endSession.String(), http.StatusFound)
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
