// SPDX-License-Identifier: CC0-1.0

package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/alexedwards/scs/v2"
	"github.com/google/uuid"

	"github.com/m4schini/splitkauf/config"
	"github.com/m4schini/splitkauf/users"
)

// stubUsers is a users.Repository with no accounts; enough for wiring tests
// that only exercise the credential-miss path.
type stubUsers struct{}

func (stubUsers) Create(context.Context, users.NewUser) (users.User, error) {
	var none users.User

	return none, nil
}

func (stubUsers) GetByUsername(context.Context, string) (users.User, string, error) {
	return users.User{}, "", users.ErrNotFound
}

// newCombinedForTest builds the combined authenticator against the mocked
// discovery server, plus its session manager.
func newCombinedForTest(t *testing.T) (*combinedAuthenticator, *scs.SessionManager) {
	t.Helper()

	issuer := newDiscoveryServer(t)

	var cfg config.Config

	cfg.Auth.OIDC.Issuer = issuer
	cfg.Auth.OIDC.ClientID = "client-id"
	cfg.Auth.OIDC.ClientSecret = "client-secret"
	cfg.Auth.OIDC.RedirectURL = "https://app.example.com/api/auth/callback"
	cfg.Auth.Password.Enabled = true

	if cfg.Mode() != config.AuthModeCombined {
		t.Fatalf("Mode() = %q, want %q", cfg.Mode(), config.AuthModeCombined)
	}

	sessions := scs.New()

	authenticator, err := New(context.Background(), &cfg, sessions, noopMembers{}, stubUsers{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	combined, ok := authenticator.(*combinedAuthenticator)
	if !ok {
		t.Fatalf("New returned %T, want *combinedAuthenticator", authenticator)
	}

	return combined, sessions
}

func TestCombinedLoginDispatchesOnMethod(t *testing.T) {
	t.Parallel()

	combined, sessions := newCombinedForTest(t)

	// GET starts the OIDC redirect flow.
	rec := httptest.NewRecorder()
	sessions.LoadAndSave(http.HandlerFunc(combined.Login)).
		ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/auth/login", nil))

	if rec.Code != http.StatusFound {
		t.Fatalf("GET login status = %d, want 302", rec.Code)
	}

	if loc := rec.Header().Get("Location"); !strings.Contains(loc, "/authorize") {
		t.Errorf("GET login redirects to %q, want the IdP authorization endpoint", loc)
	}

	// POST carries password credentials; an unknown user is a clean 401 from
	// the password path, not an OIDC redirect.
	rec = httptest.NewRecorder()
	body := strings.NewReader(`{"username":"ghost","password":"nope"}`)
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/auth/login", body)
	sessions.LoadAndSave(http.HandlerFunc(combined.Login)).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("POST login (unknown user) status = %d, want 401", rec.Code)
	}
}

func TestCombinedLogoutDispatchesOnSessionOrigin(t *testing.T) {
	t.Parallel()

	combined, sessions := newCombinedForTest(t)
	handler := sessions.LoadAndSave(http.HandlerFunc(combined.Logout))

	// A password session (no ID token) is destroyed locally and sent home,
	// never to the IdP.
	token := seedSession(t, sessions, func(ctx context.Context) {
		data := SessionData{
			UserID:  uuid.Nil,
			IDToken: "",
			Subject: "local-user",
			Email:   "",
			Name:    "",
		}
		if err := putSessionData(ctx, sessions, data); err != nil {
			t.Fatalf("putSessionData: %v", err)
		}
	})
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/auth/logout", nil)
	req.AddCookie(sessionCookie(sessions.Cookie.Name, token))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/" {
		t.Errorf("password-session logout = %d %q, want 302 /", rec.Code, rec.Header().Get("Location"))
	}

	// An OIDC session (ID token present) gets RP-initiated logout at the IdP.
	token = seedSession(t, sessions, func(ctx context.Context) {
		data := SessionData{
			UserID:  uuid.Nil,
			IDToken: "fake-id-token",
			Subject: "oidc-user",
			Email:   "",
			Name:    "",
		}
		if err := putSessionData(ctx, sessions, data); err != nil {
			t.Fatalf("putSessionData: %v", err)
		}
	})
	req = httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/auth/logout", nil)
	req.AddCookie(sessionCookie(sessions.Cookie.Name, token))

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("oidc-session logout status = %d, want 302", rec.Code)
	}

	loc := rec.Header().Get("Location")
	if !strings.Contains(loc, "/logout") || !strings.Contains(loc, "id_token_hint=fake-id-token") {
		t.Errorf("oidc-session logout redirects to %q, want the IdP end-session endpoint with the hint", loc)
	}
}
