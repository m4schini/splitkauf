// SPDX-License-Identifier: TODO

package auth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alexedwards/scs/v2"

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
	a, err := randomToken()
	if err != nil {
		t.Fatalf("randomToken: %v", err)
	}
	b, err := randomToken()
	if err != nil {
		t.Fatalf("randomToken: %v", err)
	}
	if a == "" || b == "" {
		t.Fatal("randomToken returned empty string")
	}
	if a == b {
		t.Fatal("randomToken returned identical values on two calls")
	}
	raw, err := base64.RawURLEncoding.DecodeString(a)
	if err != nil {
		t.Fatalf("token is not valid base64url: %v", err)
	}
	if len(raw) != randomTokenBytes {
		t.Errorf("decoded token = %d bytes, want %d", len(raw), randomTokenBytes)
	}
}

func TestWithUserUserFromRoundTrip(t *testing.T) {
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
	cfg := &config.Config{} // no OIDC issuer/client → dev-auth
	a, err := New(context.Background(), cfg, scs.New(), noopMembers{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, ok := a.(*devAuthenticator); !ok {
		t.Errorf("New returned %T, want *devAuthenticator", a)
	}
}

func TestNewSelectsOIDCWhenEnabled(t *testing.T) {
	issuer := newDiscoveryServer(t)
	cfg := &config.Config{}
	cfg.Auth.OIDC.Issuer = issuer
	cfg.Auth.OIDC.ClientID = "client-id"
	cfg.Auth.OIDC.ClientSecret = "client-secret"
	cfg.Auth.OIDC.RedirectURL = "https://app.example.com/api/auth/callback"

	if !cfg.IsOIDCEnabled() {
		t.Fatal("IsOIDCEnabled = false for a complete OIDC config")
	}

	a, err := New(context.Background(), cfg, scs.New(), noopMembers{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	oa, ok := a.(*oidcAuthenticator)
	if !ok {
		t.Fatalf("New returned %T, want *oidcAuthenticator", a)
	}
	if oa.endSessionEndpoint == "" {
		t.Error("expected end_session_endpoint to be read from discovery metadata")
	}
}

func TestDevAuthenticatorInjectsDevUser(t *testing.T) {
	var got User
	var ok bool
	handler := newDev().RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, ok = UserFrom(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/me", nil))

	if !ok {
		t.Fatal("dev RequireAuth did not inject a user into the context")
	}
	if got != DevUser {
		t.Errorf("injected user = %+v, want %+v", got, DevUser)
	}
}

func TestDevAuthenticatorEndpoints(t *testing.T) {
	d := newDev()

	// Login redirects home (302 /).
	rec := httptest.NewRecorder()
	d.Login(rec, httptest.NewRequest(http.MethodGet, "/api/auth/login", nil))
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/" {
		t.Errorf("Login = %d %q, want 302 /", rec.Code, rec.Header().Get("Location"))
	}

	// Logout redirects home (302 /).
	rec = httptest.NewRecorder()
	d.Logout(rec, httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil))
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/" {
		t.Errorf("Logout = %d %q, want 302 /", rec.Code, rec.Header().Get("Location"))
	}

	// Callback is 404 (no login flow in dev mode).
	rec = httptest.NewRecorder()
	d.Callback(rec, httptest.NewRequest(http.MethodGet, "/api/auth/callback", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("Callback = %d, want 404", rec.Code)
	}
}

func TestSessionDataRoundTrip(t *testing.T) {
	sm := scs.New()
	ctx, err := sm.Load(context.Background(), "")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	// No data yet.
	if _, ok := getSessionData(ctx, sm); ok {
		t.Fatal("getSessionData returned ok=true for an empty session")
	}

	want := SessionData{
		AccessToken:  "access",
		RefreshToken: "refresh",
		IDToken:      "id",
		Expiry:       time.Now().Add(time.Hour).UTC().Truncate(time.Second),
		Subject:      "sub-123",
		Email:        "user@example.com",
		Name:         "User",
	}
	if err := putSessionData(ctx, sm, want); err != nil {
		t.Fatalf("putSessionData: %v", err)
	}

	got, ok := getSessionData(ctx, sm)
	if !ok {
		t.Fatal("getSessionData returned ok=false after put")
	}
	if got != want {
		t.Errorf("round-trip = %+v, want %+v", got, want)
	}
}

func TestSubjectUUIDIsStable(t *testing.T) {
	a := subjectUUID("subject-abc")
	b := subjectUUID("subject-abc")
	c := subjectUUID("subject-xyz")
	if a != b {
		t.Error("subjectUUID is not deterministic for the same subject")
	}
	if a == c {
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
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		doc := map[string]any{
			"issuer":                                issuer,
			"authorization_endpoint":                issuer + "/authorize",
			"token_endpoint":                        issuer + "/token",
			"jwks_uri":                              issuer + "/jwks",
			"end_session_endpoint":                  issuer + "/logout",
			"id_token_signing_alg_values_supported": []string{"RS256"},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(doc)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	issuer = srv.URL
	return issuer
}
