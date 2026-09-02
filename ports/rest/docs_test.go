// SPDX-License-Identifier: CC0-1.0

package rest_test

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/m4schini/splitkauf/ports/rest"
)

// docsContentType is what the /docs handler always sends.
const docsContentType = "text/html; charset=utf-8"

// docsSpecFixture renders a minimal but valid OpenAPI document whose title is
// unique per test, so the Scalar page built from it can be told apart from a
// page built from any other spec.
func docsSpecFixture(title string) []byte {
	return fmt.Appendf(nil, "openapi: 3.0.0\ninfo:\n  title: %s\n  version: \"1.0.0\"\npaths: {}\n", title)
}

// projectOpenAPISpec re-reads the spec that TestMain registers, so tests that
// overwrite the package-level spec can put it back.
func projectOpenAPISpec(t *testing.T) []byte {
	t.Helper()

	spec, err := os.ReadFile("../../openapi.yaml")
	if err != nil {
		t.Fatalf("reading openapi spec: %v", err)
	}

	return spec
}

// keepOpenAPISpec restores the package-level OpenAPI spec once the test ends.
// That spec is global state, so a test swapping it out would otherwise poison
// the sibling tests that serve it; such tests must also stay sequential.
func keepOpenAPISpec(t *testing.T) {
	t.Helper()
	t.Cleanup(func() { rest.SetOpenAPISpec(projectOpenAPISpec(t)) })
}

// getDocs performs GET /docs against the api-docs handler.
func getDocs(t *testing.T, handler http.Handler) *httptest.ResponseRecorder {
	t.Helper()

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/docs", nil))

	return rec
}

// assertDocsPage checks the invariants of the docs response — always 200 with
// an HTML body, whatever the spec looked like — and returns that body.
func assertDocsPage(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	if ct := rec.Header().Get("Content-Type"); ct != docsContentType {
		t.Errorf("Content-Type = %q, want %q", ct, docsContentType)
	}

	body := rec.Body.String()
	if !strings.HasPrefix(body, "<!DOCTYPE html>") {
		t.Errorf("body does not start with an HTML doctype: %.80q", body)
	}

	return body
}

//nolint:paralleltest // mutates the package-level OpenAPI spec; must not run beside other tests
func TestDocsHandlerRendersPerRequest(t *testing.T) {
	keepOpenAPISpec(t)

	tests := []struct {
		name string
		spec []byte
		// wantErr, when true, expects a 500 instead of a rendered page.
		wantErr bool
		// wantInBody, when set, must appear in the rendered page — the spec
		// title ends up in the Scalar HTML.
		wantInBody string
	}{
		{
			name:       "project spec",
			spec:       projectOpenAPISpec(t),
			wantErr:    false,
			wantInBody: "Splitkauf API",
		},
		{
			name:       "minimal spec",
			spec:       docsSpecFixture("Minimal Docs Fixture"),
			wantErr:    false,
			wantInBody: "Minimal Docs Fixture",
		},
		{
			// Empty but non-nil bytes still count as configured spec bytes, so
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
			rest.SetOpenAPISpec(testCase.spec)

			handler := rest.APIDocsHandler()

			rec := getDocs(t, handler)

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

// TestDocsHandlerReadsSpecAtRequestTime pins that the returned closure reads
// the global spec per request, so a SetOpenAPISpec call made after the
// handler was built is still reflected — matching the /openapi.yaml handler.
//
//nolint:paralleltest // mutates the package-level OpenAPI spec; must not run beside other tests
func TestDocsHandlerReadsSpecAtRequestTime(t *testing.T) {
	keepOpenAPISpec(t)

	rest.SetOpenAPISpec(docsSpecFixture("Spec Before Rebuild"))

	handler := rest.APIDocsHandler()

	before := assertDocsPage(t, getDocs(t, handler))
	if !strings.Contains(before, "Spec Before Rebuild") {
		t.Fatalf("body does not contain %q; got %.200q", "Spec Before Rebuild", before)
	}

	rest.SetOpenAPISpec(docsSpecFixture("Spec After Rebuild"))

	after := assertDocsPage(t, getDocs(t, handler))
	if !strings.Contains(after, "Spec After Rebuild") {
		t.Errorf("already-built handler does not serve the spec registered after construction; got %.200q", after)
	}
}

// TestDocsHandlerServesIdenticalPagePerRequest checks that the handler holds no
// per-request state: every request replays the same captured HTML, concurrent
// ones included.
func TestDocsHandlerServesIdenticalPagePerRequest(t *testing.T) {
	t.Parallel()

	handler := rest.APIDocsHandler()
	want := assertDocsPage(t, getDocs(t, handler))

	const requests = 8

	bodies := make([]string, requests)

	var waiter sync.WaitGroup

	for index := range bodies {
		waiter.Go(func() {
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/docs", nil))
			bodies[index] = rec.Body.String()
		})
	}

	waiter.Wait()

	for index, got := range bodies {
		if got != want {
			t.Errorf("request %d body differs from the first response:\nwant %.120q\ngot = %.120q", index, want, got)
		}
	}
}

// failingResponseWriter fails every write, standing in for a client that
// disconnected while the docs page was being written.
type failingResponseWriter struct {
	header http.Header
	status int
	writes int
}

func (w *failingResponseWriter) Header() http.Header {
	if w.header == nil {
		w.header = make(http.Header)
	}

	return w.header
}

func (w *failingResponseWriter) WriteHeader(status int) { w.status = status }

func (w *failingResponseWriter) Write(_ []byte) (int, error) {
	w.writes++

	return 0, io.ErrClosedPipe
}

// TestDocsHandlerToleratesWriteFailure checks that a failed write is only
// logged: the handler must not panic, and the response that was already
// committed stays 200 with the HTML content type.
func TestDocsHandlerToleratesWriteFailure(t *testing.T) {
	t.Parallel()

	handler := rest.APIDocsHandler()
	writer := &failingResponseWriter{header: nil, status: 0, writes: 0}

	handler.ServeHTTP(writer, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/docs", nil))

	if writer.writes == 0 {
		t.Error("handler never attempted to write the docs page")
	}

	if writer.status != http.StatusOK {
		t.Errorf("status = %d, want %d", writer.status, http.StatusOK)
	}

	if ct := writer.Header().Get("Content-Type"); ct != docsContentType {
		t.Errorf("Content-Type = %q, want %q", ct, docsContentType)
	}
}
