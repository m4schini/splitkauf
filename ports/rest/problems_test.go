// SPDX-License-Identifier: TODO

package rest_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/m4schini/splitkauf/ports/rest"
	"github.com/m4schini/splitkauf/ports/rest/problem"
	v1 "github.com/m4schini/splitkauf/ports/rest/v1"
)

// TestProblemPagesNoDrift proves that every emitted problem type resolves to an
// HTML explanation page (RFC 9457: about:blank is never used). Iterating the
// registry catches drift where a new type is added without a page.
func TestProblemPagesNoDrift(t *testing.T) {
	srv := httptest.NewServer(rest.New(&v1.V1{}))
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
	srv := httptest.NewServer(rest.New(&v1.V1{}))
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
