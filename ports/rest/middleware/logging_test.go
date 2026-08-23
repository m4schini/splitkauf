// SPDX-License-Identifier: CC0-1.0

package middleware_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

	"github.com/m4schini/splitkauf/config"
	"github.com/m4schini/splitkauf/ports/rest/middleware"
	"github.com/m4schini/splitkauf/telemetry"
)

// logMessage is the single message Logging emits per handled request.
const logMessage = "request handled"

// newLoggingObserver points the zap globals at an in-memory core and returns
// the recorded logs.
//
// telemetry.Logger is invoked first on purpose: it builds the real logger
// behind a process-wide sync.Once and installs it with zap.ReplaceGlobals.
// Forcing that initialisation *before* the observer is installed keeps it from
// clobbering the observer on a later call. The initialisation dereferences
// config.C, so a minimal config is installed when the process has not loaded
// one.
//
// Tests using this helper mutate process globals and therefore must not call
// t.Parallel().
func newLoggingObserver(t *testing.T) *observer.ObservedLogs {
	t.Helper()

	if config.C == nil {
		config.C = &config.Config{App: config.AppConfig{LogLevel: "info"}}
	}

	_ = telemetry.Logger("api")

	prev := zap.L()
	core, logs := observer.New(zapcore.DebugLevel)
	zap.ReplaceGlobals(zap.New(core))
	t.Cleanup(func() { zap.ReplaceGlobals(prev) })

	return logs
}

// singleLogEntry asserts exactly one entry was recorded and returns it with its
// structured fields.
func singleLogEntry(t *testing.T, logs *observer.ObservedLogs) (observer.LoggedEntry, map[string]any) {
	t.Helper()

	all := logs.All()
	if len(all) != 1 {
		t.Fatalf("log entries = %d, want exactly 1 (%v)", len(all), all)
	}

	return all[0], all[0].ContextMap()
}

// logFieldString reads a string field, failing when it is absent or of another
// type.
func logFieldString(t *testing.T, fields map[string]any, key string) string {
	t.Helper()

	v, ok := fields[key]
	if !ok {
		t.Fatalf("log entry has no %q field; fields = %v", key, fields)
	}

	s, ok := v.(string)
	if !ok {
		t.Fatalf("field %q = %T(%v), want string", key, v, v)
	}

	return s
}

// logFieldInt reads the "status" field regardless of the integer type zap
// encoded it as.
func logFieldInt(t *testing.T, fields map[string]any) int64 {
	t.Helper()

	const key = "status"

	v, ok := fields[key]
	if !ok {
		t.Fatalf("log entry has no %q field; fields = %v", key, fields)
	}

	switch n := v.(type) {
	case int:
		return int64(n)
	case int32:
		return int64(n)
	case int64:
		return n
	case float64:
		return int64(n)
	default:
		t.Fatalf("field %q = %T(%v), want a numeric type", key, v, v)

		return 0
	}
}

// wantLog is the expected content of a single "request handled" entry.
type wantLog struct {
	method     string
	path       string
	route      string
	remoteAddr string
	status     int64
}

// assertRequestLog verifies the level, message and full field set of one
// "request handled" entry.
func assertRequestLog(t *testing.T, entry observer.LoggedEntry, fields map[string]any, want wantLog) {
	t.Helper()

	if entry.Level != zapcore.InfoLevel {
		t.Errorf("log level = %v, want %v", entry.Level, zapcore.InfoLevel)
	}

	if entry.Message != logMessage {
		t.Errorf("log message = %q, want %q", entry.Message, logMessage)
	}

	for _, f := range []struct {
		key  string
		want string
	}{
		{"method", want.method},
		{"path", want.path},
		{"route", want.route},
		{"remote_addr", want.remoteAddr},
	} {
		if got := logFieldString(t, fields, f.key); got != f.want {
			t.Errorf("%s field = %q, want %q", f.key, got, f.want)
		}
	}

	if got := logFieldInt(t, fields); got != want.status {
		t.Errorf("status field = %d, want %d", got, want.status)
	}

	d, ok := fields["duration"]
	if !ok {
		t.Fatalf("log entry has no %q field; fields = %v", "duration", fields)
	}

	if dur, ok := d.(time.Duration); ok && dur < 0 {
		t.Errorf("duration field = %v, want a non-negative duration", dur)
	}
}

// TestLogging covers the per-request log line for the cases that share a shape:
// one Info entry with method/path/route/remote_addr/status/duration, and a
// response that reaches the client untouched. Every case is served without a
// chi router, so "route" must be empty.
//
//nolint:paralleltest // swaps the process-global zap logger via newLoggingObserver; must not run beside other tests
func TestLogging(t *testing.T) {
	const remoteAddr = "198.51.100.7:51000"

	tests := []struct {
		name string
		// method and target of the incoming request.
		method string
		target string
		// handler is the wrapped downstream handler.
		handler http.HandlerFunc
		// wantPath is the value expected in the "path" field.
		wantPath string
		// wantLogStatus is the status recorded in the log entry.
		wantLogStatus int64
		// wantClientStatus is what the client observes. It diverges from
		// wantLogStatus when the handler calls WriteHeader more than once:
		// net/http keeps the first code, the wrapper records the last.
		wantClientStatus int
		wantBody         string
	}{
		{
			name:             "handler writes nothing so status defaults to 200",
			method:           http.MethodGet,
			target:           "/",
			handler:          func(http.ResponseWriter, *http.Request) {},
			wantPath:         "/",
			wantLogStatus:    http.StatusOK,
			wantClientStatus: http.StatusOK,
			wantBody:         "",
		},
		{
			name:   "body without WriteHeader is logged as 200",
			method: http.MethodGet,
			target: "/items",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				_, _ = io.WriteString(w, "hello")
			},
			wantPath:         "/items",
			wantLogStatus:    http.StatusOK,
			wantClientStatus: http.StatusOK,
			wantBody:         "hello",
		},
		{
			name:   "explicit 404 reaches both the log and the client",
			method: http.MethodGet,
			target: "/missing",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNotFound)
			},
			wantPath:         "/missing",
			wantLogStatus:    http.StatusNotFound,
			wantClientStatus: http.StatusNotFound,
			wantBody:         "",
		},
		{
			name:   "explicit 500 with a body",
			method: http.MethodPost,
			target: "/items",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = io.WriteString(w, "boom")
			},
			wantPath:         "/items",
			wantLogStatus:    http.StatusInternalServerError,
			wantClientStatus: http.StatusInternalServerError,
			wantBody:         "boom",
		},
		{
			name:   "204 on DELETE",
			method: http.MethodDelete,
			target: "/items/7",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNoContent)
			},
			wantPath:         "/items/7",
			wantLogStatus:    http.StatusNoContent,
			wantClientStatus: http.StatusNoContent,
			wantBody:         "",
		},
		{
			name:   "double WriteHeader logs the last code while the client keeps the first",
			method: http.MethodGet,
			target: "/flaky",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				w.WriteHeader(http.StatusServiceUnavailable)
			},
			wantPath:         "/flaky",
			wantLogStatus:    http.StatusServiceUnavailable,
			wantClientStatus: http.StatusInternalServerError,
			wantBody:         "",
		},
		{
			name:   "query string is not part of the logged path",
			method: http.MethodGet,
			target: "/search?q=milk&page=2",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			},
			wantPath:         "/search",
			wantLogStatus:    http.StatusOK,
			wantClientStatus: http.StatusOK,
			wantBody:         "",
		},
	}

	//nolint:paralleltest // swaps the process-global zap logger via newLoggingObserver; must not run beside other tests
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logs := newLoggingObserver(t)

			h := middleware.Logging(tt.handler)
			if h == nil {
				t.Fatal("Logging returned a nil handler")
			}

			req := httptest.NewRequestWithContext(t.Context(), tt.method, tt.target, nil)
			req.RemoteAddr = remoteAddr
			rec := httptest.NewRecorder()

			h.ServeHTTP(rec, req)

			if rec.Code != tt.wantClientStatus {
				t.Errorf("client status = %d, want %d", rec.Code, tt.wantClientStatus)
			}

			if got := rec.Body.String(); got != tt.wantBody {
				t.Errorf("client body = %q, want %q", got, tt.wantBody)
			}

			entry, fields := singleLogEntry(t, logs)
			assertRequestLog(t, entry, fields, wantLog{
				method:     tt.method,
				path:       tt.wantPath,
				route:      "",
				remoteAddr: remoteAddr,
				status:     tt.wantLogStatus,
			})
		})
	}
}

// TestLoggingRouteField pins the "route" field: inside a chi router it carries
// the pattern (distinct from the concrete path), and outside one it is empty
// without panicking.
//
//nolint:paralleltest // swaps the process-global zap logger via newLoggingObserver; must not run beside other tests
func TestLoggingRouteField(t *testing.T) {
	tests := []struct {
		name string
		// mount serves the request either through a chi router or through the
		// bare middleware.
		mount func(http.Handler) http.Handler
		// target is the concrete request URL.
		target string
		// wantRoute is the expected "route" field.
		wantRoute string
		// wantPath is the expected "path" field.
		wantPath string
	}{
		{
			name: "chi router exposes the route pattern",
			mount: func(next http.Handler) http.Handler {
				r := chi.NewRouter()
				r.Use(middleware.Logging)
				r.Method(http.MethodGet, "/items/{id}", next)

				return r
			},
			target:    "/items/42",
			wantRoute: "/items/{id}",
			wantPath:  "/items/42",
		},
		{
			name: "nested chi router exposes the full pattern",
			mount: func(next http.Handler) http.Handler {
				sub := chi.NewRouter()
				sub.Method(http.MethodGet, "/{id}", next)

				r := chi.NewRouter()
				r.Use(middleware.Logging)
				r.Mount("/groups", sub)

				return r
			},
			target:    "/groups/abc",
			wantRoute: "/groups/{id}",
			wantPath:  "/groups/abc",
		},
		{
			name:      "no chi router yields an empty route",
			mount:     middleware.Logging,
			target:    "/items/42",
			wantRoute: "",
			wantPath:  "/items/42",
		},
	}

	//nolint:paralleltest // swaps the process-global zap logger via newLoggingObserver; must not run beside other tests
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logs := newLoggingObserver(t)

			h := tt.mount(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusTeapot)
			}))

			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, tt.target, nil))

			if rec.Code != http.StatusTeapot {
				t.Fatalf("client status = %d, want %d (request never reached the handler?)", rec.Code, http.StatusTeapot)
			}

			_, fields := singleLogEntry(t, logs)

			if got := logFieldString(t, fields, "route"); got != tt.wantRoute {
				t.Errorf("route field = %q, want %q", got, tt.wantRoute)
			}

			if got := logFieldString(t, fields, "path"); got != tt.wantPath {
				t.Errorf("path field = %q, want %q", got, tt.wantPath)
			}

			if got := logFieldInt(t, fields); got != http.StatusTeapot {
				t.Errorf("status field = %d, want %d", got, http.StatusTeapot)
			}
		})
	}
}

// TestLoggingPassesRequestThrough checks the middleware forwards the original
// *http.Request untouched and writes nothing to the response itself.
//
//nolint:paralleltest // swaps the process-global zap logger via newLoggingObserver; must not run beside other tests
func TestLoggingPassesRequestThrough(t *testing.T) {
	_ = newLoggingObserver(t)

	want := httptest.NewRequestWithContext(t.Context(), http.MethodPut, "/items/1", http.NoBody)
	want.Header.Set("X-Trace", "abc123")

	var got *http.Request

	h := middleware.Logging(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r

		w.Header().Set("X-Handler", "ran")
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, want)

	if got != want {
		t.Errorf("downstream received %p, want the original request %p", got, want)
	}

	if v := rec.Header().Get("X-Handler"); v != "ran" {
		t.Errorf("X-Handler response header = %q, want %q", v, "ran")
	}

	if rec.Body.Len() != 0 {
		t.Errorf("response body = %q, want it untouched by the middleware", rec.Body.String())
	}
}

// TestLoggingPanicInNextSkipsLogLine pins the current absence of a
// defer/recover: a panicking downstream handler escapes the middleware and the
// request produces no log entry at all. If a defer is ever added, this test
// should be updated deliberately rather than silently.
//
//nolint:paralleltest // swaps the process-global zap logger via newLoggingObserver; must not run beside other tests
func TestLoggingPanicInNextSkipsLogLine(t *testing.T) {
	logs := newLoggingObserver(t)

	h := middleware.Logging(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("downstream exploded")
	}))

	func() {
		defer func() {
			r := recover()
			if r == nil {
				t.Error("panic did not propagate out of the middleware")

				return
			}

			if s, ok := r.(string); !ok || s != "downstream exploded" {
				t.Errorf("recovered value = %#v, want %q", r, "downstream exploded")
			}
		}()

		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/boom", nil))
	}()

	if got := logs.Len(); got != 0 {
		t.Errorf("log entries = %d, want 0 (no defer means the line is skipped): %v", got, logs.All())
	}
}

// TestLoggingWrappedWriterDropsOptionalInterfaces documents a real limitation:
// the wrapper only embeds http.ResponseWriter, so streaming or hijacking
// handlers mounted behind Logging lose the optional interfaces the underlying
// writer implements.
//
//nolint:paralleltest // swaps the process-global zap logger via newLoggingObserver; must not run beside other tests
func TestLoggingWrappedWriterDropsOptionalInterfaces(t *testing.T) {
	logs := newLoggingObserver(t)

	var (
		called                              bool
		isFlusher, isHijacker, isReaderFrom bool
	)

	h := middleware.Logging(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		_, isFlusher = w.(http.Flusher)
		_, isHijacker = w.(http.Hijacker)
		_, isReaderFrom = w.(io.ReaderFrom)
	}))

	// Premise of the test: the recorder itself is an http.Flusher, so a
	// negative result below can only come from the wrapper.
	rec := httptest.NewRecorder()

	var _ http.Flusher = rec

	h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/stream", nil))

	if !called {
		t.Fatal("downstream handler was not called")
	}

	if isFlusher {
		t.Error("wrapped writer implements http.Flusher; the pinned behaviour is that it does not")
	}

	if isHijacker {
		t.Error("wrapped writer implements http.Hijacker; the pinned behaviour is that it does not")
	}

	if isReaderFrom {
		t.Error("wrapped writer implements io.ReaderFrom; the pinned behaviour is that it does not")
	}

	if got := logs.Len(); got != 1 {
		t.Errorf("log entries = %d, want 1", got)
	}
}

// TestLoggingLogsEachRequestSeparately checks the returned handler is reusable:
// one entry per request, in order, with per-request status and path.
//
//nolint:paralleltest // swaps the process-global zap logger via newLoggingObserver; must not run beside other tests
func TestLoggingLogsEachRequestSeparately(t *testing.T) {
	logs := newLoggingObserver(t)

	h := middleware.Logging(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/bad" {
			w.WriteHeader(http.StatusBadRequest)

			return
		}

		_, _ = io.WriteString(w, "ok")
	}))

	targets := []string{"/good", "/bad", "/good"}
	wantStatuses := []int64{http.StatusOK, http.StatusBadRequest, http.StatusOK}

	for _, target := range targets {
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodGet, target, nil))
	}

	all := logs.All()
	if len(all) != len(targets) {
		t.Fatalf("log entries = %d, want %d", len(all), len(targets))
	}

	for i, e := range all {
		fields := e.ContextMap()

		if got := logFieldString(t, fields, "path"); got != targets[i] {
			t.Errorf("entry %d path = %q, want %q", i, got, targets[i])
		}

		if got := logFieldInt(t, fields); got != wantStatuses[i] {
			t.Errorf("entry %d status = %d, want %d", i, got, wantStatuses[i])
		}
	}
}

// TestLoggingConcurrentRequestsDoNotShareStatus checks that each request gets
// its own response wrapper, so statuses cannot bleed between requests served
// concurrently by the same wrapped handler. Meaningful under -race.
//
//nolint:paralleltest // swaps the process-global zap logger via newLoggingObserver; must not run beside other tests
func TestLoggingConcurrentRequestsDoNotShareStatus(t *testing.T) {
	logs := newLoggingObserver(t)

	statuses := []int{
		http.StatusOK,
		http.StatusCreated,
		http.StatusAccepted,
		http.StatusBadRequest,
		http.StatusNotFound,
		http.StatusTeapot,
		http.StatusInternalServerError,
		http.StatusServiceUnavailable,
	}

	want := make(map[string]int64, len(statuses))
	paths := make([]string, 0, len(statuses))

	for _, s := range statuses {
		p := "/status/" + strconv.Itoa(s)
		paths = append(paths, p)
		want[p] = int64(s)
	}

	// start releases every goroutine at once so the requests really overlap.
	start := make(chan struct{})

	h := middleware.Logging(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-start
		w.WriteHeader(int(want[r.URL.Path]))
	}))

	var wg sync.WaitGroup

	for _, p := range paths {
		wg.Go(func() {
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, p, nil))

			if int64(rec.Code) != want[p] {
				t.Errorf("client status for %s = %d, want %d", p, rec.Code, want[p])
			}
		})
	}

	close(start)
	wg.Wait()

	all := logs.All()
	if len(all) != len(paths) {
		t.Fatalf("log entries = %d, want %d", len(all), len(paths))
	}

	seen := make(map[string]int64, len(paths))

	for _, e := range all {
		fields := e.ContextMap()
		seen[logFieldString(t, fields, "path")] = logFieldInt(t, fields)
	}

	for p, wantStatus := range want {
		got, ok := seen[p]
		if !ok {
			t.Errorf("no log entry for path %s", p)

			continue
		}

		if got != wantStatus {
			t.Errorf("status logged for %s = %d, want %d", p, got, wantStatus)
		}
	}
}

// TestLoggingNilNextPanicsOnServe pins that Logging itself accepts a nil
// handler (it only closes over it) and the nil dereference surfaces later, when
// a request is served.
//
//nolint:paralleltest // swaps the process-global zap logger via newLoggingObserver; must not run beside other tests
func TestLoggingNilNextPanicsOnServe(t *testing.T) {
	_ = newLoggingObserver(t)

	h := middleware.Logging(nil)
	if h == nil {
		t.Fatal("Logging(nil) returned a nil handler, want a non-nil wrapper")
	}

	defer func() {
		if recover() == nil {
			t.Error("serving with a nil next handler did not panic")
		}
	}()

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/nil", nil))
}
