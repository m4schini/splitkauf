// SPDX-License-Identifier: CC0-1.0

package rest_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/alexedwards/scs/v2"

	"github.com/m4schini/splitkauf/auth"
	"github.com/m4schini/splitkauf/config"
	"github.com/m4schini/splitkauf/events"
	"github.com/m4schini/splitkauf/members"
	"github.com/m4schini/splitkauf/ports/rest"
	"github.com/m4schini/splitkauf/ports/rest/problem"
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

// TestProblemPagesNoDrift proves that every emitted problem type resolves to an
// HTML explanation page (RFC 9457: about:blank is never used). Iterating the
// registry catches drift where a new type is added without a page.
func TestProblemPagesNoDrift(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(devHandler(t, &v1.V1{DB: nil, Service: nil, Events: nil}))
	// Cleanup, not defer: the parallel subtests outlive this function body, and
	// t.Cleanup runs only after they have all finished.
	t.Cleanup(srv.Close)

	for _, probType := range problem.Types() {
		t.Run(probType.Slug, func(t *testing.T) {
			t.Parallel()

			resp := testGet(t, srv.URL+probType.URI())

			defer func() { _ = resp.Body.Close() }()

			if resp.StatusCode != http.StatusOK {
				t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
			}

			if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
				t.Errorf("Content-Type = %q, want text/html…", ct)
			}

			body, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatalf("reading body: %v", err)
			}

			if !strings.Contains(string(body), probType.Title) {
				t.Errorf("page for %q does not contain title %q", probType.Slug, probType.Title)
			}
		})
	}
}

func TestProblemPageUnknownSlugReturns404(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(devHandler(t, &v1.V1{DB: nil, Service: nil, Events: nil}))
	defer srv.Close()

	resp := testGet(t, srv.URL+"/problems/does-not-exist")

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusNotFound)
	}

	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type = %q, want text/html…", ct)
	}
}
