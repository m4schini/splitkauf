// SPDX-License-Identifier: CC0-1.0

package web_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/alexedwards/scs/v2"

	"github.com/m4schini/splitkauf/auth"
	"github.com/m4schini/splitkauf/config"
	"github.com/m4schini/splitkauf/events"
	"github.com/m4schini/splitkauf/members"
	"github.com/m4schini/splitkauf/ports/rest"
	v1 "github.com/m4schini/splitkauf/ports/rest/v1"
)

// devHandler builds the full REST handler in dev-auth mode for these tests.
func devHandler(t *testing.T, impl v1.ServerInterface) http.Handler {
	t.Helper()

	sessions := scs.New()

	var cfg config.Config

	authr, err := auth.New(context.Background(), &cfg, sessions, noopMembers{}, nil)
	if err != nil {
		t.Fatalf("auth.New (dev): %v", err)
	}

	return rest.New(impl, sessions, authr, events.NewBroker())
}

// testGet issues a GET request against url using the test's context and fails
// the test on a transport error. The caller owns closing the response body.
func testGet(t *testing.T, url string) *http.Response {
	t.Helper()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("building GET %s: %v", url, err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}

	return resp
}

type noopMembers struct{}

func (noopMembers) Upsert(context.Context, members.Member) error { return nil }
func (noopMembers) Get(context.Context, string) (members.Member, error) {
	return members.Member{}, members.ErrNotFound
}

func TestMain(m *testing.M) {
	if err := config.Load(); err != nil {
		panic(err)
	}

	spec, err := os.ReadFile("../../splitkauf.openapi.yaml")
	if err != nil {
		panic(err)
	}

	rest.SetOpenAPISpec(spec)
	os.Exit(m.Run())
}

func TestRootServesIndexHTML(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(devHandler(t, &v1.V1{DB: nil, Service: nil, Events: nil}))
	defer srv.Close()

	resp := testGet(t, srv.URL+"/")

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type = %q, want text/html prefix", ct)
	}
}

func TestSPARouteFallsBackToIndexHTML(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(devHandler(t, &v1.V1{DB: nil, Service: nil, Events: nil}))
	defer srv.Close()

	resp := testGet(t, srv.URL+"/some/spa/route")

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type = %q, want text/html prefix", ct)
	}
}

func TestMissingFileWithExtensionReturns404(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(devHandler(t, &v1.V1{DB: nil, Service: nil, Events: nil}))
	defer srv.Close()

	resp := testGet(t, srv.URL+"/does-not-exist.js")

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusNotFound)
	}
}

func TestAPIHealthStillReachable(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(devHandler(t, &v1.V1{DB: nil, Service: nil, Events: nil}))
	defer srv.Close()

	resp := testGet(t, srv.URL+"/api/v1/health")

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var got v1.HealthStatus
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decoding response: %v", err)
	}

	if got.Status == "" {
		t.Error("status is empty, want non-empty")
	}
}
