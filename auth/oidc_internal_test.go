// SPDX-License-Identifier: CC0-1.0

package auth

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alexedwards/scs/v2"
	"github.com/coreos/go-oidc/v3/oidc"
	"go.uber.org/zap"
	"golang.org/x/oauth2"

	"github.com/m4schini/splitkauf/members"
	"github.com/m4schini/splitkauf/ports/rest/problem"
)

// errCallbackUpsert is the failure a members repository reports when the test
// exercises the "recording membership" branch.
var errCallbackUpsert = errors.New("members table unavailable")

// errCallbackStore is the failure the session store reports when the test
// exercises the "renewing session" branch.
var errCallbackStore = errors.New("session store unavailable")

// flakyStore is an in-memory scs.Store that can be told to fail Delete — the
// call RenewToken makes to retire the pre-login session id.
type flakyStore struct {
	mu        sync.Mutex
	data      map[string][]byte
	deleteErr error
}

func newFlakyStore() *flakyStore {
	return &flakyStore{
		mu:        sync.Mutex{},
		data:      make(map[string][]byte),
		deleteErr: nil,
	}
}

func (s *flakyStore) Find(token string) ([]byte, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	b, found := s.data[token]

	return b, found, nil
}

func (s *flakyStore) Commit(token string, b []byte, _ time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.data[token] = b

	return nil
}

func (s *flakyStore) Delete(token string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.deleteErr != nil {
		return s.deleteErr
	}

	delete(s.data, token)

	return nil
}

// fail makes every subsequent Delete report err.
func (s *flakyStore) fail(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.deleteErr = err
}

// callbackIDP is the fake identity provider the Callback tests run against: a
// token endpoint whose reply the test picks, plus a record of what the handler
// posted to it (so a test can assert the PKCE verifier travelled, or that the
// endpoint was never reached at all).
type callbackIDP struct {
	issuer string

	mu     sync.Mutex
	calls  int
	form   url.Values
	status int
	body   []byte
}

// newCallbackIDP starts the fake token endpoint and returns it configured to
// reply 200 with an empty body; every test overrides that with respond.
func newCallbackIDP(t *testing.T) *callbackIDP {
	t.Helper()

	idp := new(callbackIDP)
	idp.status = http.StatusOK

	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(res http.ResponseWriter, req *http.Request) {
		if err := req.ParseForm(); err != nil {
			t.Errorf("token endpoint: ParseForm: %v", err)
		}

		idp.mu.Lock()
		idp.calls++
		idp.form = req.PostForm
		status, body := idp.status, idp.body
		idp.mu.Unlock()

		res.Header().Set("Content-Type", "application/json")
		res.WriteHeader(status)
		_, _ = res.Write(body)
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	idp.issuer = srv.URL

	return idp
}

// respond sets the status and body the token endpoint replies with.
func (i *callbackIDP) respond(status int, body []byte) {
	i.mu.Lock()
	defer i.mu.Unlock()

	i.status = status
	i.body = body
}

// observed reports how often the token endpoint was called and the form the
// last call carried.
func (i *callbackIDP) observed() (int, url.Values) {
	i.mu.Lock()
	defer i.mu.Unlock()

	return i.calls, i.form
}

// recordingMembers is a members.Repository that captures the upserted member
// and optionally fails, standing in for the Postgres adapter.
type recordingMembers struct {
	mu     sync.Mutex
	err    error
	calls  int
	member members.Member
}

func (r *recordingMembers) Upsert(_ context.Context, member members.Member) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.calls++
	r.member = member

	return r.err
}

func (r *recordingMembers) Get(context.Context, string) (members.Member, error) {
	return members.Member{}, members.ErrNotFound
}

// upserted returns the recorded call count and the last member written.
func (r *recordingMembers) upserted() (int, members.Member) {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.calls, r.member
}

// newCallbackRSAKey generates the key the fake provider signs ID tokens with.
func newCallbackRSAKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}

	return key
}

// signCallbackToken mints a compact RS256 JWT carrying claims.
func signCallbackToken(t *testing.T, key *rsa.PrivateKey, claims map[string]any) string {
	t.Helper()

	segment := func(value any) string {
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}

		return base64.RawURLEncoding.EncodeToString(raw)
	}

	signingInput := segment(map[string]any{"alg": "RS256", "typ": "JWT"}) + "." + segment(claims)
	digest := sha256.Sum256([]byte(signingInput))

	signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatalf("SignPKCS1v15: %v", err)
	}

	return signingInput + "." + base64.RawURLEncoding.EncodeToString(signature)
}

// callbackClaims builds a complete, currently valid ID token payload; a test
// overrides only the claim it cares about.
func callbackClaims(issuer, audience, subject, nonce string) map[string]any {
	return map[string]any{
		"iss":   issuer,
		"aud":   audience,
		"sub":   subject,
		"exp":   time.Now().Add(time.Hour).Unix(),
		"iat":   time.Now().Add(-time.Minute).Unix(),
		"nonce": nonce,
	}
}

// callbackTokenResponse encodes the token endpoint's success body around an
// id_token value (a signed JWT, or whatever malformed value a test wants).
func callbackTokenResponse(t *testing.T, idToken any, omitIDToken bool) []byte {
	t.Helper()

	body := map[string]any{
		"access_token": "access-token",
		"token_type":   "Bearer",
		"expires_in":   3600,
	}
	if !omitIDToken {
		body["id_token"] = idToken
	}

	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	return raw
}

// newCallbackAuthenticator wires an oidcAuthenticator (and its session manager)
// to the fake identity provider, verifying ID tokens against publicKey.
func newCallbackAuthenticator(
	t *testing.T, idp *callbackIDP, clientID string, publicKey *rsa.PublicKey, repo members.Repository,
) (*oidcAuthenticator, *scs.SessionManager) {
	t.Helper()

	sessions := scs.New()
	verifier := oidc.NewVerifier(idp.issuer, &oidc.StaticKeySet{
		PublicKeys: []crypto.PublicKey{publicKey},
	}, &oidc.Config{
		ClientID:                   clientID,
		SupportedSigningAlgs:       []string{"RS256"},
		SkipClientIDCheck:          false,
		SkipExpiryCheck:            false,
		SkipIssuerCheck:            false,
		Now:                        nil,
		InsecureSkipSignatureCheck: false,
	})

	authenticator := &oidcAuthenticator{
		oauth2Config: &oauth2.Config{
			ClientID:     clientID,
			ClientSecret: "client-secret",
			Endpoint: oauth2.Endpoint{
				AuthURL:       idp.issuer + "/authorize",
				DeviceAuthURL: "",
				TokenURL:      idp.issuer + "/token",
				AuthStyle:     oauth2.AuthStyleInParams,
			},
			RedirectURL: "https://app.example.com/api/auth/callback",
			Scopes:      []string{"openid", "profile", "email"},
		},
		verifier:              verifier,
		sm:                    sessions,
		members:               repo,
		clientID:              clientID,
		endSessionEndpoint:    idp.issuer + "/logout",
		postLogoutRedirectURL: "https://app.example.com/",
		logger:                zap.NewNop(),
	}

	return authenticator, sessions
}

// seedCallbackSessionVerifier is the PKCE verifier every seedCallbackSession
// caller stashes; callers that assert on it read this constant directly.
const seedCallbackSessionVerifier = "pkce-code-verifier"

// seedCallbackSession stashes the four pre-login values Login writes and
// returns the session token to send as the callback's cookie.
func seedCallbackSession(t *testing.T, sessions *scs.SessionManager, state, nonce, returnTo string) string {
	t.Helper()

	return seedSession(t, sessions, func(ctx context.Context) {
		sessions.Put(ctx, stateKey, state)
		sessions.Put(ctx, nonceKey, nonce)
		sessions.Put(ctx, verifierKey, seedCallbackSessionVerifier)
		sessions.Put(ctx, returnToKey, returnTo)
	})
}

// assertPreLoginCleared fails unless all four single-use pre-login values are
// gone from the session behind token.
func assertPreLoginCleared(t *testing.T, sessions *scs.SessionManager, token string) {
	t.Helper()

	ctx, err := sessions.Load(context.Background(), token)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	for _, key := range []string{stateKey, nonceKey, verifierKey, returnToKey} {
		if sessions.Exists(ctx, key) {
			t.Errorf("session still holds %q = %q, want it removed after the callback",
				key, sessions.GetString(ctx, key))
		}
	}
}

// renewedSessionToken returns the session token the response set on the
// browser, which must differ from the pre-login one (session fixation).
func renewedSessionToken(t *testing.T, rec *httptest.ResponseRecorder, name string) string {
	t.Helper()

	for _, line := range rec.Header().Values("Set-Cookie") {
		cookie, err := http.ParseSetCookie(line)
		if err != nil {
			t.Fatalf("ParseSetCookie(%q): %v", line, err)
		}

		if cookie.Name == name {
			return cookie.Value
		}
	}

	t.Fatalf("response set no %q cookie, want the renewed session token", name)

	return ""
}

// assertCallbackProblem checks the RFC-9457 body the handler wrote.
func assertCallbackProblem(t *testing.T, rec *httptest.ResponseRecorder, wantType problem.Type, wantDetail string) {
	t.Helper()

	if rec.Code != wantType.Status {
		t.Errorf("status = %d, want %d (body: %q)", rec.Code, wantType.Status, rec.Body.String())
	}

	if got := rec.Header().Get("Content-Type"); got != problem.ContentType {
		t.Errorf("Content-Type = %q, want %q", got, problem.ContentType)
	}

	var got problem.Problem

	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decoding problem body %q: %v", rec.Body.String(), err)
	}

	if got.Detail != wantDetail {
		t.Errorf("problem detail = %q, want %q", got.Detail, wantDetail)
	}

	if got.Type != wantType.URI() {
		t.Errorf("problem type = %q, want %q", got.Type, wantType.URI())
	}

	if loc := rec.Header().Get("Location"); loc != "" {
		t.Errorf("failed callback redirected to %q, want no Location header", loc)
	}
}

func TestOIDCCallbackEstablishesTheSession(t *testing.T) {
	t.Parallel()

	const (
		clientID = "client-id"
		subject  = "subject-123"
		state    = "state-from-session"
		nonce    = "nonce-from-session"
		pkce     = seedCallbackSessionVerifier
		code     = "authorization-code"
	)

	signingKey := newCallbackRSAKey(t)

	cases := []struct {
		name string
		// nameClaim and preferredUsername vary the display-name fallback.
		nameClaim         string
		preferredUsername string
		// returnTo is the value Login stashed in the session.
		returnTo     string
		wantName     string
		wantLocation string
	}{
		{
			name:              "name claim and stashed return_to",
			nameClaim:         "Ada Lovelace",
			preferredUsername: "ada",
			returnTo:          "/lists?filter=open",
			wantName:          "Ada Lovelace",
			wantLocation:      "/lists?filter=open",
		},
		{
			name:              "empty name claim falls back to preferred_username",
			nameClaim:         "",
			preferredUsername: "ada",
			returnTo:          "/lists/42",
			wantName:          "ada",
			wantLocation:      "/lists/42",
		},
		{
			name:              "no stashed return_to lands on the root",
			nameClaim:         "Ada Lovelace",
			preferredUsername: "ada",
			returnTo:          "",
			wantName:          "Ada Lovelace",
			wantLocation:      "/",
		},
		{
			name:              "absolute return_to is re-sanitised to the root",
			nameClaim:         "Ada Lovelace",
			preferredUsername: "ada",
			returnTo:          "https://evil.example.com/steal",
			wantName:          "Ada Lovelace",
			wantLocation:      "/",
		},
		{
			name:              "protocol-relative return_to is re-sanitised to the root",
			nameClaim:         "Ada Lovelace",
			preferredUsername: "ada",
			returnTo:          "//evil.example.com",
			wantName:          "Ada Lovelace",
			wantLocation:      "/",
		},
		{
			name:              "backslash return_to is re-sanitised to the root",
			nameClaim:         "Ada Lovelace",
			preferredUsername: "ada",
			returnTo:          "/\\evil.example.com",
			wantName:          "Ada Lovelace",
			wantLocation:      "/",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			idp := newCallbackIDP(t)
			repo := new(recordingMembers)
			authenticator, sessions := newCallbackAuthenticator(t, idp, clientID, &signingKey.PublicKey, repo)

			claims := callbackClaims(idp.issuer, clientID, subject, nonce)
			claims["email"] = "ada@example.com"
			claims["name"] = testCase.nameClaim
			claims["preferred_username"] = testCase.preferredUsername

			rawIDToken := signCallbackToken(t, signingKey, claims)
			idp.respond(http.StatusOK, callbackTokenResponse(t, rawIDToken, false))

			preLoginToken := seedCallbackSession(t, sessions, state, nonce, testCase.returnTo)

			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet,
				"/api/auth/callback?state="+url.QueryEscape(state)+"&code="+url.QueryEscape(code), nil)
			req.AddCookie(sessionCookie(sessions.Cookie.Name, preLoginToken))

			rec := httptest.NewRecorder()
			sessions.LoadAndSave(http.HandlerFunc(authenticator.Callback)).ServeHTTP(rec, req)

			if rec.Code != http.StatusFound {
				t.Fatalf("status = %d, want 302 (body: %q)", rec.Code, rec.Body.String())
			}

			if got := rec.Header().Get("Location"); got != testCase.wantLocation {
				t.Errorf("Location = %q, want %q", got, testCase.wantLocation)
			}

			// The code was exchanged once, with the stashed PKCE verifier.
			calls, form := idp.observed()
			if calls != 1 {
				t.Fatalf("token endpoint called %d times, want 1", calls)
			}

			if got := form.Get("code"); got != code {
				t.Errorf("exchanged code = %q, want %q", got, code)
			}

			if got := form.Get("code_verifier"); got != pkce {
				t.Errorf("code_verifier = %q, want the stashed verifier %q", got, pkce)
			}

			// Session fixation: the browser leaves with a different token.
			renewed := renewedSessionToken(t, rec, sessions.Cookie.Name)
			if renewed == preLoginToken {
				t.Error("session token is unchanged after login, want RenewToken to have issued a new one")
			}

			ctx, err := sessions.Load(context.Background(), renewed)
			if err != nil {
				t.Fatalf("Load(renewed): %v", err)
			}

			got, ok := getSessionData(ctx, sessions)
			if !ok {
				t.Fatal("renewed session carries no SessionData")
			}

			want := SessionData{
				UserID:  subjectUUID(subject),
				IDToken: rawIDToken,
				Subject: subject,
				Email:   "ada@example.com",
				Name:    testCase.wantName,
			}
			if got != want {
				t.Errorf("SessionData = %+v, want %+v", got, want)
			}

			assertPreLoginCleared(t, sessions, renewed)

			// The pre-login session id is dead, so a stolen cookie is worthless.
			oldCtx, err := sessions.Load(context.Background(), preLoginToken)
			if err != nil {
				t.Fatalf("Load(pre-login): %v", err)
			}

			if _, ok := getSessionData(oldCtx, sessions); ok {
				t.Error("the pre-login session id still carries the authenticated data")
			}

			upserts, member := repo.upserted()
			if upserts != 1 {
				t.Fatalf("members.Upsert called %d times, want 1", upserts)
			}

			if member.Subject != subject || member.UserID != subjectUUID(subject) {
				t.Errorf("upserted (subject, user id) = (%q, %s), want (%q, %s)",
					member.Subject, member.UserID, subject, subjectUUID(subject))
			}

			if member.Email != "ada@example.com" || member.Name != testCase.wantName {
				t.Errorf("upserted (email, name) = (%q, %q), want (%q, %q)",
					member.Email, member.Name, "ada@example.com", testCase.wantName)
			}
		})
	}
}

func TestOIDCCallbackRejectsBrokenFlows(t *testing.T) {
	t.Parallel()

	const (
		clientID = "client-id"
		subject  = "subject-123"
		state    = "state-from-session"
		nonce    = "nonce-from-session"
	)

	signingKey := newCallbackRSAKey(t)

	cases := []struct {
		name string
		// noCookie replays a cold hit on the callback URL: no session at all.
		noCookie bool
		// sessionState is the state Login stashed; "" models a lost session value.
		sessionState string
		// sessionNonce is the nonce Login stashed.
		sessionNonce string
		// query is the callback URL's raw query, as the IdP sent the browser.
		query string
		// tokenStatus, when non-zero, makes the token endpoint fail the exchange.
		tokenStatus int
		// tokenNonce is the nonce the returned ID token carries.
		tokenNonce string
		// upsertErr makes the members repository fail.
		upsertErr error
		// wantCalls is how often the token endpoint must have been hit.
		wantCalls int
		// wantCleared asserts the single-use pre-login values are gone; only
		// paths past state validation guarantee it.
		wantCleared bool
		wantType    problem.Type
		wantDetail  string
	}{
		{
			name:         "cold hit with no session cookie",
			noCookie:     true,
			sessionState: state,
			sessionNonce: nonce,
			query:        "state=" + state + "&code=authorization-code",
			tokenStatus:  0,
			tokenNonce:   nonce,
			upsertErr:    nil,
			wantCalls:    0,
			wantCleared:  false,
			wantType:     problem.Validation,
			wantDetail:   "invalid or missing state parameter",
		},
		{
			name:         "session lost its state and none was sent back",
			noCookie:     false,
			sessionState: "",
			sessionNonce: nonce,
			query:        "code=authorization-code",
			tokenStatus:  0,
			tokenNonce:   nonce,
			upsertErr:    nil,
			wantCalls:    0,
			wantCleared:  false,
			wantType:     problem.Validation,
			wantDetail:   "invalid or missing state parameter",
		},
		{
			name:         "tampered state never reaches the token endpoint",
			noCookie:     false,
			sessionState: state,
			sessionNonce: nonce,
			query:        "state=state-from-attacker&code=authorization-code",
			tokenStatus:  0,
			tokenNonce:   nonce,
			upsertErr:    nil,
			wantCalls:    0,
			wantCleared:  false,
			wantType:     problem.Validation,
			wantDetail:   "invalid or missing state parameter",
		},
		{
			name:         "provider denied the request instead of returning a code",
			noCookie:     false,
			sessionState: state,
			sessionNonce: nonce,
			query:        "state=" + state + "&error=access_denied&error_description=user+refused",
			tokenStatus:  0,
			tokenNonce:   nonce,
			upsertErr:    nil,
			wantCalls:    0,
			wantCleared:  true,
			wantType:     problem.Validation,
			wantDetail:   "missing authorization code",
		},
		{
			name:         "token exchange fails",
			noCookie:     false,
			sessionState: state,
			sessionNonce: nonce,
			query:        "state=" + state + "&code=authorization-code",
			tokenStatus:  http.StatusBadRequest,
			tokenNonce:   nonce,
			upsertErr:    nil,
			wantCalls:    1,
			wantCleared:  true,
			wantType:     problem.Unavailable,
			wantDetail:   "token exchange with the identity provider failed",
		},
		{
			name:         "ID token replayed from another login",
			noCookie:     false,
			sessionState: state,
			sessionNonce: nonce,
			query:        "state=" + state + "&code=authorization-code",
			tokenStatus:  0,
			tokenNonce:   "nonce-from-another-login",
			upsertErr:    nil,
			wantCalls:    1,
			wantCleared:  true,
			wantType:     problem.Unauthorized,
			wantDetail:   "ID token nonce mismatch",
		},
		{
			name:         "session lost its nonce and the token carries none",
			noCookie:     false,
			sessionState: state,
			sessionNonce: "",
			query:        "state=" + state + "&code=authorization-code",
			tokenStatus:  0,
			tokenNonce:   "",
			upsertErr:    nil,
			wantCalls:    1,
			wantCleared:  true,
			wantType:     problem.Unauthorized,
			wantDetail:   "ID token nonce mismatch",
		},
		{
			name:         "membership cannot be recorded",
			noCookie:     false,
			sessionState: state,
			sessionNonce: nonce,
			query:        "state=" + state + "&code=authorization-code",
			tokenStatus:  0,
			tokenNonce:   nonce,
			upsertErr:    errCallbackUpsert,
			wantCalls:    1,
			wantCleared:  true,
			wantType:     problem.Internal,
			wantDetail:   "recording membership",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			idp := newCallbackIDP(t)
			repo := new(recordingMembers)
			repo.err = testCase.upsertErr
			authenticator, sessions := newCallbackAuthenticator(t, idp, clientID, &signingKey.PublicKey, repo)

			if testCase.tokenStatus != 0 {
				idp.respond(testCase.tokenStatus, []byte(`{"error":"invalid_grant"}`))
			} else {
				claims := callbackClaims(idp.issuer, clientID, subject, testCase.tokenNonce)
				idp.respond(http.StatusOK, callbackTokenResponse(t, signCallbackToken(t, signingKey, claims), false))
			}

			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/auth/callback?"+testCase.query, nil)

			var sessionToken string

			if !testCase.noCookie {
				sessionToken = seedCallbackSession(t, sessions, testCase.sessionState, testCase.sessionNonce, "/lists")
				req.AddCookie(sessionCookie(sessions.Cookie.Name, sessionToken))
			}

			rec := httptest.NewRecorder()
			sessions.LoadAndSave(http.HandlerFunc(authenticator.Callback)).ServeHTTP(rec, req)

			assertCallbackProblem(t, rec, testCase.wantType, testCase.wantDetail)

			if calls, _ := idp.observed(); calls != testCase.wantCalls {
				t.Errorf("token endpoint called %d times, want %d", calls, testCase.wantCalls)
			}

			if testCase.wantCleared {
				assertPreLoginCleared(t, sessions, sessionToken)
			}
		})
	}
}

func TestOIDCCallbackRejectsUnusableIDTokens(t *testing.T) {
	t.Parallel()

	const (
		clientID = "client-id"
		subject  = "subject-123"
		state    = "state-from-session"
		nonce    = "nonce-from-session"
	)

	signingKey := newCallbackRSAKey(t)
	foreignKey := newCallbackRSAKey(t)

	cases := []struct {
		name string
		// omitIDToken drops the id_token member from the token response.
		omitIDToken bool
		// idTokenValue replaces the signed token with a raw JSON value.
		idTokenValue any
		// signKey signs the ID token; nil means the provider's own key.
		signKey    *rsa.PrivateKey
		wantType   problem.Type
		wantDetail string
	}{
		{
			name:         "plain OAuth2 response without an ID token",
			omitIDToken:  true,
			idTokenValue: nil,
			signKey:      nil,
			wantType:     problem.Unavailable,
			wantDetail:   "identity provider returned no ID token",
		},
		{
			name:         "empty id_token",
			omitIDToken:  false,
			idTokenValue: "",
			signKey:      nil,
			wantType:     problem.Unavailable,
			wantDetail:   "identity provider returned no ID token",
		},
		{
			name:         "id_token is not a JSON string",
			omitIDToken:  false,
			idTokenValue: 12345,
			signKey:      nil,
			wantType:     problem.Unavailable,
			wantDetail:   "identity provider returned no ID token",
		},
		{
			name:         "id_token signed by an unknown key",
			omitIDToken:  false,
			idTokenValue: nil,
			signKey:      foreignKey,
			wantType:     problem.Unauthorized,
			wantDetail:   "ID token verification failed",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			idp := newCallbackIDP(t)
			repo := new(recordingMembers)
			authenticator, sessions := newCallbackAuthenticator(t, idp, clientID, &signingKey.PublicKey, repo)

			idToken := testCase.idTokenValue
			if idToken == nil && !testCase.omitIDToken {
				signKey := signingKey
				if testCase.signKey != nil {
					signKey = testCase.signKey
				}

				idToken = signCallbackToken(t, signKey, callbackClaims(idp.issuer, clientID, subject, nonce))
			}

			idp.respond(http.StatusOK, callbackTokenResponse(t, idToken, testCase.omitIDToken))

			sessionToken := seedCallbackSession(t, sessions, state, nonce, "/lists")
			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet,
				"/api/auth/callback?state="+state+"&code=authorization-code", nil)
			req.AddCookie(sessionCookie(sessions.Cookie.Name, sessionToken))

			rec := httptest.NewRecorder()
			sessions.LoadAndSave(http.HandlerFunc(authenticator.Callback)).ServeHTTP(rec, req)

			assertCallbackProblem(t, rec, testCase.wantType, testCase.wantDetail)
			assertPreLoginCleared(t, sessions, sessionToken)

			if calls, _ := repo.upserted(); calls != 0 {
				t.Errorf("members.Upsert called %d times, want 0 for a rejected ID token", calls)
			}
		})
	}
}

func TestSameSiteString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		mode http.SameSite
		want string
	}{
		{name: "default mode", mode: http.SameSiteDefaultMode, want: "default"},
		{name: "lax mode", mode: http.SameSiteLaxMode, want: "lax"},
		{name: "strict mode", mode: http.SameSiteStrictMode, want: "strict"},
		{name: "none mode", mode: http.SameSiteNoneMode, want: "none"},
		{name: "zero value", mode: http.SameSite(0), want: "unknown"},
		{name: "just above defined range", mode: http.SameSite(5), want: "unknown"},
		{name: "far above defined range", mode: http.SameSite(999), want: "unknown"},
		{name: "negative", mode: http.SameSite(-1), want: "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := sameSiteString(tt.mode); got != tt.want {
				t.Errorf("sameSiteString(%d) = %q, want %q", int(tt.mode), got, tt.want)
			}
		})
	}
}

// TestSameSiteStringNeverEmpty pins the invariant that every input, including
// values outside the defined constants, renders as a non-empty label.
func TestSameSiteStringNeverEmpty(t *testing.T) {
	t.Parallel()

	for i := -10; i <= 10; i++ {
		if got := sameSiteString(http.SameSite(i)); got == "" {
			t.Errorf("sameSiteString(%d) returned an empty string", i)
		}
	}
}

func TestSameSiteStringNamedModesAreDistinct(t *testing.T) {
	t.Parallel()

	modes := []http.SameSite{
		http.SameSiteDefaultMode,
		http.SameSiteLaxMode,
		http.SameSiteStrictMode,
		http.SameSiteNoneMode,
	}

	seen := make(map[string]http.SameSite, len(modes))
	for _, mode := range modes {
		got := sameSiteString(mode)
		if got == "unknown" {
			t.Errorf("sameSiteString(%d) = %q, want a named rendering", int(mode), got)

			continue
		}

		if got != strings.ToLower(got) {
			t.Errorf("sameSiteString(%d) = %q, want lowercase", int(mode), got)
		}

		if prev, dup := seen[got]; dup {
			t.Errorf("sameSiteString(%d) = %q, already returned for mode %d", int(mode), got, int(prev))

			continue
		}

		seen[got] = mode
	}
}

// TestOIDCCallbackRejectsInvalidIDTokenClaims covers the checks that run on a
// well-formed, well-signed JWT: the verifier's expiry/audience/issuer rules,
// the nonce guard when only the session side lost its value, and the claim
// decoding that follows.
func TestOIDCCallbackRejectsInvalidIDTokenClaims(t *testing.T) {
	t.Parallel()

	const (
		clientID = "client-id"
		subject  = "subject-123"
		state    = "state-from-session"
		nonce    = "nonce-from-session"
	)

	signingKey := newCallbackRSAKey(t)

	cases := []struct {
		name string
		// mutate rewrites the otherwise valid claim set the provider signs.
		mutate func(claims map[string]any)
		// loseSessionNonce drops the nonce Login stashed while the ID token
		// still carries one.
		loseSessionNonce bool
		wantType         problem.Type
		wantDetail       string
	}{
		{
			name: "ID token expired before it was redeemed",
			mutate: func(claims map[string]any) {
				claims["iat"] = time.Now().Add(-2 * time.Hour).Unix()
				claims["exp"] = time.Now().Add(-time.Hour).Unix()
			},
			loseSessionNonce: false,
			wantType:         problem.Unauthorized,
			wantDetail:       "ID token verification failed",
		},
		{
			name: "ID token minted for another client",
			mutate: func(claims map[string]any) {
				claims["aud"] = "some-other-client"
			},
			loseSessionNonce: false,
			wantType:         problem.Unauthorized,
			wantDetail:       "ID token verification failed",
		},
		{
			name: "ID token issued by another provider",
			mutate: func(claims map[string]any) {
				claims["iss"] = "https://evil.example.com"
			},
			loseSessionNonce: false,
			wantType:         problem.Unauthorized,
			wantDetail:       "ID token verification failed",
		},
		{
			name:             "session lost its nonce while the token still carries one",
			mutate:           nil,
			loseSessionNonce: true,
			wantType:         problem.Unauthorized,
			wantDetail:       "ID token nonce mismatch",
		},
		{
			name: "claims cannot be decoded",
			mutate: func(claims map[string]any) {
				// A numeric email is valid JSON but not a Go string.
				claims["email"] = 42
			},
			loseSessionNonce: false,
			wantType:         problem.Internal,
			wantDetail:       "reading ID token claims",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			idp := newCallbackIDP(t)
			repo := new(recordingMembers)
			authenticator, sessions := newCallbackAuthenticator(t, idp, clientID, &signingKey.PublicKey, repo)

			claims := callbackClaims(idp.issuer, clientID, subject, nonce)
			if testCase.mutate != nil {
				testCase.mutate(claims)
			}

			idp.respond(http.StatusOK, callbackTokenResponse(t, signCallbackToken(t, signingKey, claims), false))

			sessionNonce := nonce
			if testCase.loseSessionNonce {
				sessionNonce = ""
			}

			sessionToken := seedCallbackSession(t, sessions, state, sessionNonce, "/lists")
			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet,
				"/api/auth/callback?state="+state+"&code=authorization-code", nil)
			req.AddCookie(sessionCookie(sessions.Cookie.Name, sessionToken))

			rec := httptest.NewRecorder()
			sessions.LoadAndSave(http.HandlerFunc(authenticator.Callback)).ServeHTTP(rec, req)

			assertCallbackProblem(t, rec, testCase.wantType, testCase.wantDetail)
			assertPreLoginCleared(t, sessions, sessionToken)

			// The code was still redeemed once: these checks all run after the
			// exchange.
			if calls, _ := idp.observed(); calls != 1 {
				t.Errorf("token endpoint called %d times, want 1", calls)
			}

			if calls, _ := repo.upserted(); calls != 0 {
				t.Errorf("members.Upsert called %d times, want 0 for a rejected ID token", calls)
			}
		})
	}
}

// TestOIDCCallbackWithoutDisplayNameClaims pins the exhausted end of the
// display-name fallback: with neither name nor preferred_username usable, the
// session and the member record carry an empty name rather than a placeholder.
func TestOIDCCallbackWithoutDisplayNameClaims(t *testing.T) {
	t.Parallel()

	const (
		clientID = "client-id"
		subject  = "subject-123"
		state    = "state-from-session"
		nonce    = "nonce-from-session"
	)

	signingKey := newCallbackRSAKey(t)

	cases := []struct {
		name string
		// omitClaims leaves both name claims out of the payload entirely
		// instead of sending them as empty strings.
		omitClaims bool
	}{
		{name: "both name claims are empty strings", omitClaims: false},
		{name: "neither name claim is present", omitClaims: true},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			idp := newCallbackIDP(t)
			repo := new(recordingMembers)
			authenticator, sessions := newCallbackAuthenticator(t, idp, clientID, &signingKey.PublicKey, repo)

			claims := callbackClaims(idp.issuer, clientID, subject, nonce)
			claims["email"] = "grace@example.com"

			if !testCase.omitClaims {
				claims["name"] = ""
				claims["preferred_username"] = ""
			}

			rawIDToken := signCallbackToken(t, signingKey, claims)
			idp.respond(http.StatusOK, callbackTokenResponse(t, rawIDToken, false))

			preLoginToken := seedCallbackSession(t, sessions, state, nonce, "/lists")
			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet,
				"/api/auth/callback?state="+state+"&code=authorization-code", nil)
			req.AddCookie(sessionCookie(sessions.Cookie.Name, preLoginToken))

			rec := httptest.NewRecorder()
			sessions.LoadAndSave(http.HandlerFunc(authenticator.Callback)).ServeHTTP(rec, req)

			if rec.Code != http.StatusFound {
				t.Fatalf("status = %d, want 302 (body: %q)", rec.Code, rec.Body.String())
			}

			renewed := renewedSessionToken(t, rec, sessions.Cookie.Name)

			ctx, err := sessions.Load(context.Background(), renewed)
			if err != nil {
				t.Fatalf("Load(renewed): %v", err)
			}

			got, ok := getSessionData(ctx, sessions)
			if !ok {
				t.Fatal("renewed session carries no SessionData")
			}

			want := SessionData{
				UserID:  subjectUUID(subject),
				IDToken: rawIDToken,
				Subject: subject,
				Email:   "grace@example.com",
				Name:    "",
			}
			if got != want {
				t.Errorf("SessionData = %+v, want %+v", got, want)
			}

			upserts, member := repo.upserted()
			if upserts != 1 {
				t.Fatalf("members.Upsert called %d times, want 1", upserts)
			}

			if member.Name != "" {
				t.Errorf("upserted member name = %q, want it empty", member.Name)
			}
		})
	}
}

// TestOIDCCallbackFailsWhenTheSessionCannotBeRenewed drives the store error
// RenewToken surfaces: the login must abort before any membership is recorded
// and before an authenticated session exists anywhere.
func TestOIDCCallbackFailsWhenTheSessionCannotBeRenewed(t *testing.T) {
	t.Parallel()

	const (
		clientID = "client-id"
		subject  = "subject-123"
		state    = "state-from-session"
		nonce    = "nonce-from-session"
	)

	signingKey := newCallbackRSAKey(t)
	idp := newCallbackIDP(t)
	repo := new(recordingMembers)
	authenticator, sessions := newCallbackAuthenticator(t, idp, clientID, &signingKey.PublicKey, repo)

	store := newFlakyStore()
	sessions.Store = store

	claims := callbackClaims(idp.issuer, clientID, subject, nonce)
	claims["email"] = "renewal@example.com"
	claims["name"] = "Grace Hopper"
	idp.respond(http.StatusOK, callbackTokenResponse(t, signCallbackToken(t, signingKey, claims), false))

	preLoginToken := seedCallbackSession(t, sessions, state, nonce, "/lists")

	// Only the renewal must fail; seeding the pre-login session above still
	// had to succeed.
	store.fail(errCallbackStore)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet,
		"/api/auth/callback?state="+state+"&code=authorization-code", nil)
	req.AddCookie(sessionCookie(sessions.Cookie.Name, preLoginToken))

	rec := httptest.NewRecorder()
	sessions.LoadAndSave(http.HandlerFunc(authenticator.Callback)).ServeHTTP(rec, req)

	assertCallbackProblem(t, rec, problem.Internal, "renewing session")

	if calls, _ := repo.upserted(); calls != 0 {
		t.Errorf("members.Upsert called %d times, want 0 when the session cannot be renewed", calls)
	}

	ctx, err := sessions.Load(context.Background(), preLoginToken)
	if err != nil {
		t.Fatalf("Load(pre-login): %v", err)
	}

	if _, ok := getSessionData(ctx, sessions); ok {
		t.Error("an authenticated session was stored even though the renewal failed")
	}
}
