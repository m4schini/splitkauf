// SPDX-License-Identifier: CC0-1.0

package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// rwCaptureWriter is a minimal http.ResponseWriter that records every
// WriteHeader call verbatim. Unlike httptest.ResponseRecorder it performs no
// validation and no de-duplication, so tests can observe exactly what the
// wrapper forwards — including codes a real writer would reject.
type rwCaptureWriter struct {
	header http.Header
	codes  []int
}

func newRWCaptureWriter() *rwCaptureWriter {
	return &rwCaptureWriter{header: make(http.Header)}
}

func (w *rwCaptureWriter) Header() http.Header         { return w.header }
func (w *rwCaptureWriter) Write(b []byte) (int, error) { return len(b), nil }
func (w *rwCaptureWriter) WriteHeader(status int)      { w.codes = append(w.codes, status) }

// rwPanicWriter mimics net/http's rejection of out-of-range status codes.
type rwPanicWriter struct {
	http.ResponseWriter
}

func (w *rwPanicWriter) WriteHeader(status int) {
	panic("underlying WriteHeader panicked")
}

func equalInts(t *testing.T, got, want []int) bool {
	t.Helper()

	if len(got) != len(want) {
		return false
	}

	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}

	return true
}

// TestResponseWriterWriteHeaderCapturesAndDelegates pins the whole contract of
// the method against a permissive underlying writer: the status argument is
// stored verbatim in rw.status and forwarded unchanged, with no validation and
// no de-duplication of its own.
func TestResponseWriterWriteHeaderCapturesAndDelegates(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		initial    int
		calls      []int
		wantStatus int
		wantCodes  []int
	}{
		{
			name:       "ok",
			initial:    http.StatusOK,
			calls:      []int{http.StatusOK},
			wantStatus: http.StatusOK,
			wantCodes:  []int{http.StatusOK},
		},
		{
			name:       "not found",
			initial:    http.StatusOK,
			calls:      []int{http.StatusNotFound},
			wantStatus: http.StatusNotFound,
			wantCodes:  []int{http.StatusNotFound},
		},
		{
			name:       "server error from zero value",
			initial:    0,
			calls:      []int{http.StatusInternalServerError},
			wantStatus: http.StatusInternalServerError,
			wantCodes:  []int{http.StatusInternalServerError},
		},
		{
			name:       "no call leaves seeded status untouched",
			initial:    http.StatusOK,
			calls:      nil,
			wantStatus: http.StatusOK,
			wantCodes:  nil,
		},
		{
			name:       "no call on zero value leaves status zero",
			initial:    0,
			calls:      nil,
			wantStatus: 0,
			wantCodes:  nil,
		},
		{
			// Last write wins in the wrapper even though a real server sends
			// only the first header and logs "superfluous WriteHeader": the
			// logged status can diverge from what the client saw.
			name:       "double call last write wins",
			initial:    http.StatusOK,
			calls:      []int{http.StatusOK, http.StatusInternalServerError},
			wantStatus: http.StatusInternalServerError,
			wantCodes:  []int{http.StatusOK, http.StatusInternalServerError},
		},
		{
			// Interim 1xx headers are recorded like any other code, so the
			// final status is only reflected if it is written afterwards.
			name:       "informational then final",
			initial:    http.StatusOK,
			calls:      []int{http.StatusEarlyHints, http.StatusNoContent},
			wantStatus: http.StatusNoContent,
			wantCodes:  []int{http.StatusEarlyHints, http.StatusNoContent},
		},
		{
			name:       "informational only",
			initial:    http.StatusOK,
			calls:      []int{http.StatusContinue},
			wantStatus: http.StatusContinue,
			wantCodes:  []int{http.StatusContinue},
		},
		{
			name:       "zero code stored verbatim",
			initial:    http.StatusOK,
			calls:      []int{0},
			wantStatus: 0,
			wantCodes:  []int{0},
		},
		{
			name:       "below range code stored verbatim",
			initial:    http.StatusOK,
			calls:      []int{99},
			wantStatus: 99,
			wantCodes:  []int{99},
		},
		{
			name:       "above range code stored verbatim",
			initial:    http.StatusOK,
			calls:      []int{1000},
			wantStatus: 1000,
			wantCodes:  []int{1000},
		},
		{
			name:       "negative code stored verbatim",
			initial:    http.StatusOK,
			calls:      []int{-1},
			wantStatus: -1,
			wantCodes:  []int{-1},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			under := newRWCaptureWriter()
			rw := &responseWriter{ResponseWriter: under, status: tt.initial}

			for _, code := range tt.calls {
				rw.WriteHeader(code)
			}

			if rw.status != tt.wantStatus {
				t.Errorf("rw.status = %d, want %d", rw.status, tt.wantStatus)
			}

			if !equalInts(t, under.codes, tt.wantCodes) {
				t.Errorf("delegated codes = %v, want %v", under.codes, tt.wantCodes)
			}
		})
	}
}

// TestResponseWriterWriteHeaderDelegatesToRealWriter checks the delegation is
// not dropped when the underlying writer is a real one, and that the wrapper's
// recorded status is the one net/http-style writers actually keep.
func TestResponseWriterWriteHeaderDelegatesToRealWriter(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		status   int
		wantCode int
	}{
		{name: "ok", status: http.StatusOK, wantCode: http.StatusOK},
		{name: "created", status: http.StatusCreated, wantCode: http.StatusCreated},
		{name: "not found", status: http.StatusNotFound, wantCode: http.StatusNotFound},
		{name: "teapot", status: http.StatusTeapot, wantCode: http.StatusTeapot},
		{
			name:     "server error",
			status:   http.StatusInternalServerError,
			wantCode: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rec := httptest.NewRecorder()
			rw := &responseWriter{ResponseWriter: rec, status: http.StatusOK}

			rw.WriteHeader(tt.status)

			if rw.status != tt.status {
				t.Errorf("rw.status = %d, want %d", rw.status, tt.status)
			}

			if rec.Code != tt.wantCode {
				t.Errorf("recorder code = %d, want %d", rec.Code, tt.wantCode)
			}
		})
	}
}

// TestResponseWriterWriteHeaderDoubleCallDivergesFromWire documents that a real
// writer keeps the first header while the wrapper reports the last one.
func TestResponseWriterWriteHeaderDoubleCallDivergesFromWire(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	rw := &responseWriter{ResponseWriter: rec, status: http.StatusOK}

	rw.WriteHeader(http.StatusOK)
	rw.WriteHeader(http.StatusInternalServerError)

	if want := http.StatusInternalServerError; rw.status != want {
		t.Errorf("rw.status = %d, want %d (last write wins in the wrapper)", rw.status, want)
	}

	if want := http.StatusOK; rec.Code != want {
		t.Errorf("recorder code = %d, want %d (first header wins on the wire)", rec.Code, want)
	}
}

// TestResponseWriterWriteHeaderAssignsBeforeDelegating pins the ordering: the
// field is updated before the underlying call, so a panic in the underlying
// writer still leaves the captured status set.
func TestResponseWriterWriteHeaderAssignsBeforeDelegating(t *testing.T) {
	t.Parallel()

	rw := &responseWriter{
		ResponseWriter: &rwPanicWriter{ResponseWriter: httptest.NewRecorder()},
		status:         http.StatusOK,
	}

	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Fatal("WriteHeader did not propagate the underlying panic")
			}
		}()

		rw.WriteHeader(http.StatusBadGateway)
	}()

	if want := http.StatusBadGateway; rw.status != want {
		t.Errorf("rw.status = %d, want %d", rw.status, want)
	}
}

// TestResponseWriterHidesOptionalInterfaces documents a consequence of the
// wrapper: optional interfaces of the underlying writer are not promoted, so
// streaming and hijacking break through this middleware.
func TestResponseWriterHidesOptionalInterfaces(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	if _, ok := any(rec).(http.Flusher); !ok {
		t.Fatal("precondition failed: httptest.ResponseRecorder is not an http.Flusher")
	}

	rw := &responseWriter{ResponseWriter: newRWCaptureWriter(), status: http.StatusOK}
	if _, ok := any(rw).(http.Flusher); ok {
		t.Error("responseWriter reports http.Flusher for a non-flushing writer")
	}

	if _, ok := any(rw).(http.Hijacker); ok {
		t.Error("responseWriter reports http.Hijacker for a non-hijacking writer")
	}
}

// statusSpyWriter is a permissive http.ResponseWriter double: it records every
// WriteHeader argument verbatim, without net/http's range validation or its
// "superfluous WriteHeader" suppression, so a test can observe exactly what the
// wrapper forwards.
type statusSpyWriter struct {
	header http.Header
	codes  []int
	writes int
}

func newStatusSpyWriter() *statusSpyWriter {
	return &statusSpyWriter{header: make(http.Header)}
}

func (w *statusSpyWriter) Header() http.Header { return w.header }

func (w *statusSpyWriter) Write(b []byte) (int, error) {
	w.writes++

	return len(b), nil
}

func (w *statusSpyWriter) WriteHeader(status int) { w.codes = append(w.codes, status) }

// panickingStatusWriter stands in for a writer that rejects the status code, as
// net/http's real writer does for codes outside 100-999.
type panickingStatusWriter struct {
	http.ResponseWriter
}

func (w *panickingStatusWriter) WriteHeader(int) { panic("underlying WriteHeader rejected the code") }

func equalStatusCodes(t *testing.T, got, want []int) bool {
	t.Helper()

	if len(got) != len(want) {
		return false
	}

	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}

	return true
}

// TestResponseWriterWriteHeaderRecordsAndForwards pins the core contract: the
// argument is stored in rw.status and forwarded unchanged to the embedded
// writer, exactly once per call, with no validation and no de-duplication.
func TestResponseWriterWriteHeaderRecordsAndForwards(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		seed       int
		calls      []int
		wantStatus int
		wantCodes  []int
	}{
		{
			name:       "ok",
			seed:       http.StatusOK,
			calls:      []int{http.StatusOK},
			wantStatus: http.StatusOK,
			wantCodes:  []int{http.StatusOK},
		},
		{
			name:       "overrides seeded ok",
			seed:       http.StatusOK,
			calls:      []int{http.StatusNotFound},
			wantStatus: http.StatusNotFound,
			wantCodes:  []int{http.StatusNotFound},
		},
		{
			name:       "zero value writer records first call",
			seed:       0,
			calls:      []int{http.StatusInternalServerError},
			wantStatus: http.StatusInternalServerError,
			wantCodes:  []int{http.StatusInternalServerError},
		},
		{
			name:       "never called keeps seeded status",
			seed:       http.StatusOK,
			calls:      nil,
			wantStatus: http.StatusOK,
			wantCodes:  nil,
		},
		{
			name:       "never called keeps zero status",
			seed:       0,
			calls:      nil,
			wantStatus: 0,
			wantCodes:  nil,
		},
		{
			// Last write wins in the wrapper; both calls are forwarded.
			name:       "second call overwrites and is forwarded",
			seed:       http.StatusOK,
			calls:      []int{http.StatusAccepted, http.StatusBadGateway},
			wantStatus: http.StatusBadGateway,
			wantCodes:  []int{http.StatusAccepted, http.StatusBadGateway},
		},
		{
			name:       "repeated identical calls are not collapsed",
			seed:       http.StatusOK,
			calls:      []int{http.StatusNoContent, http.StatusNoContent, http.StatusNoContent},
			wantStatus: http.StatusNoContent,
			wantCodes:  []int{http.StatusNoContent, http.StatusNoContent, http.StatusNoContent},
		},
		{
			// An interim 1xx header is captured like any other code, so the
			// logged status is the interim one unless a final code follows.
			name:       "interim informational then final",
			seed:       http.StatusOK,
			calls:      []int{http.StatusEarlyHints, http.StatusCreated},
			wantStatus: http.StatusCreated,
			wantCodes:  []int{http.StatusEarlyHints, http.StatusCreated},
		},
		{
			name:       "lower boundary 100",
			seed:       http.StatusOK,
			calls:      []int{100},
			wantStatus: 100,
			wantCodes:  []int{100},
		},
		{
			name:       "upper boundary 599",
			seed:       http.StatusOK,
			calls:      []int{599},
			wantStatus: 599,
			wantCodes:  []int{599},
		},
		{
			name:       "non standard 999 is not validated",
			seed:       http.StatusOK,
			calls:      []int{999},
			wantStatus: 999,
			wantCodes:  []int{999},
		},
		{
			name:       "zero is not validated",
			seed:       http.StatusOK,
			calls:      []int{0},
			wantStatus: 0,
			wantCodes:  []int{0},
		},
		{
			name:       "negative is not validated",
			seed:       http.StatusOK,
			calls:      []int{-42},
			wantStatus: -42,
			wantCodes:  []int{-42},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			spy := newStatusSpyWriter()
			rw := &responseWriter{ResponseWriter: spy, status: tt.seed}

			for _, code := range tt.calls {
				rw.WriteHeader(code)
			}

			if rw.status != tt.wantStatus {
				t.Errorf("rw.status = %d, want %d", rw.status, tt.wantStatus)
			}

			if !equalStatusCodes(t, spy.codes, tt.wantCodes) {
				t.Errorf("forwarded codes = %v, want %v", spy.codes, tt.wantCodes)
			}

			if spy.writes != 0 {
				t.Errorf("underlying Write called %d times, want 0", spy.writes)
			}
		})
	}
}

// TestResponseWriterWriteHeaderReachesRealWriter checks delegation fidelity
// against a real writer: the recorder ends up with the same code the wrapper
// captured.
func TestResponseWriterWriteHeaderReachesRealWriter(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		status int
	}{
		{name: "ok", status: http.StatusOK},
		{name: "created", status: http.StatusCreated},
		{name: "moved permanently", status: http.StatusMovedPermanently},
		{name: "unauthorized", status: http.StatusUnauthorized},
		{name: "teapot", status: http.StatusTeapot},
		{name: "service unavailable", status: http.StatusServiceUnavailable},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rec := httptest.NewRecorder()
			rw := &responseWriter{ResponseWriter: rec, status: http.StatusOK}

			rw.WriteHeader(tt.status)

			if rw.status != tt.status {
				t.Errorf("rw.status = %d, want %d", rw.status, tt.status)
			}

			if rec.Code != rw.status {
				t.Errorf("recorder code = %d, want %d", rec.Code, rw.status)
			}
		})
	}
}

// TestResponseWriterWriteHeaderStoresBeforeDelegating pins the ordering the
// middleware relies on: rw.status is assigned before the embedded writer is
// called, so it is set even when that call panics.
func TestResponseWriterWriteHeaderStoresBeforeDelegating(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		under http.ResponseWriter
	}{
		{
			name:  "underlying writer panics",
			under: &panickingStatusWriter{ResponseWriter: httptest.NewRecorder()},
		},
		{
			// Documents that the zero value responseWriter{} is unusable: the
			// nil embedded interface panics on delegation.
			name:  "nil embedded writer",
			under: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rw := &responseWriter{ResponseWriter: tt.under, status: http.StatusOK}

			func() {
				defer func() {
					if r := recover(); r == nil {
						t.Error("WriteHeader returned normally, want a propagated panic")
					}
				}()

				rw.WriteHeader(http.StatusGatewayTimeout)
			}()

			if want := http.StatusGatewayTimeout; rw.status != want {
				t.Errorf("rw.status = %d, want %d", rw.status, want)
			}
		})
	}
}

// TestResponseWriterStatusUnchangedByImplicitWrite covers the middleware's
// default-status path: Write is not overridden, so an implicit 200 sent by the
// inner writer bypasses the wrapper and leaves rw.status at its seeded value.
func TestResponseWriterStatusUnchangedByImplicitWrite(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		seed       int
		body       string
		wantStatus int
		wantCode   int
	}{
		{
			name:       "seeded ok stays ok",
			seed:       http.StatusOK,
			body:       "hello",
			wantStatus: http.StatusOK,
			wantCode:   http.StatusOK,
		},
		{
			name:       "zero seed is not backfilled",
			seed:       0,
			body:       "hello",
			wantStatus: 0,
			wantCode:   http.StatusOK,
		},
		{
			name:       "no write at all leaves seed untouched",
			seed:       http.StatusOK,
			body:       "",
			wantStatus: http.StatusOK,
			wantCode:   http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rec := httptest.NewRecorder()
			rw := &responseWriter{ResponseWriter: rec, status: tt.seed}

			if tt.body != "" {
				if _, err := rw.Write([]byte(tt.body)); err != nil {
					t.Fatalf("Write() error = %v", err)
				}
			}

			if rw.status != tt.wantStatus {
				t.Errorf("rw.status = %d, want %d", rw.status, tt.wantStatus)
			}

			if rec.Code != tt.wantCode {
				t.Errorf("recorder code = %d, want %d", rec.Code, tt.wantCode)
			}
		})
	}
}
