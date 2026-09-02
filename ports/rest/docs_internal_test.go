// SPDX-License-Identifier: CC0-1.0

package rest

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

const (
	// docsHTMLContentType is the content type docsHandler always sets.
	docsHTMLContentType = "text/html; charset=utf-8"
	// docsRoute is the path chi mounts the handler on.
	docsRoute = "/docs"
)

// docsInternalSpec renders a minimal but valid OpenAPI document whose title is
// unique per test, so the Scalar page built from it is recognisable.
func docsInternalSpec(title string) []byte {
	return fmt.Appendf(nil, "openapi: 3.0.0\ninfo:\n  title: %s\n  version: \"1.0.0\"\npaths: {}\n", title)
}

// keepInternalOpenAPISpec restores the package-level OpenAPI spec after a test
// that overwrites it. The spec is process-wide state shared with every other
// test in this binary, so such tests must also stay sequential.
func keepInternalOpenAPISpec(t *testing.T) {
	t.Helper()

	t.Cleanup(func() {
		spec, err := os.ReadFile("../../openapi.yaml")
		if err != nil {
			t.Fatalf("reading openapi spec: %v", err)
		}

		SetOpenAPISpec(spec)
	})
}

// serveDocs runs one request through the handler and returns the recorder.
func serveDocs(t *testing.T, handler http.HandlerFunc, method, target string) *httptest.ResponseRecorder {
	t.Helper()

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), method, target, nil))

	return rec
}

// assertDocsPage checks the invariants every docs response shares — 200 with an
// HTML content type and an HTML body — and returns that body.
func assertDocsPage(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	if ct := rec.Header().Get("Content-Type"); ct != docsHTMLContentType {
		t.Errorf("Content-Type = %q, want %q", ct, docsHTMLContentType)
	}

	body := rec.Body.String()
	if !strings.HasPrefix(body, "<!DOCTYPE html>") {
		t.Errorf("body does not start with an HTML doctype: %.80q", body)
	}

	return body
}

// TestDocsHandlerRendersPerRequest covers what the registered spec does to
// docsHandler: rendering happens per request, reading the package-level spec
// fresh each time, and a spec Scalar cannot render turns into a logged 500
// rather than a panic.
//
//nolint:paralleltest // mutates the package-level OpenAPI spec; must not run beside other tests
func TestDocsHandlerRendersPerRequest(t *testing.T) {
	keepInternalOpenAPISpec(t)

	tests := []struct {
		name string
		spec []byte
		// wantErr, when true, expects a 500 instead of a rendered page.
		wantErr bool
		// wantInBody, when set, must appear in the rendered page — the spec
		// title is embedded in the Scalar HTML.
		wantInBody string
	}{
		{
			name:       "minimal spec",
			spec:       docsInternalSpec("Internal Docs Fixture"),
			wantErr:    false,
			wantInBody: "Internal Docs Fixture",
		},
		{
			// Empty but non-nil bytes still count as a configured spec, so
			// Scalar renders a titleless page instead of failing.
			name:       "empty spec bytes",
			spec:       []byte{},
			wantErr:    false,
			wantInBody: "",
		},
		{
			name:       "spec never set",
			spec:       nil,
			wantErr:    true,
			wantInBody: "",
		},
		{
			name:       "malformed spec",
			spec:       []byte("this: is: not: valid: yaml: [[["),
			wantErr:    true,
			wantInBody: "",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			SetOpenAPISpec(testCase.spec)

			handler := docsHandler()

			rec := serveDocs(t, handler, http.MethodGet, docsRoute)

			if testCase.wantErr {
				if rec.Code != http.StatusInternalServerError {
					t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
				}

				return
			}

			body := assertDocsPage(t, rec)

			if testCase.wantInBody != "" && !strings.Contains(body, testCase.wantInBody) {
				t.Errorf("body does not contain %q; got %.200q", testCase.wantInBody, body)
			}
		})
	}
}

// TestDocsHandlerIgnoresRequest pins that the returned closure inspects nothing
// on the request beyond the current spec: method and path filtering belongs to
// the chi route, so every request gets the same 200 and the same page.
func TestDocsHandlerIgnoresRequest(t *testing.T) {
	t.Parallel()

	handler := docsHandler()

	want := assertDocsPage(t, serveDocs(t, handler, http.MethodGet, docsRoute))
	if want == "" {
		t.Fatal("docs page is empty")
	}

	tests := []struct {
		name   string
		method string
		target string
	}{
		{name: "get docs", method: http.MethodGet, target: docsRoute},
		{name: "head docs", method: http.MethodHead, target: docsRoute},
		{name: "post docs", method: http.MethodPost, target: docsRoute},
		{name: "delete unrelated path", method: http.MethodDelete, target: "/not/the/docs/route"},
		{name: "get root", method: http.MethodGet, target: "/"},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := assertDocsPage(t, serveDocs(t, handler, testCase.method, testCase.target))
			if got != want {
				t.Errorf("body differs from the GET /docs page:\nwant %.120q\ngot = %.120q", want, got)
			}
		})
	}
}

// docsBrokenWriter fails every write, standing in for a client that hung up
// while the docs page was being written.
type docsBrokenWriter struct {
	header http.Header
	status int
	writes int
}

func (w *docsBrokenWriter) Header() http.Header {
	if w.header == nil {
		w.header = make(http.Header)
	}

	return w.header
}

func (w *docsBrokenWriter) WriteHeader(status int) { w.status = status }

func (w *docsBrokenWriter) Write(_ []byte) (int, error) {
	w.writes++

	return 0, io.ErrClosedPipe
}

// TestDocsHandlerSwallowsWriteError checks the documented failure handling of
// the closure: a write error is only logged, never propagated or panicked on,
// and the 200 response committed before the write stands.
func TestDocsHandlerSwallowsWriteError(t *testing.T) {
	t.Parallel()

	handler := docsHandler()

	writer := &docsBrokenWriter{header: nil, status: 0, writes: 0}

	handler.ServeHTTP(writer, httptest.NewRequestWithContext(t.Context(), http.MethodGet, docsRoute, nil))

	if writer.writes == 0 {
		t.Error("handler never attempted to write the docs page")
	}

	if writer.status != http.StatusOK {
		t.Errorf("status = %d, want %d", writer.status, http.StatusOK)
	}

	if ct := writer.Header().Get("Content-Type"); ct != docsHTMLContentType {
		t.Errorf("Content-Type = %q, want %q", ct, docsHTMLContentType)
	}
}
