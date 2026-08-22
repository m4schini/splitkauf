// SPDX-License-Identifier: CC0-1.0

package auth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/alexedwards/scs/v2"
	"github.com/google/uuid"

	"github.com/m4schini/splitkauf/config"
	"github.com/m4schini/splitkauf/members"
)

func TestMain(m *testing.M) {
	// telemetry.Logger (used by the OIDC authenticator) reads config.C.
	if err := config.Load(); err != nil {
		panic(err)
	}

	m.Run()
}

func TestSafeReturnTo(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in   string
		want string
	}{
		{"", "/"},
		{"/", "/"},
		{"/lists", "/lists"},
		{"/lists/123", "/lists/123"},
		{"/lists?filter=open", "/lists?filter=open"},
		// Absolute URLs must be rejected.
		{"http://evil.com", "/"},
		{"https://evil.com/path", "/"},
		{"ftp://evil.com", "/"},
		// Scheme-relative (protocol-relative) URLs must be rejected.
		{"//evil.com", "/"},
		{"//evil.com/path", "/"},
		// Backslash variants browsers may normalise to "//".
		{"/\\evil.com", "/"},
		{"\\\\evil.com", "/"},
		// Non-rooted paths must be rejected.
		{"relative/path", "/"},
		{"lists", "/"},
	}
	for _, c := range cases {
		if got := safeReturnTo(c.in); got != c.want {
			t.Errorf("safeReturnTo(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestRandomTokenIsRandomAndSized(t *testing.T) {
	t.Parallel()

	first, err := randomToken()
	if err != nil {
		t.Fatalf("randomToken: %v", err)
	}

	second, err := randomToken()
	if err != nil {
		t.Fatalf("randomToken: %v", err)
	}

	if first == "" || second == "" {
		t.Fatal("randomToken returned empty string")
	}

	if first == second {
		t.Fatal("randomToken returned identical values on two calls")
	}

	raw, err := base64.RawURLEncoding.DecodeString(first)
	if err != nil {
		t.Fatalf("token is not valid base64url: %v", err)
	}

	if len(raw) != randomTokenBytes {
		t.Errorf("decoded token = %d bytes, want %d", len(raw), randomTokenBytes)
	}
}

func TestWithUserUserFromRoundTrip(t *testing.T) {
	t.Parallel()

	want := User{ID: devUserID, Name: "Alice", Email: "alice@example.com"}
	ctx := WithUser(context.Background(), want)

	got, ok := UserFrom(ctx)
	if !ok {
		t.Fatal("UserFrom returned ok=false after WithUser")
	}

	if got != want {
		t.Errorf("UserFrom = %+v, want %+v", got, want)
	}

	// Absent from a bare context.
	if _, ok := UserFrom(context.Background()); ok {
		t.Error("UserFrom returned ok=true for a context with no user")
	}
}

func TestNewSelectsDevWhenOIDCDisabled(t *testing.T) {
	t.Parallel()

	var cfg config.Config // no OIDC issuer/client → dev-auth

	authenticator, err := New(context.Background(), &cfg, scs.New(), noopMembers{}, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if _, ok := authenticator.(*devAuthenticator); !ok {
		t.Errorf("New returned %T, want *devAuthenticator", authenticator)
	}
}

func TestNewSelectsOIDCWhenEnabled(t *testing.T) {
	t.Parallel()

	issuer := newDiscoveryServer(t)

	var cfg config.Config

	cfg.Auth.OIDC.Issuer = issuer
	cfg.Auth.OIDC.ClientID = "client-id"
	cfg.Auth.OIDC.ClientSecret = "client-secret"
	cfg.Auth.OIDC.RedirectURL = "https://app.example.com/api/auth/callback"

	if !cfg.IsOIDCEnabled() {
		t.Fatal("IsOIDCEnabled = false for a complete OIDC config")
	}

	authenticator, err := New(context.Background(), &cfg, scs.New(), noopMembers{}, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	oidcAuth, ok := authenticator.(*oidcAuthenticator)
	if !ok {
		t.Fatalf("New returned %T, want *oidcAuthenticator", authenticator)
	}

	if oidcAuth.endSessionEndpoint == "" {
		t.Error("expected end_session_endpoint to be read from discovery metadata")
	}
	// Authentication-only scopes: exactly these three, nothing that would keep
	// tokens alive beyond login.
	wantScopes := []string{"openid", "profile", "email"}
	if !slices.Equal(oidcAuth.oauth2Config.Scopes, wantScopes) {
		t.Errorf("configured scopes = %v, want %v", oidcAuth.oauth2Config.Scopes, wantScopes)
	}
}

func TestDevAuthenticatorInjectsDevUser(t *testing.T) {
	t.Parallel()

	var (
		got   User
		gotOK bool
	)

	handler := newDev().RequireAuth(http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {
		got, gotOK = UserFrom(req.Context())

		res.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/me", nil))

	if !gotOK {
		t.Fatal("dev RequireAuth did not inject a user into the context")
	}

	if got != DevUser {
		t.Errorf("injected user = %+v, want %+v", got, DevUser)
	}
}

func TestDevAuthenticatorEndpoints(t *testing.T) {
	t.Parallel()

	dev := newDev()

	// Login redirects home (302 /).
	rec := httptest.NewRecorder()
	dev.Login(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/auth/login", nil))

	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/" {
		t.Errorf("Login = %d %q, want 302 /", rec.Code, rec.Header().Get("Location"))
	}

	// Logout redirects home (302 /).
	rec = httptest.NewRecorder()
	dev.Logout(rec, httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/auth/logout", nil))

	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/" {
		t.Errorf("Logout = %d %q, want 302 /", rec.Code, rec.Header().Get("Location"))
	}

	// Callback is 404 (no login flow in dev mode).
	rec = httptest.NewRecorder()
	dev.Callback(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/auth/callback", nil))

	if rec.Code != http.StatusNotFound {
		t.Errorf("Callback = %d, want 404", rec.Code)
	}
}

func TestSessionDataRoundTrip(t *testing.T) {
	t.Parallel()

	sessions := scs.New()

	ctx, err := sessions.Load(context.Background(), "")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	// No data yet.
	if _, ok := getSessionData(ctx, sessions); ok {
		t.Fatal("getSessionData returned ok=true for an empty session")
	}

	want := SessionData{
		UserID:  uuid.MustParse("22222222-2222-2222-2222-222222222222"),
		IDToken: "id",
		Subject: "sub-123",
		Email:   "user@example.com",
		Name:    "User",
	}
	if err := putSessionData(ctx, sessions, want); err != nil {
		t.Fatalf("putSessionData: %v", err)
	}

	got, ok := getSessionData(ctx, sessions)
	if !ok {
		t.Fatal("getSessionData returned ok=false after put")
	}

	if got != want {
		t.Errorf("round-trip = %+v, want %+v", got, want)
	}
}

func TestSubjectUUIDIsStable(t *testing.T) {
	t.Parallel()

	first := subjectUUID("subject-abc")
	second := subjectUUID("subject-abc")
	other := subjectUUID("subject-xyz")

	if first != second {
		t.Error("subjectUUID is not deterministic for the same subject")
	}

	if first == other {
		t.Error("subjectUUID collided for different subjects")
	}
}

// noopMembers is a members.Repository that records nothing; sufficient for
// constructor tests that never sign a user in.
type noopMembers struct{}

func (noopMembers) Upsert(context.Context, members.Member) error { return nil }
func (noopMembers) Get(context.Context, string) (members.Member, error) {
	return members.Member{}, members.ErrNotFound
}

// newDiscoveryServer starts an httptest server that serves a minimal OIDC
// discovery document (no live IdP), and returns its issuer URL. It lets
// newOIDC's provider discovery succeed without a real provider.
func newDiscoveryServer(t *testing.T) string {
	t.Helper()

	mux := http.NewServeMux()

	var issuer string

	mux.HandleFunc("/.well-known/openid-configuration", func(res http.ResponseWriter, _ *http.Request) {
		doc := map[string]any{
			"issuer":                                issuer,
			"authorization_endpoint":                issuer + "/authorize",
			"token_endpoint":                        issuer + "/token",
			"jwks_uri":                              issuer + "/jwks",
			"end_session_endpoint":                  issuer + "/logout",
			"id_token_signing_alg_values_supported": []string{"RS256"},
		}

		res.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(res).Encode(doc)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	issuer = srv.URL

	return issuer
}
