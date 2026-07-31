// SPDX-License-Identifier: TODO

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
	"github.com/m4schini/splitkauf/members"
	"github.com/m4schini/splitkauf/ports/rest"
	"github.com/m4schini/splitkauf/ports/rest/problem"
	v1 "github.com/m4schini/splitkauf/ports/rest/v1"
)

// devHandler builds the full REST handler in dev-auth mode for these tests.
func devHandler(t *testing.T, si v1.ServerInterface) http.Handler {
	t.Helper()
	sm := scs.New()
	authr, err := auth.New(context.Background(), &config.Config{}, sm, noopMembers{})
	if err != nil {
		t.Fatalf("auth.New (dev): %v", err)
	}
	return rest.New(si, sm, authr)
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
	srv := httptest.NewServer(devHandler(t, &v1.V1{}))
	defer srv.Close()

	for _, ty := range problem.Types() {
		t.Run(ty.Slug, func(t *testing.T) {
			resp, err := http.Get(srv.URL + ty.URI())
			if err != nil {
				t.Fatalf("GET %s: %v", ty.URI(), err)
			}
			defer resp.Body.Close()

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
			if !strings.Contains(string(body), ty.Title) {
				t.Errorf("page for %q does not contain title %q", ty.Slug, ty.Title)
			}
		})
	}
}

func TestProblemPageUnknownSlugReturns404(t *testing.T) {
	srv := httptest.NewServer(devHandler(t, &v1.V1{}))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/problems/does-not-exist")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusNotFound)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type = %q, want text/html…", ct)
	}
}
