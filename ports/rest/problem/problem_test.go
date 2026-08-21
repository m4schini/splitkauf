// SPDX-License-Identifier: CC0-1.0

package problem_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/m4schini/splitkauf/config"
	"github.com/m4schini/splitkauf/ports/rest/problem"
)

func TestMain(m *testing.M) {
	// Write logs via telemetry.Logger, which reads config.C.
	if err := config.Load(); err != nil {
		panic(err)
	}

	m.Run()
}

func TestWriteSetsContentTypeAndStatus(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/nope", nil)

	problem.Write(rec, req, problem.New(problem.NotFound, "no resource"))

	if got := rec.Code; got != http.StatusNotFound {
		t.Errorf("status = %d, want %d", got, http.StatusNotFound)
	}

	if got := rec.Header().Get("Content-Type"); got != problem.ContentType {
		t.Errorf("Content-Type = %q, want %q", got, problem.ContentType)
	}

	var got problem.Problem
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decoding body: %v", err)
	}

	if got.Type != "/problems/not-found" {
		t.Errorf("type = %q, want %q", got.Type, "/problems/not-found")
	}

	if got.Title != http.StatusText(http.StatusNotFound) {
		t.Errorf("title = %q, want %q", got.Title, http.StatusText(http.StatusNotFound))
	}

	if got.Status != http.StatusNotFound {
		t.Errorf("status member = %d, want %d", got.Status, http.StatusNotFound)
	}

	if got.Detail != "no resource" {
		t.Errorf("detail = %q, want %q", got.Detail, "no resource")
	}
}

func TestWriteDefaultsInstanceToRequestPath(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/foo/bar", nil)

	problem.Write(rec, req, problem.New(problem.NotFound, "x"))

	var got problem.Problem
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decoding body: %v", err)
	}

	if got.Instance != "/api/v1/foo/bar" {
		t.Errorf("instance = %q, want %q", got.Instance, "/api/v1/foo/bar")
	}
}

func TestWriteOmitsEmptyMembers(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	// A problem with only a title set: all other members must be omitted.
	problem.Write(rec, req, problem.Problem{Title: "Teapot", Status: http.StatusTeapot})

	body := rec.Body.String()
	for _, member := range []string{`"detail"`, `"errors"`} {
		if strings.Contains(body, member) {
			t.Errorf("body %q unexpectedly contains %s", body, member)
		}
	}

	if !strings.Contains(body, `"instance":"/"`) {
		t.Errorf("body %q should contain the defaulted instance", body)
	}
}

func TestWriteDefaultsStatusTo500(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	problem.Write(rec, req, problem.Problem{Title: "boom"})

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
}

func TestFromStatus(t *testing.T) {
	cases := map[int]problem.Type{
		http.StatusBadRequest:            problem.Validation,
		http.StatusNotFound:              problem.NotFound,
		http.StatusMethodNotAllowed:      problem.MethodNotAllowed,
		http.StatusRequestEntityTooLarge: problem.PayloadTooLarge,
		http.StatusInternalServerError:   problem.Internal,
		http.StatusTeapot:                problem.Internal, // unknown → internal
		http.StatusBadGateway:            problem.Internal,
	}
	for status, want := range cases {
		if got := problem.FromStatus(status); got.Slug != want.Slug {
			t.Errorf("FromStatus(%d).Slug = %q, want %q", status, got.Slug, want.Slug)
		}
	}
}

func TestRegistrySlugsUniqueAndPopulated(t *testing.T) {
	seen := map[string]bool{}

	for _, ty := range problem.Types() {
		if ty.Slug == "" {
			t.Errorf("type %+v has empty slug", ty)
		}

		if ty.Title == "" {
			t.Errorf("type %q has empty title", ty.Slug)
		}

		if ty.Status == 0 {
			t.Errorf("type %q has zero status", ty.Slug)
		}

		if ty.Description == "" {
			t.Errorf("type %q has empty description", ty.Slug)
		}

		if seen[ty.Slug] {
			t.Errorf("duplicate slug %q", ty.Slug)
		}

		seen[ty.Slug] = true
		if ty.URI() != "/problems/"+ty.Slug {
			t.Errorf("URI() = %q, want %q", ty.URI(), "/problems/"+ty.Slug)
		}
	}
}
