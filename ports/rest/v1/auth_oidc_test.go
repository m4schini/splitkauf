// SPDX-License-Identifier: CC0-1.0

package v1_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/alexedwards/scs/v2"

	"github.com/m4schini/splitkauf/auth"
	"github.com/m4schini/splitkauf/config"
	"github.com/m4schini/splitkauf/events"
	"github.com/m4schini/splitkauf/lists"
	"github.com/m4schini/splitkauf/ports/rest"
	v1 "github.com/m4schini/splitkauf/ports/rest/v1"
)

// newOIDCServer builds a full-stack REST server in OIDC mode against a local
// discovery stub (no live IdP), backed by a fake service that fails the test if
// it is ever reached without authentication.
func newOIDCServer(t *testing.T) *httptest.Server {
	t.Helper()

	sessions := scs.New()

	var cfg config.Config

	cfg.Auth.OIDC.Issuer = newDiscoveryServer(t)
	cfg.Auth.OIDC.ClientID = "client-id"
	cfg.Auth.OIDC.ClientSecret = "client-secret"

	cfg.Auth.OIDC.RedirectURL = "https://app.example.com/api/auth/callback"
	if !cfg.IsOIDCEnabled() {
		t.Fatal("test config is not OIDC-enabled")
	}

	authr, err := auth.New(t.Context(), &cfg, sessions, noopMembers{}, nil)
	if err != nil {
		t.Fatalf("auth.New (oidc): %v", err)
	}

	// The service must never be reached for an unauthenticated request.
	var svc fakeService

	svc.listsFn = func(context.Context) ([]lists.List, error) {
		t.Error("service should not be called for an unauthenticated request")

		return nil, nil
	}

	srv := httptest.NewServer(rest.New(&v1.V1{DB: nil, Service: &svc, Events: nil}, sessions, authr, events.NewBroker()))
	t.Cleanup(srv.Close)

	return srv
}

// TestOIDCUnauthenticated401 is the key auth-mode acceptance check: in OIDC
// mode, a request to a guarded /api/v1 endpoint with no session is rejected by
// RequireAuth with a 401 application/problem+json unauthorized problem — before
// the handler runs. The provider is a local discovery stub (no live IdP).
func TestOIDCUnauthenticated401(t *testing.T) {
	t.Parallel()

	srv := newOIDCServer(t)

	resp := getURL(t, srv.URL+"/api/v1/lists")
	defer closeBody(t, resp)

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}

	if ct := resp.Header.Get("Content-Type"); ct != contentTypeProblem {
		t.Errorf("content-type = %q, want %s", ct, contentTypeProblem)
	}

	var prob struct {
		Type   string `json:"type"`
		Status int    `json:"status"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&prob); err != nil {
		t.Fatalf("decode problem: %v", err)
	}

	if prob.Type != "/problems/unauthorized" {
		t.Errorf("type = %q, want /problems/unauthorized", prob.Type)
	}

	if prob.Status != http.StatusUnauthorized {
		t.Errorf("problem status = %d, want 401", prob.Status)
	}
}

// TestOIDCHealthPublic proves that even in OIDC mode the health endpoint stays
// publicly reachable: GET /api/v1/health returns 200 with no session, while a
// guarded route (GET /api/v1/lists) is still rejected with 401. This is the
// wiring-layer carve-out in rest.New that bypasses RequireAuth for health only.
func TestOIDCHealthPublic(t *testing.T) {
	t.Parallel()

	srv := newOIDCServer(t)

	// Health is public: 200 even with no session.
	health := getURL(t, srv.URL+"/api/v1/health")
	defer closeBody(t, health)

	if health.StatusCode != http.StatusOK {
		t.Fatalf("health status = %d, want 200", health.StatusCode)
	}

	// A guarded route is still rejected: 401.
	guarded := getURL(t, srv.URL+"/api/v1/lists")
	defer closeBody(t, guarded)

	if guarded.StatusCode != http.StatusUnauthorized {
		t.Fatalf("lists status = %d, want 401", guarded.StatusCode)
	}
}

// newDiscoveryServer starts an httptest server serving a minimal OIDC discovery
// document so auth.New's provider discovery succeeds without a live IdP.
func newDiscoveryServer(t *testing.T) string {
	t.Helper()

	mux := http.NewServeMux()

	var issuer string

	mux.HandleFunc("/.well-known/openid-configuration", func(writer http.ResponseWriter, _ *http.Request) {
		doc := map[string]any{
			"issuer":                                issuer,
			"authorization_endpoint":                issuer + "/authorize",
			"token_endpoint":                        issuer + "/token",
			"jwks_uri":                              issuer + "/jwks",
			"end_session_endpoint":                  issuer + "/logout",
			"id_token_signing_alg_values_supported": []string{"RS256"},
		}

		writer.Header().Set("Content-Type", contentTypeJSON)
		_ = json.NewEncoder(writer).Encode(doc)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	issuer = srv.URL

	return issuer
}
