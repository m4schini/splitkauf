// SPDX-License-Identifier: CC0-1.0

package auth

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alexedwards/scs/v2"
	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/google/uuid"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

	"github.com/m4schini/splitkauf/members"
	"github.com/m4schini/splitkauf/ports/rest/problem"
)

// stateSessionManager returns a session manager backed by the default
// in-memory store, suitable for seeding and loading a callback-state session.
func stateSessionManager(t *testing.T) *scs.SessionManager {
	t.Helper()

	sessions := scs.New()
	sessions.Lifetime = time.Hour

	return sessions
}

// stateCallbackRequest builds a GET request for the callback path whose context
// carries the scs session identified by token, optionally attaching the session
// cookie the failure-path logging inspects.
func stateCallbackRequest(
	t *testing.T, sessions *scs.SessionManager, token, rawQuery string, withCookie bool,
) *http.Request {
	t.Helper()

	ctx, err := sessions.Load(context.Background(), token)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	target := "/auth/callback"
	if rawQuery != "" {
		target += "?" + rawQuery
	}

	req := httptest.NewRequestWithContext(ctx, http.MethodGet, target, nil)

	if withCookie {
		cookieValue := token
		if cookieValue == "" {
			cookieValue = uuid.NewString()
		}

		req.AddCookie(&http.Cookie{Name: sessions.Cookie.Name, Value: cookieValue})
	}

	return req
}

func TestOIDCAuthenticatorValidCallbackState(t *testing.T) {
	t.Parallel()

	// 22 bytes, the shape of a base64url-encoded 128 bit pre-login state.
	const (
		wantState     = "s7QpX2f8Lm0aZr4KbN1cVw"
		sameLenState  = "s7QpX2f8Lm0aZr4KbN1cVx" // same length, last byte differs
		prefixState   = "s7QpX2f8Lm0aZr4KbN1cV"  // one byte short
		longerState   = "s7QpX2f8Lm0aZr4KbN1cVwZ"
		callbackPath  = "/auth/callback"
		problemDetail = "invalid or missing state parameter"
	)

	tests := []struct {
		name string
		// seedState is written to the session under stateKey; "" seeds no
		// session at all (the pre-login cookie was never sent back).
		seedState string
		rawQuery  string
		// cookie controls whether a session cookie rides along; it only feeds
		// the failure log, so it must never change the return value.
		cookie bool
		want   bool
	}{
		{
			name:      "exact match",
			seedState: wantState,
			rawQuery:  "state=" + wantState,
			cookie:    true,
			want:      true,
		},
		{
			name:      "exact match without session cookie header",
			seedState: wantState,
			rawQuery:  "state=" + wantState,
			cookie:    false,
			want:      true,
		},
		{
			name:      "repeated state parameter uses the first value",
			seedState: wantState,
			rawQuery:  "state=" + wantState + "&state=" + sameLenState,
			cookie:    true,
			want:      true,
		},
		{
			name:      "repeated state parameter with matching second value",
			seedState: wantState,
			rawQuery:  "state=" + sameLenState + "&state=" + wantState,
			cookie:    true,
			want:      false,
		},
		{
			name:      "session state missing but query state present",
			seedState: "",
			rawQuery:  "state=" + wantState,
			cookie:    true,
			want:      false,
		},
		{
			name:      "session state missing without session cookie",
			seedState: "",
			rawQuery:  "state=" + wantState,
			cookie:    false,
			want:      false,
		},
		{
			name:      "both states empty",
			seedState: "",
			rawQuery:  "state=",
			cookie:    false,
			want:      false,
		},
		{
			name:      "no session state and no query parameter",
			seedState: "",
			rawQuery:  "",
			cookie:    false,
			want:      false,
		},
		{
			name:      "query state parameter absent",
			seedState: wantState,
			rawQuery:  "",
			cookie:    true,
			want:      false,
		},
		{
			name:      "query state parameter present but empty",
			seedState: wantState,
			rawQuery:  "state=",
			cookie:    true,
			want:      false,
		},
		{
			name:      "same length different bytes",
			seedState: wantState,
			rawQuery:  "state=" + sameLenState,
			cookie:    true,
			want:      false,
		},
		{
			name:      "query state is a prefix of session state",
			seedState: wantState,
			rawQuery:  "state=" + prefixState,
			cookie:    true,
			want:      false,
		},
		{
			name:      "query state longer than session state",
			seedState: wantState,
			rawQuery:  "state=" + longerState,
			cookie:    true,
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			sessions := stateSessionManager(t)

			var token string
			if tt.seedState != "" {
				token = seedSession(t, sessions, func(ctx context.Context) {
					sessions.Put(ctx, stateKey, tt.seedState)
				})
			}

			a := &oidcAuthenticator{
				sm:     sessions,
				logger: zap.NewNop(),
			}

			req := stateCallbackRequest(t, sessions, token, tt.rawQuery, tt.cookie)
			res := httptest.NewRecorder()

			got := a.validCallbackState(res, req)
			if got != tt.want {
				t.Fatalf("validCallbackState() = %v, want %v", got, tt.want)
			}

			if tt.want {
				if res.Code != http.StatusOK {
					t.Errorf("status = %d, want %d (nothing should be written on success)", res.Code, http.StatusOK)
				}

				if body := res.Body.String(); body != "" {
					t.Errorf("body = %q, want empty on success", body)
				}

				if ct := res.Header().Get("Content-Type"); ct != "" {
					t.Errorf("Content-Type = %q, want empty on success", ct)
				}

				return
			}

			if res.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want %d", res.Code, http.StatusBadRequest)
			}

			if ct := res.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/problem+json") {
				t.Errorf("Content-Type = %q, want application/problem+json", ct)
			}

			var body struct {
				Title    string `json:"title"`
				Detail   string `json:"detail"`
				Instance string `json:"instance"`
				Status   int    `json:"status"`
			}
			if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode problem body %q: %v", res.Body.String(), err)
			}

			if body.Title != "Bad Request" {
				t.Errorf("problem title = %q, want %q", body.Title, "Bad Request")
			}

			if body.Detail != problemDetail {
				t.Errorf("problem detail = %q, want %q", body.Detail, problemDetail)
			}

			if body.Instance != callbackPath {
				t.Errorf("problem instance = %q, want %q", body.Instance, callbackPath)
			}

			if body.Status != 0 && body.Status != http.StatusBadRequest {
				t.Errorf("problem status = %d, want %d", body.Status, http.StatusBadRequest)
			}
		})
	}
}

// The fixed inputs the exchangeAndVerify tests drive the token exchange with.
const (
	exchangeClientID = "splitkauf-web"
	exchangeSubject  = "auth0|exchange-and-verify"
	exchangeNonce    = "expected-nonce-value"
	exchangeCode     = "authorization-code"
	exchangeVerifier = "pkce-code-verifier"
)

// The problem details exchangeAndVerify writes, one per failure branch.
const (
	exchangeFailedDetail = "token exchange with the identity provider failed"
	noIDTokenDetail      = "identity provider returned no ID token"
	verifyFailedDetail   = "ID token verification failed"
	nonceMismatchDetail  = "ID token nonce mismatch"
)

// exchangeEnv is what a failure case's reply builder needs: the subtest's
// *testing.T and the fake provider whose issuer the ID token must name.
type exchangeEnv struct {
	t   *testing.T
	idp *callbackIDP
}

// exchangeFailure is one exchangeAndVerify failure: the reply the token
// endpoint sends, the arguments the callback hands the method, and the problem
// response that must come back instead of a token.
type exchangeFailure struct {
	status     int
	body       func(env exchangeEnv) []byte
	code       string
	verifier   string
	nonce      string
	cancel     bool
	wantCalls  int
	wantType   problem.Type
	wantDetail string
}

// runExchangeFailure drives one failure case against an authenticator whose
// verifier trusts signKey, and asserts exchangeAndVerify wrote the problem
// response and returned nothing usable.
func runExchangeFailure(t *testing.T, signKey *rsa.PrivateKey, failure exchangeFailure) {
	t.Helper()

	idp := newCallbackIDP(t)
	authenticator, _ := newCallbackAuthenticator(t, idp, exchangeClientID, &signKey.PublicKey, new(recordingMembers))
	idp.respond(failure.status, failure.body(exchangeEnv{t: t, idp: idp}))

	ctx := context.Background()

	if failure.cancel {
		cancelled, cancel := context.WithCancel(ctx)
		cancel()

		ctx = cancelled
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/api/auth/callback?code=x&state=y", nil)

	rawIDToken, idToken, ok := authenticator.exchangeAndVerify(rec, req, failure.code, failure.verifier, failure.nonce)
	if ok {
		t.Fatal("ok = true, want false")
	}

	if rawIDToken != "" {
		t.Errorf("raw ID token = %q, want the empty string", rawIDToken)
	}

	if idToken != nil {
		t.Errorf("ID token = %+v, want nil", idToken)
	}

	assertCallbackProblem(t, rec, failure.wantType, failure.wantDetail)

	if calls, _ := idp.observed(); calls != failure.wantCalls {
		t.Errorf("token endpoint calls = %d, want %d", calls, failure.wantCalls)
	}
}

func TestOIDCExchangeAndVerifyReturnsTheVerifiedIDToken(t *testing.T) {
	t.Parallel()

	key := newCallbackRSAKey(t)
	idp := newCallbackIDP(t)
	authenticator, _ := newCallbackAuthenticator(t, idp, exchangeClientID, &key.PublicKey, new(recordingMembers))

	rawIDToken := signCallbackToken(t, key,
		callbackClaims(idp.issuer, exchangeClientID, exchangeSubject, exchangeNonce))
	idp.respond(http.StatusOK, callbackTokenResponse(t, rawIDToken, false))

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(
		context.Background(), http.MethodGet, "/api/auth/callback?code=x&state=y", nil)

	gotRaw, idToken, ok := authenticator.exchangeAndVerify(rec, req, exchangeCode, exchangeVerifier, exchangeNonce)
	if !ok {
		t.Fatalf("ok = false, want true (response: %d %q)", rec.Code, rec.Body.String())
	}

	// The raw JWT is kept as the RP-initiated-logout hint, so it must come back
	// byte-identical to what the provider sent.
	if gotRaw != rawIDToken {
		t.Errorf("raw ID token = %q, want %q", gotRaw, rawIDToken)
	}

	if idToken == nil {
		t.Fatal("verified ID token is nil, want the parsed token")
	}

	if idToken.Subject != exchangeSubject {
		t.Errorf("ID token subject = %q, want %q", idToken.Subject, exchangeSubject)
	}

	if idToken.Nonce != exchangeNonce {
		t.Errorf("ID token nonce = %q, want %q", idToken.Nonce, exchangeNonce)
	}

	if rec.Body.Len() != 0 {
		t.Errorf("success wrote the body %q, want no problem response", rec.Body.String())
	}

	// The code and the PKCE verifier must reach the token endpoint verbatim.
	calls, form := idp.observed()
	if calls != 1 {
		t.Fatalf("token endpoint calls = %d, want 1", calls)
	}

	if got := form.Get("code"); got != exchangeCode {
		t.Errorf("token request code = %q, want %q", got, exchangeCode)
	}

	if got := form.Get("code_verifier"); got != exchangeVerifier {
		t.Errorf("token request code_verifier = %q, want %q", got, exchangeVerifier)
	}
}

func TestOIDCExchangeAndVerifyRejectsUnusableTokenResponses(t *testing.T) {
	t.Parallel()

	signKey := newCallbackRSAKey(t)

	// validToken mints an ID token the verifier accepts, carrying nonce.
	validToken := func(env exchangeEnv, nonce string) string {
		return signCallbackToken(env.t, signKey,
			callbackClaims(env.idp.issuer, exchangeClientID, exchangeSubject, nonce))
	}

	cases := []struct {
		name    string
		failure exchangeFailure
	}{
		{
			name: "token endpoint rejects the code",
			failure: exchangeFailure{
				status: http.StatusBadRequest,
				body: func(exchangeEnv) []byte {
					return []byte(`{"error":"invalid_grant"}`)
				},
				code:       "reused-code",
				verifier:   exchangeVerifier,
				nonce:      exchangeNonce,
				cancel:     false,
				wantCalls:  1,
				wantType:   problem.Unavailable,
				wantDetail: exchangeFailedDetail,
			},
		},
		{
			// Neither argument is validated locally, so an empty code or
			// verifier can only surface as an exchange failure.
			name: "empty code and verifier go straight to the token endpoint",
			failure: exchangeFailure{
				status: http.StatusBadRequest,
				body: func(exchangeEnv) []byte {
					return []byte(`{"error":"invalid_request"}`)
				},
				code:       "",
				verifier:   "",
				nonce:      exchangeNonce,
				cancel:     false,
				wantCalls:  1,
				wantType:   problem.Unavailable,
				wantDetail: exchangeFailedDetail,
			},
		},
		{
			name: "cancelled request context",
			failure: exchangeFailure{
				status: http.StatusOK,
				body: func(env exchangeEnv) []byte {
					return callbackTokenResponse(env.t, validToken(env, exchangeNonce), false)
				},
				code:       exchangeCode,
				verifier:   exchangeVerifier,
				nonce:      exchangeNonce,
				cancel:     true,
				wantCalls:  0,
				wantType:   problem.Unavailable,
				wantDetail: exchangeFailedDetail,
			},
		},
		{
			name: "token response omits id_token",
			failure: exchangeFailure{
				status: http.StatusOK,
				body: func(env exchangeEnv) []byte {
					return callbackTokenResponse(env.t, nil, true)
				},
				code:       exchangeCode,
				verifier:   exchangeVerifier,
				nonce:      exchangeNonce,
				cancel:     false,
				wantCalls:  1,
				wantType:   problem.Unavailable,
				wantDetail: noIDTokenDetail,
			},
		},
		{
			name: "id_token is null",
			failure: exchangeFailure{
				status: http.StatusOK,
				body: func(env exchangeEnv) []byte {
					return callbackTokenResponse(env.t, nil, false)
				},
				code:       exchangeCode,
				verifier:   exchangeVerifier,
				nonce:      exchangeNonce,
				cancel:     false,
				wantCalls:  1,
				wantType:   problem.Unavailable,
				wantDetail: noIDTokenDetail,
			},
		},
		{
			name: "id_token is not a string",
			failure: exchangeFailure{
				status: http.StatusOK,
				body: func(env exchangeEnv) []byte {
					return callbackTokenResponse(env.t, 1234, false)
				},
				code:       exchangeCode,
				verifier:   exchangeVerifier,
				nonce:      exchangeNonce,
				cancel:     false,
				wantCalls:  1,
				wantType:   problem.Unavailable,
				wantDetail: noIDTokenDetail,
			},
		},
		{
			name: "id_token is the empty string",
			failure: exchangeFailure{
				status: http.StatusOK,
				body: func(env exchangeEnv) []byte {
					return callbackTokenResponse(env.t, "", false)
				},
				code:       exchangeCode,
				verifier:   exchangeVerifier,
				nonce:      exchangeNonce,
				cancel:     false,
				wantCalls:  1,
				wantType:   problem.Unavailable,
				wantDetail: noIDTokenDetail,
			},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			runExchangeFailure(t, signKey, testCase.failure)
		})
	}
}

func TestOIDCExchangeAndVerifyRejectsUnverifiedIDTokens(t *testing.T) {
	t.Parallel()

	signKey := newCallbackRSAKey(t)
	// untrustedKey stands in for a forged signature: the verifier never sees
	// its public half.
	untrustedKey := newCallbackRSAKey(t)

	validToken := func(env exchangeEnv, nonce string) string {
		return signCallbackToken(env.t, signKey,
			callbackClaims(env.idp.issuer, exchangeClientID, exchangeSubject, nonce))
	}

	cases := []struct {
		name    string
		failure exchangeFailure
	}{
		{
			name: "id_token signed by an untrusted key",
			failure: exchangeFailure{
				status: http.StatusOK,
				body: func(env exchangeEnv) []byte {
					forged := signCallbackToken(env.t, untrustedKey,
						callbackClaims(env.idp.issuer, exchangeClientID, exchangeSubject, exchangeNonce))

					return callbackTokenResponse(env.t, forged, false)
				},
				code:       exchangeCode,
				verifier:   exchangeVerifier,
				nonce:      exchangeNonce,
				cancel:     false,
				wantCalls:  1,
				wantType:   problem.Unauthorized,
				wantDetail: verifyFailedDetail,
			},
		},
		{
			name: "id_token issued for another audience",
			failure: exchangeFailure{
				status: http.StatusOK,
				body: func(env exchangeEnv) []byte {
					other := signCallbackToken(env.t, signKey,
						callbackClaims(env.idp.issuer, "another-client", exchangeSubject, exchangeNonce))

					return callbackTokenResponse(env.t, other, false)
				},
				code:       exchangeCode,
				verifier:   exchangeVerifier,
				nonce:      exchangeNonce,
				cancel:     false,
				wantCalls:  1,
				wantType:   problem.Unauthorized,
				wantDetail: verifyFailedDetail,
			},
		},
		{
			name: "id_token already expired",
			failure: exchangeFailure{
				status: http.StatusOK,
				body: func(env exchangeEnv) []byte {
					claims := callbackClaims(env.idp.issuer, exchangeClientID, exchangeSubject, exchangeNonce)
					claims["exp"] = time.Now().Add(-time.Hour).Unix()

					return callbackTokenResponse(env.t, signCallbackToken(env.t, signKey, claims), false)
				},
				code:       exchangeCode,
				verifier:   exchangeVerifier,
				nonce:      exchangeNonce,
				cancel:     false,
				wantCalls:  1,
				wantType:   problem.Unauthorized,
				wantDetail: verifyFailedDetail,
			},
		},
		{
			// Verification runs before the nonce check, so a token that is
			// wrong on both counts must report the signature failure.
			name: "bad signature and bad nonce reports the verification failure",
			failure: exchangeFailure{
				status: http.StatusOK,
				body: func(env exchangeEnv) []byte {
					forged := signCallbackToken(env.t, untrustedKey,
						callbackClaims(env.idp.issuer, exchangeClientID, exchangeSubject, "nonce-from-another-login"))

					return callbackTokenResponse(env.t, forged, false)
				},
				code:       exchangeCode,
				verifier:   exchangeVerifier,
				nonce:      exchangeNonce,
				cancel:     false,
				wantCalls:  1,
				wantType:   problem.Unauthorized,
				wantDetail: verifyFailedDetail,
			},
		},
		{
			name: "nonce belongs to another login",
			failure: exchangeFailure{
				status: http.StatusOK,
				body: func(env exchangeEnv) []byte {
					return callbackTokenResponse(env.t, validToken(env, "nonce-from-another-login"), false)
				},
				code:       exchangeCode,
				verifier:   exchangeVerifier,
				nonce:      exchangeNonce,
				cancel:     false,
				wantCalls:  1,
				wantType:   problem.Unauthorized,
				wantDetail: nonceMismatchDetail,
			},
		},
		{
			name: "token nonce is a prefix of the expected nonce",
			failure: exchangeFailure{
				status: http.StatusOK,
				body: func(env exchangeEnv) []byte {
					return callbackTokenResponse(env.t, validToken(env, exchangeNonce[:len(exchangeNonce)-1]), false)
				},
				code:       exchangeCode,
				verifier:   exchangeVerifier,
				nonce:      exchangeNonce,
				cancel:     false,
				wantCalls:  1,
				wantType:   problem.Unauthorized,
				wantDetail: nonceMismatchDetail,
			},
		},
		{
			name: "token nonce extends the expected nonce",
			failure: exchangeFailure{
				status: http.StatusOK,
				body: func(env exchangeEnv) []byte {
					return callbackTokenResponse(env.t, validToken(env, exchangeNonce+"-extra"), false)
				},
				code:       exchangeCode,
				verifier:   exchangeVerifier,
				nonce:      exchangeNonce,
				cancel:     false,
				wantCalls:  1,
				wantType:   problem.Unauthorized,
				wantDetail: nonceMismatchDetail,
			},
		},
		{
			name: "token carries no nonce at all",
			failure: exchangeFailure{
				status: http.StatusOK,
				body: func(env exchangeEnv) []byte {
					return callbackTokenResponse(env.t, validToken(env, ""), false)
				},
				code:       exchangeCode,
				verifier:   exchangeVerifier,
				nonce:      exchangeNonce,
				cancel:     false,
				wantCalls:  1,
				wantType:   problem.Unauthorized,
				wantDetail: nonceMismatchDetail,
			},
		},
		{
			// An empty expected nonce short-circuits before the constant-time
			// compare: empty versus empty must not be accepted as a match.
			name: "no expected nonce and no token nonce",
			failure: exchangeFailure{
				status: http.StatusOK,
				body: func(env exchangeEnv) []byte {
					return callbackTokenResponse(env.t, validToken(env, ""), false)
				},
				code:       exchangeCode,
				verifier:   exchangeVerifier,
				nonce:      "",
				cancel:     false,
				wantCalls:  1,
				wantType:   problem.Unauthorized,
				wantDetail: nonceMismatchDetail,
			},
		},
		{
			name: "no expected nonce but the token carries one",
			failure: exchangeFailure{
				status: http.StatusOK,
				body: func(env exchangeEnv) []byte {
					return callbackTokenResponse(env.t, validToken(env, exchangeNonce), false)
				},
				code:       exchangeCode,
				verifier:   exchangeVerifier,
				nonce:      "",
				cancel:     false,
				wantCalls:  1,
				wantType:   problem.Unauthorized,
				wantDetail: nonceMismatchDetail,
			},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			runExchangeFailure(t, signKey, testCase.failure)
		})
	}
}

// newReadClaimsAuthenticator builds the receiver readClaims runs on. The method
// only touches its arguments, so every collaborator stays nil.
func newReadClaimsAuthenticator() *oidcAuthenticator {
	return &oidcAuthenticator{
		oauth2Config:          nil,
		verifier:              nil,
		sm:                    nil,
		members:               nil,
		clientID:              "",
		endSessionEndpoint:    "",
		postLogoutRedirectURL: "",
		logger:                zap.NewNop(),
	}
}

// readClaimsToken wraps a claims payload in a JWT and runs it through a
// verifier: attaching the raw claims JSON is the verifier's job, so a verified
// token is the only kind readClaims can decode. Signature checking is off, so
// the JWT needs a well-formed shape but no real signature.
func readClaimsToken(t *testing.T, claimsJSON string) *oidc.IDToken {
	t.Helper()

	segment := func(raw string) string {
		return base64.RawURLEncoding.EncodeToString([]byte(raw))
	}

	rawIDToken := segment(`{"alg":"RS256","typ":"JWT"}`) + "." + segment(claimsJSON) + "." +
		segment("signature-is-not-checked")

	verifier := oidc.NewVerifier("https://idp.example", nil, &oidc.Config{
		ClientID:                   "",
		SupportedSigningAlgs:       []string{"RS256"},
		SkipClientIDCheck:          true,
		SkipExpiryCheck:            true,
		SkipIssuerCheck:            true,
		Now:                        nil,
		InsecureSkipSignatureCheck: true,
	})

	idToken, err := verifier.Verify(t.Context(), rawIDToken)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}

	return idToken
}

// readClaimsRequest is the callback request in flight when readClaims runs; its
// path is what problem.Write records as the problem instance.
func readClaimsRequest(t *testing.T) *http.Request {
	t.Helper()

	return httptest.NewRequestWithContext(
		t.Context(), http.MethodGet, "/api/auth/callback?code=auth-code&state=state", nil,
	)
}

func TestOIDCReadClaimsDecodesTheSessionClaims(t *testing.T) {
	t.Parallel()

	authenticator := newReadClaimsAuthenticator()

	tests := []struct {
		name   string
		claims string
		want   idTokenClaims
	}{
		{
			name: "every claim present",
			claims: `{"iss":"https://idp.example","sub":"user-1","email":"ada@example.com",` +
				`"name":"Ada Lovelace","preferred_username":"ada"}`,
			want: idTokenClaims{
				Email:             "ada@example.com",
				Name:              "Ada Lovelace",
				PreferredUsername: "ada",
			},
		},
		{
			name:   "no claim present",
			claims: `{}`,
			want:   idTokenClaims{Email: "", Name: "", PreferredUsername: ""},
		},
		{
			name: "unknown claims ignored",
			claims: `{"sub":"user-2","email":"grace@example.com","email_verified":true,` +
				`"groups":["admins"],"realm_access":{"roles":["user"]}}`,
			want: idTokenClaims{Email: "grace@example.com", Name: "", PreferredUsername: ""},
		},
		{
			name:   "null claims decode as empty strings",
			claims: `{"email":null,"name":null,"preferred_username":null}`,
			want:   idTokenClaims{Email: "", Name: "", PreferredUsername: ""},
		},
		{
			name:   "preferred_username is not substituted for a missing name",
			claims: `{"email":"linus@example.com","preferred_username":"linus"}`,
			want:   idTokenClaims{Email: "linus@example.com", Name: "", PreferredUsername: "linus"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			rec := httptest.NewRecorder()

			got, ok := authenticator.readClaims(rec, readClaimsRequest(t), readClaimsToken(t, test.claims))
			if !ok {
				t.Fatalf("ok = false, want true (body: %q)", rec.Body.String())
			}

			if got != test.want {
				t.Errorf("claims = %+v, want %+v", got, test.want)
			}

			if rec.Code != http.StatusOK || rec.Body.Len() != 0 {
				t.Errorf("wrote status %d and body %q, want an untouched response", rec.Code, rec.Body.String())
			}

			if contentType := rec.Header().Get("Content-Type"); contentType != "" {
				t.Errorf("Content-Type = %q, want the response left untouched", contentType)
			}
		})
	}
}

func TestOIDCReadClaimsRejectsUndecodableClaims(t *testing.T) {
	t.Parallel()

	authenticator := newReadClaimsAuthenticator()

	tests := []struct {
		name  string
		token func(t *testing.T) *oidc.IDToken
	}{
		{
			name: "claims never set",
			token: func(_ *testing.T) *oidc.IDToken {
				// A token that never came out of a verifier carries no raw
				// claims JSON, so decoding fails before it starts.
				return new(oidc.IDToken)
			},
		},
		{
			name: "email is not a string",
			token: func(t *testing.T) *oidc.IDToken {
				t.Helper()

				return readClaimsToken(t, `{"sub":"user-3","email":42}`)
			},
		},
		{
			name: "name is not a string",
			token: func(t *testing.T) *oidc.IDToken {
				t.Helper()

				return readClaimsToken(t, `{"sub":"user-4","name":["Ada"]}`)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			rec := httptest.NewRecorder()

			got, ok := authenticator.readClaims(rec, readClaimsRequest(t), test.token(t))
			if ok {
				t.Fatalf("ok = true, want false (claims: %+v)", got)
			}

			if (got != idTokenClaims{Email: "", Name: "", PreferredUsername: ""}) {
				t.Errorf("claims = %+v, want the zero value", got)
			}

			if rec.Code != problem.Internal.Status {
				t.Errorf("status = %d, want %d (body: %q)", rec.Code, problem.Internal.Status, rec.Body.String())
			}

			if contentType := rec.Header().Get("Content-Type"); contentType != problem.ContentType {
				t.Errorf("Content-Type = %q, want %q", contentType, problem.ContentType)
			}

			var body problem.Problem

			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("decoding problem body %q: %v", rec.Body.String(), err)
			}

			if body.Type != problem.Internal.URI() {
				t.Errorf("problem type = %q, want %q", body.Type, problem.Internal.URI())
			}

			if wantDetail := "reading ID token claims"; body.Detail != wantDetail {
				t.Errorf("problem detail = %q, want %q", body.Detail, wantDetail)
			}
		})
	}
}

// TestOIDCReadClaimsRequiresAnIDToken pins the precondition the callback
// handler upholds: readClaims dereferences the verified token instead of
// turning a nil one into a problem response.
func TestOIDCReadClaimsRequiresAnIDToken(t *testing.T) {
	t.Parallel()

	defer func() {
		if recovered := recover(); recovered == nil {
			t.Error("readClaims survived a nil ID token, want a panic")
		}
	}()

	authenticator := newReadClaimsAuthenticator()

	_, _ = authenticator.readClaims(httptest.NewRecorder(), readClaimsRequest(t), nil)
}

// newIDToken builds a minimal *oidc.IDToken carrying only the subject, which
// is the sole field buildSessionData reads from the verified token.
func newIDToken(subject string) *oidc.IDToken {
	return &oidc.IDToken{Subject: subject}
}

func TestBuildSessionData(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		rawIDToken  string
		subject     string
		claims      idTokenClaims
		wantIDToken string
		wantSubject string
		wantEmail   string
		wantName    string
	}{
		{
			name:        "name claim wins over preferred_username",
			rawIDToken:  "header.payload.signature",
			subject:     "sub-123",
			claims:      idTokenClaims{Email: "ada@example.com", Name: "Ada Lovelace", PreferredUsername: "ada"},
			wantIDToken: "header.payload.signature",
			wantSubject: "sub-123",
			wantEmail:   "ada@example.com",
			wantName:    "Ada Lovelace",
		},
		{
			name:        "empty name falls back to preferred_username",
			rawIDToken:  "raw-token",
			subject:     "sub-456",
			claims:      idTokenClaims{Email: "ada@example.com", Name: "", PreferredUsername: "ada"},
			wantIDToken: "raw-token",
			wantSubject: "sub-456",
			wantEmail:   "ada@example.com",
			wantName:    "ada",
		},
		{
			name:        "name and preferred_username both empty leaves name empty",
			rawIDToken:  "raw-token",
			subject:     "sub-789",
			claims:      idTokenClaims{Email: "ada@example.com"},
			wantIDToken: "raw-token",
			wantSubject: "sub-789",
			wantEmail:   "ada@example.com",
			wantName:    "",
		},
		{
			name:        "whitespace-only name is kept verbatim without falling back",
			rawIDToken:  "raw-token",
			subject:     "sub-ws",
			claims:      idTokenClaims{Name: "   ", PreferredUsername: "ada"},
			wantIDToken: "raw-token",
			wantSubject: "sub-ws",
			wantEmail:   "",
			wantName:    "   ",
		},
		{
			name:        "zero-value claims yield empty email and name",
			rawIDToken:  "raw-token",
			subject:     "sub-zero",
			claims:      idTokenClaims{},
			wantIDToken: "raw-token",
			wantSubject: "sub-zero",
			wantEmail:   "",
			wantName:    "",
		},
		{
			name:        "empty raw id token is passed through untouched",
			rawIDToken:  "",
			subject:     "sub-empty-raw",
			claims:      idTokenClaims{Email: "ada@example.com", Name: "Ada"},
			wantIDToken: "",
			wantSubject: "sub-empty-raw",
			wantEmail:   "ada@example.com",
			wantName:    "Ada",
		},
		{
			name:        "empty subject is stored verbatim",
			rawIDToken:  "raw-token",
			subject:     "",
			claims:      idTokenClaims{Email: "ada@example.com", Name: "Ada"},
			wantIDToken: "raw-token",
			wantSubject: "",
			wantEmail:   "ada@example.com",
			wantName:    "Ada",
		},
		{
			name:        "email is copied without validation or normalization",
			rawIDToken:  "raw-token",
			subject:     "sub-raw-email",
			claims:      idTokenClaims{Email: "  NOT-an-Email  ", Name: "Ada"},
			wantIDToken: "raw-token",
			wantSubject: "sub-raw-email",
			wantEmail:   "  NOT-an-Email  ",
			wantName:    "Ada",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := buildSessionData(tt.rawIDToken, newIDToken(tt.subject), tt.claims)

			if got.IDToken != tt.wantIDToken {
				t.Errorf("IDToken = %q, want %q", got.IDToken, tt.wantIDToken)
			}

			if got.Subject != tt.wantSubject {
				t.Errorf("Subject = %q, want %q", got.Subject, tt.wantSubject)
			}

			if got.Email != tt.wantEmail {
				t.Errorf("Email = %q, want %q", got.Email, tt.wantEmail)
			}

			if got.Name != tt.wantName {
				t.Errorf("Name = %q, want %q", got.Name, tt.wantName)
			}

			if got.UserID == uuid.Nil {
				t.Error("UserID = uuid.Nil, want a UUID derived from the subject")
			}
		})
	}
}

func TestBuildSessionDataUserIDDerivedFromSubject(t *testing.T) {
	t.Parallel()

	const subject = "sub-deterministic"

	first := buildSessionData("raw-a", newIDToken(subject), idTokenClaims{Email: "a@example.com", Name: "A"})
	second := buildSessionData("raw-b", newIDToken(subject), idTokenClaims{Email: "b@example.com", Name: "B"})

	if first.UserID != second.UserID {
		t.Errorf("UserID for the same subject differs: %v != %v", first.UserID, second.UserID)
	}

	if first.UserID == uuid.Nil {
		t.Error("UserID = uuid.Nil, want a UUID derived from the subject")
	}

	if got := first.UserID.Version(); got != 5 {
		t.Errorf("UserID version = %d, want 5 (name-based SHA-1)", got)
	}

	other := buildSessionData("raw-a", newIDToken("sub-other"), idTokenClaims{})
	if other.UserID == first.UserID {
		t.Errorf("UserID for subject %q equals UserID for %q: %v", "sub-other", subject, other.UserID)
	}
}

func TestBuildSessionDataUserIDForEmptySubject(t *testing.T) {
	t.Parallel()

	first := buildSessionData("raw-a", newIDToken(""), idTokenClaims{})
	second := buildSessionData("raw-b", newIDToken(""), idTokenClaims{Name: "Ada"})

	if first.UserID == uuid.Nil {
		t.Error("UserID = uuid.Nil, want a deterministic UUID even for an empty subject")
	}

	if first.UserID != second.UserID {
		t.Errorf("UserID for the empty subject differs between calls: %v != %v", first.UserID, second.UserID)
	}

	nonEmpty := buildSessionData("raw-a", newIDToken("sub-123"), idTokenClaims{})
	if first.UserID == nonEmpty.UserID {
		t.Errorf("UserID for the empty subject equals UserID for %q: %v", "sub-123", first.UserID)
	}
}

// observeErrors returns a logger backed by an in-memory core, so a test can
// assert whether establishSession reported a failure it is documented to log.
func observeErrors() (*zap.Logger, *observer.ObservedLogs) {
	core, logs := observer.New(zapcore.ErrorLevel)

	return zap.New(core), logs
}

// assertEstablishProblem checks the response is the RFC 9457 internal problem
// establishSession writes itself, naming the step that failed in its detail.
func assertEstablishProblem(t *testing.T, rec *httptest.ResponseRecorder, instance, detail string) {
	t.Helper()

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}

	if contentType := rec.Header().Get("Content-Type"); contentType != problem.ContentType {
		t.Errorf("Content-Type = %q, want %q", contentType, problem.ContentType)
	}

	var got problem.Problem
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decoding problem body %q: %v", rec.Body.String(), err)
	}

	if got.Type != problem.Internal.URI() || got.Status != http.StatusInternalServerError {
		t.Errorf("problem type/status = %q/%d, want %q/%d",
			got.Type, got.Status, problem.Internal.URI(), http.StatusInternalServerError)
	}

	if got.Detail != detail {
		t.Errorf("problem detail = %q, want %q", got.Detail, detail)
	}

	if got.Instance != instance {
		t.Errorf("problem instance = %q, want %q", got.Instance, instance)
	}
}

// TestOIDCEstablishSession covers the shared final login step: renew, store,
// upsert — in that order, short-circuiting on the first failure.
//
// The putSessionData branch ("storing session") is unreachable from real
// inputs: SessionData is only strings and a UUID, so json.Marshal cannot fail
// and scs.Put returns nothing. It is therefore deliberately not exercised.
func TestOIDCEstablishSession(t *testing.T) {
	t.Parallel()

	session := SessionData{
		UserID:  uuid.MustParse("55555555-5555-5555-5555-555555555555"),
		IDToken: "header.payload.signature",
		Subject: "oidc|subject-1",
		Email:   "bob@example.com",
		Name:    "Bob",
	}

	cases := []struct {
		name string
		data SessionData
		// storeDeleteErr fails RenewToken; upsertErr fails members.Upsert.
		storeDeleteErr error
		upsertErr      error
		// cancelCtx cancels the request context before the call, so the
		// repository failure stands in for a canceled database round trip.
		cancelCtx   bool
		want        bool
		wantRenewed bool
		wantStored  bool
		wantUpserts int
		// wantDetail names the failing step in the problem response; it is
		// empty when the call is expected to succeed.
		wantDetail string
		wantLogged bool
	}{
		{
			name:           "success",
			data:           session,
			storeDeleteErr: nil,
			upsertErr:      nil,
			cancelCtx:      false,
			want:           true,
			wantRenewed:    true,
			wantStored:     true,
			wantUpserts:    1,
			wantDetail:     "",
			wantLogged:     false,
		},
		{
			// Password mode resolves the same shape without an ID token; the
			// hint stays empty and never reaches the members repository.
			name: "empty id token is accepted",
			data: SessionData{
				UserID:  session.UserID,
				IDToken: "",
				Subject: session.Subject,
				Email:   session.Email,
				Name:    session.Name,
			},
			storeDeleteErr: nil,
			upsertErr:      nil,
			cancelCtx:      false,
			want:           true,
			wantRenewed:    true,
			wantStored:     true,
			wantUpserts:    1,
			wantDetail:     "",
			wantLogged:     false,
		},
		{
			// No validation happens here: a nil user id is stored and upserted
			// as-is (requireSession is what rejects it on later requests).
			name: "zero-value session data is stored unvalidated",
			data: SessionData{
				UserID:  uuid.Nil,
				IDToken: "",
				Subject: "",
				Email:   "",
				Name:    "",
			},
			storeDeleteErr: nil,
			upsertErr:      nil,
			cancelCtx:      false,
			want:           true,
			wantRenewed:    true,
			wantStored:     true,
			wantUpserts:    1,
			wantDetail:     "",
			wantLogged:     false,
		},
		{
			// Session fixation guard fails first: nothing is stored, no member
			// is written, and the failure is not logged.
			name:           "renew token fails",
			data:           session,
			storeDeleteErr: errRenewDelete,
			upsertErr:      nil,
			cancelCtx:      false,
			want:           false,
			wantRenewed:    false,
			wantStored:     false,
			wantUpserts:    0,
			wantDetail:     "renewing session",
			wantLogged:     false,
		},
		{
			// No rollback: the token stays renewed and the data stays in the
			// session even though the login is reported as failed.
			name:           "member upsert fails after session data was stored",
			data:           session,
			storeDeleteErr: nil,
			upsertErr:      errMemberUpsert,
			cancelCtx:      false,
			want:           false,
			wantRenewed:    true,
			wantStored:     true,
			wantUpserts:    1,
			wantDetail:     "recording membership",
			wantLogged:     true,
		},
		{
			name:           "canceled request context surfaces as an upsert failure",
			data:           session,
			storeDeleteErr: nil,
			upsertErr:      context.Canceled,
			cancelCtx:      true,
			want:           false,
			wantRenewed:    true,
			wantStored:     true,
			wantUpserts:    1,
			wantDetail:     "recording membership",
			wantLogged:     true,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			sessions := scs.New()
			sessions.Store = renewFailStore{Store: sessions.Store, deleteErr: testCase.storeDeleteErr}
			memberRepo := &recordingMembers{
				mu:    sync.Mutex{},
				err:   testCase.upsertErr,
				calls: 0,
				member: members.Member{
					Subject:   "",
					UserID:    uuid.Nil,
					Email:     "",
					Name:      "",
					CreatedAt: time.Time{},
					UpdatedAt: time.Time{},
				},
			}
			logger, logs := observeErrors()
			authenticator := &oidcAuthenticator{
				oauth2Config:          nil,
				verifier:              nil,
				sm:                    sessions,
				members:               memberRepo,
				clientID:              "splitkauf",
				endSessionEndpoint:    "",
				postLogoutRedirectURL: "",
				logger:                logger,
			}

			// A committed session gives the request an scs-loaded context with
			// a non-empty token, so RenewToken has an old token to delete —
			// without a loaded context RenewToken and Put panic outright.
			seeded := seedSession(t, sessions, func(context.Context) {})

			ctx, err := sessions.Load(t.Context(), seeded)
			if err != nil {
				t.Fatalf("Load: %v", err)
			}

			if testCase.cancelCtx {
				canceled, cancel := context.WithCancel(ctx)
				cancel()

				ctx = canceled
			}

			req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/api/auth/callback", nil)
			rec := httptest.NewRecorder()

			if got := authenticator.establishSession(rec, req, testCase.data); got != testCase.want {
				t.Fatalf("establishSession = %v, want %v", got, testCase.want)
			}

			// Session fixation: the token is renewed before anything is stored,
			// so a failed renew must leave the session untouched.
			if renewed := sessions.Token(ctx) != seeded; renewed != testCase.wantRenewed {
				t.Errorf("session token renewed = %v, want %v", renewed, testCase.wantRenewed)
			}

			assertSessionData(t, ctx, sessions, testCase.data, testCase.wantStored)
			assertMemberFromSessionData(t, memberRepo, testCase.data, testCase.wantUpserts)

			if logged := logs.FilterMessage("upserting member").Len(); (logged > 0) != testCase.wantLogged {
				t.Errorf("logged %d %q entries, want logged = %v",
					logged, "upserting member", testCase.wantLogged)
			}

			if testCase.want {
				assertNothingWritten(t, rec)

				return
			}

			assertEstablishProblem(t, rec, req.URL.Path, testCase.wantDetail)
		})
	}
}

// assertSessionData checks whether the session carries exactly the data that
// was handed to establishSession, and that it carries none when stored is false.
func assertSessionData(
	t *testing.T,
	ctx context.Context,
	sessions *scs.SessionManager,
	data SessionData,
	stored bool,
) {
	t.Helper()

	got, ok := getSessionData(ctx, sessions)
	if ok != stored {
		t.Fatalf("session data present = %v, want %v", ok, stored)
	}

	if !stored {
		return
	}

	if got != data {
		t.Errorf("stored session data = %+v, want %+v", got, data)
	}
}

// assertMemberFromSessionData checks how often the members repository was
// called and, when it was, that the member mirrors the session data with the
// repository-owned timestamps left zero.
func assertMemberFromSessionData(
	t *testing.T,
	repo *recordingMembers,
	data SessionData,
	wantCalls int,
) {
	t.Helper()

	calls, last := repo.upserted()
	if calls != wantCalls {
		t.Errorf("members.Upsert called %d times, want %d", calls, wantCalls)
	}

	if wantCalls == 0 {
		return
	}

	// The ID token is session-only state and never reaches the members table.
	want := members.Member{
		Subject:   data.Subject,
		UserID:    data.UserID,
		Email:     data.Email,
		Name:      data.Name,
		CreatedAt: time.Time{},
		UpdatedAt: time.Time{},
	}
	if last != want {
		t.Errorf("upserted member = %+v, want %+v", last, want)
	}
}
