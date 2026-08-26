// SPDX-License-Identifier: CC0-1.0

package middleware_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/m4schini/splitkauf/config"
	"github.com/m4schini/splitkauf/ports/rest/middleware"
)

// errRecoverBoom is a sentinel panic value used to assert that Recover
// re-panics with the exact value it recovered.
var errRecoverBoom = errors.New("boom: db password hunter2")

// errRecoverConnReset stands in for the transport error
// TestRecoverWrapsWriteError exercises below.
var errRecoverConnReset = errors.New("connection reset by peer")

// recoverPanicPayload is a non-error panic value used to assert that value
// identity is preserved across the re-panic path.
type recoverPanicPayload struct {
	Secret string
}

// recoverProblemBody mirrors the RFC 9457 members Recover's problem response
// is documented to carry.
type recoverProblemBody struct {
	Type     string `json:"type"`
	Title    string `json:"title"`
	Status   int    `json:"status"`
	Detail   string `json:"detail"`
	Instance string `json:"instance"`
}

// recoverFlushCounter is a ResponseWriter that implements http.Flusher and
// counts the forwarded calls.
type recoverFlushCounter struct {
	http.ResponseWriter

	flushes int
}

func (w *recoverFlushCounter) Flush() { w.flushes++ }

// recoverNoFlushWriter hides the http.Flusher implementation of the embedded
// recorder, so the wrapper has nothing to forward Flush to.
type recoverNoFlushWriter struct {
	rec *httptest.ResponseRecorder
}

func (w *recoverNoFlushWriter) Header() http.Header { return w.rec.Header() }

func (w *recoverNoFlushWriter) Write(b []byte) (int, error) { return w.rec.Write(b) }
func (w *recoverNoFlushWriter) WriteHeader(status int)      { w.rec.WriteHeader(status) }

// recoverFailingWriter fails every Write with a fixed error and byte count.
type recoverFailingWriter struct {
	http.ResponseWriter

	n   int
	err error
}

func (w *recoverFailingWriter) Write([]byte) (int, error) { return w.n, w.err }

// serveRecovered runs h against the given writer/request and returns whatever
// escaped as a panic (nil when the panic was swallowed).
//
//nolint:nonamedreturns // defer/recover requires a named return to capture the panic value
func serveRecovered(t *testing.T, h http.Handler, w http.ResponseWriter, r *http.Request) (escaped any) {
	t.Helper()

	defer func() { escaped = recover() }()

	h.ServeHTTP(w, r)

	return nil
}

// assertRecoverProblem verifies the generic RFC 9457 Internal problem response
// and that nothing from forbidden leaked into headers or body.
func assertRecoverProblem(t *testing.T, rec *httptest.ResponseRecorder, wantInstance, forbidden string) {
	t.Helper()

	res := rec.Result()
	defer func() { _ = res.Body.Close() }()

	if res.StatusCode != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", res.StatusCode, http.StatusInternalServerError)
	}

	if ct := res.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/problem+json") {
		t.Errorf("Content-Type = %q, want application/problem+json", ct)
	}

	raw := rec.Body.String()

	var body recoverProblemBody
	if err := json.Unmarshal([]byte(raw), &body); err != nil {
		t.Fatalf("unmarshal problem body %q: %v", raw, err)
	}

	if body.Type == "" {
		t.Error("problem type is empty, want a type URI")
	}

	if body.Title != "Internal Server Error" {
		t.Errorf("problem title = %q, want %q", body.Title, "Internal Server Error")
	}

	if body.Status != http.StatusInternalServerError {
		t.Errorf("problem status = %d, want %d", body.Status, http.StatusInternalServerError)
	}

	if body.Detail == "" {
		t.Error("problem detail is empty, want the generic Internal description")
	}

	if body.Instance != wantInstance {
		t.Errorf("problem instance = %q, want %q", body.Instance, wantInstance)
	}

	// RFC 9457 §5: panic details must never reach the client.
	if forbidden != "" {
		if strings.Contains(raw, forbidden) {
			t.Errorf("problem body leaked panic detail %q: %s", forbidden, raw)
		}

		for name, values := range res.Header {
			for _, v := range values {
				if strings.Contains(v, forbidden) {
					t.Errorf("header %s leaked panic detail %q: %s", name, forbidden, v)
				}
			}
		}
	}
}

func TestRecover(t *testing.T) {
	t.Parallel()

	const (
		path    = "/widgets/42"
		target  = path + "?token=abc"
		leakStr = "boom: db password hunter2"
	)

	payload := &recoverPanicPayload{Secret: leakStr}

	tests := []struct {
		name string
		// handler is the wrapped handler under test.
		handler http.HandlerFunc
		// wantPanic is the value Recover must re-panic with; nil means the
		// panic (if any) must be swallowed.
		wantPanic any
		// wantProblem asserts the generic 500 problem response.
		wantProblem bool
		// wantStatus/wantBody are only checked when wantProblem is false.
		wantStatus int
		wantBody   string
		// forbidden must not appear in the response when wantProblem is set.
		forbidden string
	}{
		{
			name: "no panic passes through unchanged",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("X-Handler", "ran")
				w.WriteHeader(http.StatusAccepted)
				_, _ = w.Write([]byte("handler body"))
			},
			wantStatus: http.StatusAccepted,
			wantBody:   "handler body",
		},
		{
			name: "panic before any write yields generic problem response",
			handler: func(_ http.ResponseWriter, _ *http.Request) {
				panic(errRecoverBoom)
			},
			wantProblem: true,
			forbidden:   leakStr,
		},
		{
			name: "string panic before any write does not leak the value",
			handler: func(_ http.ResponseWriter, _ *http.Request) {
				panic(leakStr)
			},
			wantProblem: true,
			forbidden:   leakStr,
		},
		{
			name: "nil panic is still handled (runtime.PanicNilError)",
			handler: func(_ http.ResponseWriter, _ *http.Request) {
				var v any
				panic(v)
			},
			wantProblem: true,
		},
		{
			name: "ErrAbortHandler is converted to a problem response",
			handler: func(_ http.ResponseWriter, _ *http.Request) {
				panic(http.ErrAbortHandler)
			},
			wantProblem: true,
		},
		{
			name: "panic after Write re-panics with the same value",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte("partial"))

				panic(errRecoverBoom)
			},
			wantPanic:  errRecoverBoom,
			wantStatus: http.StatusOK,
			wantBody:   "partial",
		},
		{
			name: "panic after WriteHeader only re-panics",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusServiceUnavailable)
				panic(errRecoverBoom)
			},
			wantPanic:  errRecoverBoom,
			wantStatus: http.StatusServiceUnavailable,
			wantBody:   "",
		},
		{
			name: "non-error panic value is re-panicked as-is",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte("partial"))

				panic(payload)
			},
			wantPanic:  payload,
			wantStatus: http.StatusOK,
			wantBody:   "partial",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rec := httptest.NewRecorder()
			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, target, nil)

			escaped := serveRecovered(t, middleware.Recover(tt.handler), rec, req)

			switch {
			case tt.wantPanic != nil:
				if escaped == nil {
					t.Fatalf("Recover swallowed the panic, want re-panic with %v", tt.wantPanic)
				}

				if escaped != tt.wantPanic {
					t.Errorf("re-panicked value = %#v, want %#v", escaped, tt.wantPanic)
				}
			default:
				if escaped != nil {
					t.Fatalf("unexpected panic escaped Recover: %#v", escaped)
				}
			}

			if tt.wantProblem {
				assertRecoverProblem(t, rec, path, tt.forbidden)

				return
			}

			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}

			if got := rec.Body.String(); got != tt.wantBody {
				t.Errorf("body = %q, want %q (no problem body may be appended)", got, tt.wantBody)
			}

			if !tt.wantProblem && rec.Header().Get("X-Handler") != "" && rec.Header().Get("X-Handler") != "ran" {
				t.Errorf("handler header mangled: %q", rec.Header().Get("X-Handler"))
			}
		})
	}
}

func TestRecoverForwardsFlush(t *testing.T) {
	t.Parallel()

	underlying := &recoverFlushCounter{ResponseWriter: httptest.NewRecorder()}

	var sawFlusher bool

	h := middleware.Recover(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		f, ok := w.(http.Flusher)
		sawFlusher = ok

		if ok {
			f.Flush()
			f.Flush()
		}
	}))

	h.ServeHTTP(underlying, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/flush", nil))

	if !sawFlusher {
		t.Fatal("wrapped writer does not implement http.Flusher")
	}

	if underlying.flushes != 2 {
		t.Errorf("forwarded flushes = %d, want 2", underlying.flushes)
	}
}

func TestRecoverFlushWithoutUnderlyingFlusher(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	underlying := &recoverNoFlushWriter{rec: rec}

	h := middleware.Recover(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if f, ok := w.(http.Flusher); ok {
			f.Flush() // must be a no-op, not a panic
		}

		_, _ = w.Write([]byte("ok"))
	}))

	escaped := serveRecovered(t, h, underlying, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/flush", nil))
	if escaped != nil {
		t.Fatalf("Flush on a non-Flusher writer panicked: %#v", escaped)
	}

	if got := rec.Body.String(); got != "ok" {
		t.Errorf("body = %q, want %q", got, "ok")
	}
}

// TestRecoverFlushDoesNotStartResponse pins the current behaviour: Flush does
// not set the wrote flag, so a handler that flushed and then panicked still
// takes the write-a-problem-body path instead of re-panicking.
func TestRecoverFlushDoesNotStartResponse(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	underlying := &recoverFlushCounter{ResponseWriter: rec}

	h := middleware.Recover(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("recoverWriter does not forward http.Flusher")
		}

		flusher.Flush()
		panic(errRecoverBoom)
	}))

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/flushed", nil)
	escaped := serveRecovered(t, h, underlying, req)

	if escaped != nil {
		t.Fatalf("panic escaped after a flush-only response: %#v", escaped)
	}

	assertRecoverProblem(t, rec, "/flushed", errRecoverBoom.Error())
}

func TestRecoverWrapsWriteError(t *testing.T) {
	t.Parallel()

	wantErr := errRecoverConnReset
	underlying := &recoverFailingWriter{
		ResponseWriter: httptest.NewRecorder(),
		n:              3,
		err:            wantErr,
	}

	var (
		gotN   int
		gotErr error
	)

	h := middleware.Recover(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		gotN, gotErr = w.Write([]byte("hello"))
	}))

	h.ServeHTTP(underlying, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/write", nil))

	if gotN != 3 {
		t.Errorf("byte count = %d, want 3 (must be passed through)", gotN)
	}

	if !errors.Is(gotErr, wantErr) {
		t.Fatalf("error = %v, want it to wrap %v", gotErr, wantErr)
	}

	if !strings.Contains(gotErr.Error(), "writing response: ") {
		t.Errorf("error = %q, want it prefixed with %q", gotErr.Error(), "writing response: ")
	}
}

// recConfigOnce guards the config load Recover needs: the middleware logs via
// telemetry.Logger, which reads config.C (nil until config.Load runs).
//
//nolint:gochecknoglobals // test-wide one-time setup
var recConfigOnce sync.Once

func recEnsureConfig(t *testing.T) {
	t.Helper()

	recConfigOnce.Do(func() {
		if err := config.Load(); err != nil {
			t.Fatalf("load config: %v", err)
		}
	})
}

// errRecPanic is a sentinel panic value; its message doubles as a secret that
// must never reach the client (RFC 9457 §5).
var errRecPanic = errors.New("panic detail: dsn=postgres://user:hunter2@db/app")

// recPanicValue is a non-error panic value used to prove that Recover handles
// arbitrary recovered types and preserves their identity when re-panicking.
type recPanicValue struct {
	Secret string
}

// recProblemDoc mirrors the RFC 9457 members of the Internal problem response.
type recProblemDoc struct {
	Type   string `json:"type"`
	Title  string `json:"title"`
	Status int    `json:"status"`
	Detail string `json:"detail"`
}

// recPlainWriter is a bare ResponseWriter: it deliberately does not implement
// http.Flusher, so Flush on the wrapper has nothing to forward to.
type recPlainWriter struct {
	header http.Header
	code   int
	body   strings.Builder
}

func newRecPlainWriter() *recPlainWriter {
	return &recPlainWriter{header: make(http.Header)}
}

func (w *recPlainWriter) Header() http.Header { return w.header }

func (w *recPlainWriter) WriteHeader(code int) {
	if w.code == 0 {
		w.code = code
	}
}

func (w *recPlainWriter) Write(b []byte) (int, error) {
	if w.code == 0 {
		w.code = http.StatusOK
	}

	return w.body.Write(b)
}

// recFlusher adds http.Flusher to an httptest.ResponseRecorder-backed writer
// and records how many flushes were forwarded.
type recFlusher struct {
	http.ResponseWriter

	rec     *httptest.ResponseRecorder
	flushes int
}

func (w *recFlusher) Flush() {
	w.flushes++
	w.rec.Flush()
}

// serveRecPanic serves the request and reports the panic value that escaped
// the middleware, if any.
//
//nolint:nonamedreturns // defer/recover requires named returns to capture the panic value
func serveRecPanic(t *testing.T, h http.Handler, w http.ResponseWriter, r *http.Request) (escaped any, panicked bool) {
	t.Helper()

	defer func() {
		if v := recover(); v != nil {
			escaped, panicked = v, true
		}
	}()

	h.ServeHTTP(w, r)

	return nil, false
}

// assertRecProblem checks the generic RFC 9457 Internal problem response and
// that none of the forbidden strings leaked into the body or the headers.
func assertRecProblem(t *testing.T, rec *httptest.ResponseRecorder, forbidden ...string) {
	t.Helper()

	res := rec.Result()
	defer func() { _ = res.Body.Close() }()

	if res.StatusCode != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", res.StatusCode, http.StatusInternalServerError)
	}

	if ct := res.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/problem+json") {
		t.Errorf("Content-Type = %q, want application/problem+json", ct)
	}

	raw := rec.Body.String()

	var doc recProblemDoc
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		t.Fatalf("problem body %q is not JSON: %v", raw, err)
	}

	if doc.Status != http.StatusInternalServerError {
		t.Errorf("problem status member = %d, want %d", doc.Status, http.StatusInternalServerError)
	}

	if doc.Title == "" {
		t.Error("problem title is empty, want the generic Internal title")
	}

	if doc.Detail == "" {
		t.Error("problem detail is empty, want the generic Internal description")
	}

	for _, secret := range forbidden {
		if secret == "" {
			continue
		}

		if strings.Contains(raw, secret) {
			t.Errorf("problem body leaked panic detail %q: %s", secret, raw)
		}

		for name, values := range res.Header {
			for _, v := range values {
				if strings.Contains(v, secret) {
					t.Errorf("header %s leaked panic detail %q: %s", name, secret, v)
				}
			}
		}
	}
}

// TestRecoverPanicBeforeResponseStarted covers the "nothing written yet" path:
// whatever the handler panicked with, the client sees the generic 500 problem
// response and no panic detail.
func TestRecoverPanicBeforeResponseStarted(t *testing.T) {
	t.Parallel()

	recEnsureConfig(t)

	tests := []struct {
		name       string
		panicValue any
		// forbidden must not appear anywhere in the response.
		forbidden []string
	}{
		{
			name:       "error value",
			panicValue: errRecPanic,
			forbidden:  []string{errRecPanic.Error(), "hunter2"},
		},
		{
			name:       "string value",
			panicValue: "raw-string-panic-9f2c",
			forbidden:  []string{"raw-string-panic-9f2c"},
		},
		{
			name:       "int value",
			panicValue: 1234567,
			forbidden:  []string{"1234567"},
		},
		{
			name:       "custom struct value",
			panicValue: recPanicValue{Secret: "struct-secret-4b71"},
			forbidden:  []string{"struct-secret-4b71"},
		},
		{
			name:       "runtime error from a nil map write",
			panicValue: nil, // handled below by triggering a real runtime panic
			forbidden:  []string{"nil map"},
		},
		{
			name:       "abort sentinel is not special-cased",
			panicValue: http.ErrAbortHandler,
			forbidden:  []string{http.ErrAbortHandler.Error()},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			handler := http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
				if tt.panicValue == nil {
					var m map[string]string

					//nolint:staticcheck // deliberate: a real runtime panic, not a Go-level panic()
					m["boom"] = "nil map"

					return
				}

				panic(tt.panicValue)
			})

			rec := httptest.NewRecorder()
			req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/orders/7?secret=abc", nil)

			escaped, panicked := serveRecPanic(t, middleware.Recover(handler), rec, req)
			if panicked {
				t.Fatalf("panic escaped Recover: %#v", escaped)
			}

			assertRecProblem(t, rec, tt.forbidden...)
		})
	}
}

// TestRecoverPanicNilValue pins that panic(nil) is a real panic under Go 1.21+
// (recover returns *runtime.PanicNilError), so it takes the problem path
// instead of being mistaken for "no panic".
func TestRecoverPanicNilValue(t *testing.T) {
	t.Parallel()

	recEnsureConfig(t)

	handler := http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		var v any
		panic(v)
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/nil-panic", nil)

	escaped, panicked := serveRecPanic(t, middleware.Recover(handler), rec, req)
	if panicked {
		t.Fatalf("panic(nil) escaped Recover: %#v", escaped)
	}

	assertRecProblem(t, rec)
}

// TestRecoverRepanicsAfterResponseStarted covers the "response already
// started" path: Recover must re-panic with the original value rather than
// append a problem body to a partial response.
func TestRecoverRepanicsAfterResponseStarted(t *testing.T) {
	t.Parallel()

	recEnsureConfig(t)

	structValue := &recPanicValue{Secret: "repanic-identity"}

	tests := []struct {
		name       string
		start      func(w http.ResponseWriter)
		panicValue any
		wantStatus int
		wantBody   string
	}{
		{
			name:       "explicit WriteHeader then panic",
			start:      func(w http.ResponseWriter) { w.WriteHeader(http.StatusOK) },
			panicValue: errRecPanic,
			wantStatus: http.StatusOK,
			wantBody:   "",
		},
		{
			name: "implicit header via Write then panic",
			start: func(w http.ResponseWriter) {
				_, _ = w.Write([]byte("partial payload"))
			},
			panicValue: errRecPanic,
			wantStatus: http.StatusOK,
			wantBody:   "partial payload",
		},
		{
			name: "streamed chunks then panic with a non-error value",
			start: func(w http.ResponseWriter) {
				w.WriteHeader(http.StatusPartialContent)
				_, _ = w.Write([]byte("chunk-1"))
			},
			panicValue: structValue,
			wantStatus: http.StatusPartialContent,
			wantBody:   "chunk-1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				tt.start(w)
				panic(tt.panicValue)
			})

			rec := httptest.NewRecorder()
			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/stream", nil)

			escaped, panicked := serveRecPanic(t, middleware.Recover(handler), rec, req)
			if !panicked {
				t.Fatalf("Recover swallowed the panic, want a re-panic with %#v", tt.panicValue)
			}

			if escaped != tt.panicValue {
				t.Errorf("re-panicked value = %#v, want the original %#v", escaped, tt.panicValue)
			}

			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d (handler's status must survive)", rec.Code, tt.wantStatus)
			}

			if got := rec.Body.String(); got != tt.wantBody {
				t.Errorf("body = %q, want %q (no problem body may be appended)", got, tt.wantBody)
			}

			if ct := rec.Header().Get("Content-Type"); strings.HasPrefix(ct, "application/problem+json") {
				t.Errorf("Content-Type = %q, want no problem response on a started response", ct)
			}
		})
	}
}

// TestRecoverPassesThroughCleanResponses asserts the middleware is transparent
// when the handler returns normally.
func TestRecoverPassesThroughCleanResponses(t *testing.T) {
	t.Parallel()

	recEnsureConfig(t)

	tests := []struct {
		name       string
		handler    http.HandlerFunc
		wantStatus int
		wantBody   string
		wantHeader map[string]string
	}{
		{
			name: "implicit 200 with body",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(`{"ok":true}`))
			},
			wantStatus: http.StatusOK,
			wantBody:   `{"ok":true}`,
		},
		{
			name: "explicit status and custom headers",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "text/plain; charset=utf-8")
				w.Header().Set("X-Request-Id", "req-42")
				w.WriteHeader(http.StatusCreated)
				_, _ = w.Write([]byte("created"))
			},
			wantStatus: http.StatusCreated,
			wantBody:   "created",
			wantHeader: map[string]string{
				"Content-Type": "text/plain; charset=utf-8",
				"X-Request-Id": "req-42",
			},
		},
		{
			name: "status only, no body",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNoContent)
			},
			wantStatus: http.StatusNoContent,
			wantBody:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rec := httptest.NewRecorder()
			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/clean", nil)

			escaped, panicked := serveRecPanic(t, middleware.Recover(tt.handler), rec, req)
			if panicked {
				t.Fatalf("unexpected panic from a clean handler: %#v", escaped)
			}

			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}

			if got := rec.Body.String(); got != tt.wantBody {
				t.Errorf("body = %q, want %q", got, tt.wantBody)
			}

			for name, want := range tt.wantHeader {
				if got := rec.Header().Get(name); got != want {
					t.Errorf("header %s = %q, want %q", name, got, want)
				}
			}
		})
	}
}

// TestRecoverWriteReturnsUnderlyingCount asserts the wrapper does not alter the
// byte count reported to a successfully writing handler.
func TestRecoverWriteReturnsUnderlyingCount(t *testing.T) {
	t.Parallel()

	recEnsureConfig(t)

	payload := []byte("twelve bytes")

	var (
		gotN   int
		gotErr error
	)

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		gotN, gotErr = w.Write(payload)
	})

	rec := httptest.NewRecorder()
	middleware.Recover(handler).ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/write", nil))

	if gotErr != nil {
		t.Fatalf("Write error = %v, want nil", gotErr)
	}

	if gotN != len(payload) {
		t.Errorf("Write n = %d, want %d", gotN, len(payload))
	}
}

// TestRecoverFlushOnPlainWriter asserts the wrapper always satisfies
// http.Flusher and that flushing a writer without an underlying Flusher is a
// no-op rather than a type-assertion panic.
func TestRecoverFlushOnPlainWriter(t *testing.T) {
	t.Parallel()

	recEnsureConfig(t)

	plain := newRecPlainWriter()

	var isFlusher bool

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		var f http.Flusher

		f, isFlusher = w.(http.Flusher)
		if isFlusher {
			f.Flush()
		}

		_, _ = w.Write([]byte("done"))
	})

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/plain", nil)
	escaped, panicked := serveRecPanic(t, middleware.Recover(handler), plain, req)

	if panicked {
		t.Fatalf("Flush on a non-Flusher writer panicked: %#v", escaped)
	}

	if !isFlusher {
		t.Fatal("wrapped writer does not implement http.Flusher, want the wrapper to expose Flush")
	}

	if got := plain.body.String(); got != "done" {
		t.Errorf("body = %q, want %q", got, "done")
	}
}

// TestRecoverFlushThenPanicGap pins the current behaviour of the flush path:
// Flush is forwarded but does not mark the response as started, so a handler
// that flushed (starting the response with an implicit 200) and then panicked
// gets a problem body appended instead of the re-panic that a started
// response should trigger.
func TestRecoverFlushThenPanicGap(t *testing.T) {
	t.Parallel()

	recEnsureConfig(t)

	rec := httptest.NewRecorder()
	flusher := &recFlusher{ResponseWriter: rec, rec: rec}

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("recoverWriter does not forward http.Flusher")
		}

		flusher.Flush()
		panic(errRecPanic)
	})

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/flushed", nil)
	escaped, panicked := serveRecPanic(t, middleware.Recover(handler), flusher, req)

	if panicked {
		t.Fatalf("panic escaped after a flush-only response: %#v", escaped)
	}

	if flusher.flushes != 1 {
		t.Errorf("forwarded flushes = %d, want 1", flusher.flushes)
	}

	// The flush already committed the implicit 200, so the problem body lands
	// on top of an already-started response.
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d (the flush committed the header first)", rec.Code, http.StatusOK)
	}

	var doc recProblemDoc
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatalf("problem body %q is not JSON: %v", rec.Body.String(), err)
	}

	if doc.Status != http.StatusInternalServerError {
		t.Errorf("problem status member = %d, want %d", doc.Status, http.StatusInternalServerError)
	}

	if strings.Contains(rec.Body.String(), "hunter2") {
		t.Errorf("problem body leaked panic detail: %s", rec.Body.String())
	}
}
