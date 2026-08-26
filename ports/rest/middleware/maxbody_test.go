// SPDX-License-Identifier: CC0-1.0

package middleware_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/m4schini/splitkauf/ports/rest/middleware"
)

// maxBodyTrackingBody is an io.ReadCloser that records how often it was read
// or closed, so tests can assert that the reject path never touches the
// original request body.
type maxBodyTrackingBody struct {
	reader *bytes.Reader
	reads  int
	closes int
}

func newMaxBodyTrackingBody(payload []byte) *maxBodyTrackingBody {
	return &maxBodyTrackingBody{reader: bytes.NewReader(payload)}
}

func (b *maxBodyTrackingBody) Read(p []byte) (int, error) {
	b.reads++

	return b.reader.Read(p)
}

func (b *maxBodyTrackingBody) Close() error {
	b.closes++

	return nil
}

// maxBodyProblem mirrors the RFC 9457 members the middleware is expected to
// emit on the reject path.
type maxBodyProblem struct {
	Type     string `json:"type"`
	Title    string `json:"title"`
	Status   int    `json:"status"`
	Detail   string `json:"detail"`
	Instance string `json:"instance"`
}

// newMaxBodyRequest builds a request with an explicitly controlled
// ContentLength, which is the only input the up-front check looks at.
func newMaxBodyRequest(t *testing.T, method, target string, body io.ReadCloser, contentLength int64) *http.Request {
	t.Helper()

	req := httptest.NewRequestWithContext(t.Context(), method, target, body)
	req.ContentLength = contentLength

	return req
}

func TestMaxBodyContentLengthGate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		limit         int64
		contentLength int64
		wantNext      bool
		wantStatus    int
	}{
		{
			name:          "well under limit passes through",
			limit:         1024,
			contentLength: 10,
			wantNext:      true,
			wantStatus:    http.StatusOK,
		},
		{
			name:          "exactly at limit passes through",
			limit:         1024,
			contentLength: 1024,
			wantNext:      true,
			wantStatus:    http.StatusOK,
		},
		{
			name:          "one byte over limit is rejected",
			limit:         1024,
			contentLength: 1025,
			wantNext:      false,
			wantStatus:    http.StatusRequestEntityTooLarge,
		},
		{
			name:          "unknown length bypasses the up-front check",
			limit:         1024,
			contentLength: -1,
			wantNext:      true,
			wantStatus:    http.StatusOK,
		},
		{
			name:          "empty body passes through",
			limit:         1024,
			contentLength: 0,
			wantNext:      true,
			wantStatus:    http.StatusOK,
		},
		{
			name:          "zero limit with empty body passes through",
			limit:         0,
			contentLength: 0,
			wantNext:      true,
			wantStatus:    http.StatusOK,
		},
		{
			name:          "zero limit with one declared byte is rejected",
			limit:         0,
			contentLength: 1,
			wantNext:      false,
			wantStatus:    http.StatusRequestEntityTooLarge,
		},
		{
			name:          "negative limit rejects any declared body",
			limit:         -5,
			contentLength: 1,
			wantNext:      false,
			wantStatus:    http.StatusRequestEntityTooLarge,
		},
		{
			// Pathological but documents that limit is unvalidated:
			// -1 > -5, so even a chunked request trips the gate.
			name:          "limit below minus one rejects unknown length",
			limit:         -5,
			contentLength: -1,
			wantNext:      false,
			wantStatus:    http.StatusRequestEntityTooLarge,
		},
		{
			// -1 > -1 is false, so the gate still lets this through.
			name:          "limit minus one passes unknown length",
			limit:         -1,
			contentLength: -1,
			wantNext:      true,
			wantStatus:    http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var nextCalled bool

			next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				nextCalled = true

				w.WriteHeader(http.StatusOK)
			})

			mw := middleware.MaxBody(tt.limit)
			if mw == nil {
				t.Fatal("MaxBody returned a nil middleware")
			}

			handler := mw(next)
			if handler == nil {
				t.Fatal("MaxBody middleware returned a nil handler")
			}

			req := newMaxBodyRequest(t, http.MethodPost, "/groups", newMaxBodyTrackingBody([]byte("payload")), tt.contentLength)
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if nextCalled != tt.wantNext {
				t.Errorf("next called = %t, want %t", nextCalled, tt.wantNext)
			}

			if got := rec.Code; got != tt.wantStatus {
				t.Errorf("status = %d, want %d", got, tt.wantStatus)
			}
		})
	}
}

func TestMaxBodyRejectWritesProblemResponse(t *testing.T) {
	t.Parallel()

	next := http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("next handler must not be called when Content-Length exceeds the limit")
	})

	body := newMaxBodyTrackingBody([]byte("this payload is too large"))
	req := newMaxBodyRequest(t, http.MethodPost, "/groups/42/expenses", body, 1025)
	rec := httptest.NewRecorder()

	middleware.MaxBody(1024)(next).ServeHTTP(rec, req)

	if got := rec.Code; got != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want %d", got, http.StatusRequestEntityTooLarge)
	}

	const wantContentType = "application/problem+json"

	gotContentType := rec.Header().Get("Content-Type")
	if gotContentType != wantContentType && !strings.HasPrefix(gotContentType, wantContentType+";") {
		t.Errorf("Content-Type = %q, want %q (optionally with parameters)", gotContentType, wantContentType)
	}

	var got maxBodyProblem
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode problem body: %v (body = %q)", err, rec.Body.String())
	}

	if !strings.Contains(got.Type, "payload-too-large") {
		t.Errorf("problem type = %q, want it to identify payload-too-large", got.Type)
	}

	if want := "Request Entity Too Large"; got.Title != want {
		t.Errorf("problem title = %q, want %q", got.Title, want)
	}

	if got.Status != http.StatusRequestEntityTooLarge {
		t.Errorf("problem status = %d, want %d", got.Status, http.StatusRequestEntityTooLarge)
	}

	if want := "request body exceeds the maximum allowed size"; got.Detail != want {
		t.Errorf("problem detail = %q, want %q", got.Detail, want)
	}

	if want := "/groups/42/expenses"; got.Instance != want {
		t.Errorf("problem instance = %q, want the request path %q", got.Instance, want)
	}

	// The body is neither read nor closed on the reject path.
	if body.reads != 0 {
		t.Errorf("original body Read calls = %d, want 0", body.reads)
	}

	if body.closes != 0 {
		t.Errorf("original body Close calls = %d, want 0", body.closes)
	}
}

func TestMaxBodyPassThroughWrapsBody(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		limit         int64
		payload       []byte
		contentLength int64
		wantTooLarge  bool
	}{
		{
			name:          "unknown length under limit reads fine",
			limit:         8,
			payload:       []byte("12345678"),
			contentLength: -1,
			wantTooLarge:  false,
		},
		{
			name:          "unknown length over limit trips the backstop",
			limit:         8,
			payload:       []byte("123456789"),
			contentLength: -1,
			wantTooLarge:  true,
		},
		{
			name:          "declared length under limit reads fine",
			limit:         16,
			payload:       []byte("hello"),
			contentLength: 5,
			wantTooLarge:  false,
		},
		{
			name:          "zero limit trips the backstop on undeclared bytes",
			limit:         0,
			payload:       []byte("x"),
			contentLength: -1,
			wantTooLarge:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			original := newMaxBodyTrackingBody(tt.payload)

			var (
				nextCalled bool
				wasWrapped bool
				readBytes  []byte
				readErr    error
			)

			next := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				nextCalled = true
				wasWrapped = r.Body != io.ReadCloser(original)
				readBytes, readErr = io.ReadAll(r.Body)
			})

			req := newMaxBodyRequest(t, http.MethodPost, "/groups", original, tt.contentLength)
			rec := httptest.NewRecorder()

			middleware.MaxBody(tt.limit)(next).ServeHTTP(rec, req)

			if !nextCalled {
				t.Fatal("next handler was not called on the pass-through path")
			}

			if !wasWrapped {
				t.Error("r.Body seen by next is the original reader, want it wrapped in a MaxBytesReader")
			}

			var maxBytesErr *http.MaxBytesError
			if tt.wantTooLarge {
				if !errors.As(readErr, &maxBytesErr) {
					t.Fatalf("read error = %v, want *http.MaxBytesError", readErr)
				}

				if maxBytesErr.Limit != tt.limit {
					t.Errorf("MaxBytesError.Limit = %d, want %d", maxBytesErr.Limit, tt.limit)
				}

				return
			}

			if readErr != nil {
				t.Fatalf("read body: unexpected error %v", readErr)
			}

			if !bytes.Equal(readBytes, tt.payload) {
				t.Errorf("read body = %q, want %q", readBytes, tt.payload)
			}
		})
	}
}

func TestMaxBodyPassThroughMutatesRequestInPlaceAndWritesNothing(t *testing.T) {
	t.Parallel()

	original := newMaxBodyTrackingBody([]byte("ok"))
	req := newMaxBodyRequest(t, http.MethodPost, "/groups", original, 2)

	var seen *http.Request

	next := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		seen = r
	})

	rec := httptest.NewRecorder()

	middleware.MaxBody(1024)(next).ServeHTTP(rec, req)

	if seen != req {
		t.Errorf("next received request %p, want the same pointer %p", seen, req)
	}

	if req.Body == io.ReadCloser(original) {
		t.Error("req.Body was not replaced by the MaxBytesReader backstop")
	}

	// The middleware writes nothing itself on the pass-through path; the
	// recorder only carries the (unwritten) default status.
	if rec.Body.Len() != 0 {
		t.Errorf("response body = %q, want empty", rec.Body.String())
	}

	if got := rec.Header().Get("Content-Type"); got != "" {
		t.Errorf("Content-Type = %q, want it unset", got)
	}
}

func TestMaxBodyMiddlewareIsReusableAcrossRequests(t *testing.T) {
	t.Parallel()

	var calls int

	handler := middleware.MaxBody(8)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++

		w.WriteHeader(http.StatusOK)
	}))

	requests := []struct {
		contentLength int64
		wantStatus    int
	}{
		{contentLength: 8, wantStatus: http.StatusOK},
		{contentLength: 9, wantStatus: http.StatusRequestEntityTooLarge},
		{contentLength: 0, wantStatus: http.StatusOK},
	}

	for _, r := range requests {
		rec := httptest.NewRecorder()
		req := newMaxBodyRequest(t, http.MethodPost, "/groups", newMaxBodyTrackingBody([]byte("12345678")), r.contentLength)

		handler.ServeHTTP(rec, req)

		if rec.Code != r.wantStatus {
			t.Errorf("Content-Length %d: status = %d, want %d", r.contentLength, rec.Code, r.wantStatus)
		}
	}

	if want := 2; calls != want {
		t.Errorf("next invocations = %d, want %d", calls, want)
	}
}

func TestMaxBodyBodylessRequestsFlowThrough(t *testing.T) {
	t.Parallel()

	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			t.Parallel()

			var nextCalled bool

			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				nextCalled = true

				got, err := io.ReadAll(r.Body)
				if err != nil {
					t.Errorf("read body: unexpected error %v", err)
				}

				if len(got) != 0 {
					t.Errorf("read body = %q, want empty", got)
				}

				w.WriteHeader(http.StatusNoContent)
			})

			req := newMaxBodyRequest(t, method, "/groups", http.NoBody, 0)
			rec := httptest.NewRecorder()

			middleware.MaxBody(1024)(next).ServeHTTP(rec, req)

			if !nextCalled {
				t.Error("next handler was not called for a bodyless request")
			}

			if got := rec.Code; got != http.StatusNoContent {
				t.Errorf("status = %d, want %d", got, http.StatusNoContent)
			}
		})
	}
}
