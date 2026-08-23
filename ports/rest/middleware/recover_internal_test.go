// SPDX-License-Identifier: CC0-1.0

// NOTE: recoverWriter and its wrote field are unexported, so these tests must
// live in the internal test package (package middleware) rather than
// middleware_test.

package middleware

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
)

// recoverWriterStub is a minimal http.ResponseWriter that records every
// WriteHeader call verbatim. Unlike httptest.ResponseRecorder it performs no
// validation and no de-duplication, so tests can observe exactly what the
// wrapper forwards — including codes a real writer would reject — and can be
// told to panic on delegation.
type recoverWriterStub struct {
	header    http.Header
	codes     []int
	writes    int
	panicWith any
}

func (w *recoverWriterStub) Header() http.Header {
	if w.header == nil {
		w.header = make(http.Header)
	}

	return w.header
}

func (w *recoverWriterStub) Write(b []byte) (int, error) {
	w.writes++

	return len(b), nil
}

func (w *recoverWriterStub) WriteHeader(status int) {
	w.codes = append(w.codes, status)
	if w.panicWith != nil {
		panic(w.panicWith)
	}
}

func TestRecoverWriterWriteHeader(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		calls []int
		want  []int
	}{
		{
			name:  "created is forwarded unchanged",
			calls: []int{http.StatusCreated},
			want:  []int{201},
		},
		{
			name:  "not found is forwarded unchanged",
			calls: []int{http.StatusNotFound},
			want:  []int{404},
		},
		{
			name:  "informational code is not clamped",
			calls: []int{http.StatusContinue},
			want:  []int{100},
		},
		{
			name:  "unregistered code passes through verbatim",
			calls: []int{599},
			want:  []int{599},
		},
		{
			name:  "negative code is not validated",
			calls: []int{-1},
			want:  []int{-1},
		},
		{
			name:  "repeated calls are not de-duplicated",
			calls: []int{http.StatusOK, http.StatusOK},
			want:  []int{200, 200},
		},
		{
			name:  "each of several distinct calls delegates again",
			calls: []int{http.StatusOK, http.StatusInternalServerError, http.StatusTeapot},
			want:  []int{200, 500, 418},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			stub := &recoverWriterStub{}
			rw := &recoverWriter{ResponseWriter: stub}

			if rw.wrote {
				t.Fatalf("wrote = true before any WriteHeader call, want false")
			}

			for _, status := range tt.calls {
				rw.WriteHeader(status)
			}

			if !rw.wrote {
				t.Errorf("wrote = false after WriteHeader, want true")
			}

			if !slices.Equal(stub.codes, tt.want) {
				t.Errorf("delegated codes = %v, want %v", stub.codes, tt.want)
			}

			if stub.writes != 0 {
				t.Errorf("underlying Write called %d times, want 0", stub.writes)
			}
		})
	}
}

// TestRecoverWriterWriteHeaderSetsWroteBeforeDelegating pins the ordering that
// lets Recover re-panic instead of writing a second response: wrote must be
// true even when the underlying WriteHeader panics part-way through.
func TestRecoverWriterWriteHeaderSetsWroteBeforeDelegating(t *testing.T) {
	t.Parallel()

	stub := &recoverWriterStub{panicWith: "boom"}
	rw := &recoverWriter{ResponseWriter: stub}

	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Errorf("underlying panic did not propagate through WriteHeader")
			} else if r != "boom" {
				t.Errorf("recovered %v, want \"boom\"", r)
			}
		}()

		rw.WriteHeader(http.StatusInternalServerError)
	}()

	if !rw.wrote {
		t.Errorf("wrote = false after panicking delegation, want true")
	}

	if !slices.Equal(stub.codes, []int{500}) {
		t.Errorf("delegated codes = %v, want [500]", stub.codes)
	}
}

// TestRecoverWriterWriteHeaderAloneMarksWrote distinguishes the two entry
// points that flip wrote: a bodiless WriteHeader must be sufficient on its own,
// with no Write involved.
func TestRecoverWriterWriteHeaderAloneMarksWrote(t *testing.T) {
	t.Parallel()

	stub := &recoverWriterStub{}
	rw := &recoverWriter{ResponseWriter: stub}

	rw.Header().Set("X-Test", "set")

	if rw.wrote {
		t.Fatalf("wrote = true after touching headers only, want false")
	}

	rw.WriteHeader(http.StatusNoContent)

	if !rw.wrote {
		t.Errorf("wrote = false after bodiless WriteHeader, want true")
	}

	if stub.writes != 0 {
		t.Errorf("underlying Write called %d times, want 0", stub.writes)
	}

	if got := stub.Header().Get("X-Test"); got != "set" {
		t.Errorf("underlying header X-Test = %q, want %q", got, "set")
	}
}

// stubResponseWriter is a minimal http.ResponseWriter standing in for the
// wrapped writer. It records every Write and WriteHeader call and returns a
// scripted (n, err) pair so the pass-through behaviour of recoverWriter.Write
// can be observed exactly.
type stubResponseWriter struct {
	header      http.Header
	writes      [][]byte
	statusCodes []int

	// n and err are what Write reports back to the caller.
	n   int
	err error

	// onWrite, when set, runs at the start of Write. It lets a test observe
	// the state of the wrapping recoverWriter at the moment delegation
	// happens.
	onWrite func()
}

func (w *stubResponseWriter) Header() http.Header {
	if w.header == nil {
		w.header = make(http.Header)
	}

	return w.header
}

func (w *stubResponseWriter) Write(b []byte) (int, error) {
	if w.onWrite != nil {
		w.onWrite()
	}

	w.writes = append(w.writes, b)

	return w.n, w.err
}

func (w *stubResponseWriter) WriteHeader(statusCode int) {
	w.statusCodes = append(w.statusCodes, statusCode)
}

// errRecoverWriterConnReset stands in for the transport error
// TestRecoverWriterWrite exercises below.
var errRecoverWriterConnReset = errors.New("connection reset by peer")

func TestRecoverWriterWrite(t *testing.T) {
	t.Parallel()

	underlyingErr := errRecoverWriterConnReset

	tests := []struct {
		name string
		// body is passed to recoverWriter.Write.
		body []byte
		// stubN and stubErr are what the wrapped ResponseWriter reports.
		stubN   int
		stubErr error
		// wantN is the byte count recoverWriter.Write must return.
		wantN int
		// wantErrIs, when non-nil, must be reachable via errors.Is on the
		// returned (wrapped) error.
		wantErrIs error
	}{
		{
			name:  "success returns the underlying byte count and no error",
			body:  []byte("hello"),
			stubN: 5,
			wantN: 5,
		},
		{
			name:  "empty slice is forwarded and still succeeds",
			body:  []byte{},
			stubN: 0,
			wantN: 0,
		},
		{
			name:  "nil slice is forwarded and still succeeds",
			body:  nil,
			stubN: 0,
			wantN: 0,
		},
		{
			name:      "underlying error is wrapped",
			body:      []byte("hello"),
			stubN:     0,
			stubErr:   underlyingErr,
			wantN:     0,
			wantErrIs: underlyingErr,
		},
		{
			name:      "short write passes the partial count through with the wrapped error",
			body:      []byte("hello"),
			stubN:     3,
			stubErr:   underlyingErr,
			wantN:     3,
			wantErrIs: underlyingErr,
		},
		{
			name:      "empty slice that fails still reports the wrapped error",
			body:      []byte{},
			stubN:     0,
			stubErr:   underlyingErr,
			wantN:     0,
			wantErrIs: underlyingErr,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			stub := &stubResponseWriter{n: tt.stubN, err: tt.stubErr}
			rw := &recoverWriter{ResponseWriter: stub}

			n, err := rw.Write(tt.body)

			if n != tt.wantN {
				t.Errorf("n = %d, want %d", n, tt.wantN)
			}

			if tt.wantErrIs == nil {
				if err != nil {
					t.Errorf("err = %v, want nil", err)
				}
			} else {
				assertWrappedWriteError(t, err, tt.wantErrIs)
			}

			// The response counts as started regardless of the outcome, so
			// Recover must re-panic instead of emitting a problem body.
			if !rw.wrote {
				t.Errorf("wrote = false, want true after Write")
			}

			if len(stub.writes) != 1 {
				t.Fatalf("underlying Write called %d times, want 1", len(stub.writes))
			}

			if !bytes.Equal(stub.writes[0], tt.body) {
				t.Errorf("underlying Write got %q, want %q", stub.writes[0], tt.body)
			}

			// Sending the implicit 200 is the embedded ResponseWriter's job.
			if len(stub.statusCodes) != 0 {
				t.Errorf("underlying WriteHeader called with %v, want no calls", stub.statusCodes)
			}
		})
	}
}

// errRecoverWriterBoom stands in for the write failure
// TestRecoverWriterWriteSetsWroteBeforeDelegating exercises below.
var errRecoverWriterBoom = errors.New("boom")

func TestRecoverWriterWriteSetsWroteBeforeDelegating(t *testing.T) {
	t.Parallel()

	stub := &stubResponseWriter{err: errRecoverWriterBoom}
	rw := &recoverWriter{ResponseWriter: stub}

	var wroteDuringDelegation bool

	stub.onWrite = func() { wroteDuringDelegation = rw.wrote }

	if _, err := rw.Write([]byte("body")); err == nil {
		t.Fatalf("Write err = nil, want the underlying error wrapped")
	}

	if !wroteDuringDelegation {
		t.Errorf("wrote = false while the underlying Write ran, want it set before delegation")
	}
}

func TestRecoverWriterWriteSequentialWritesKeepWroteSet(t *testing.T) {
	t.Parallel()

	stub := &stubResponseWriter{n: 4}
	rw := &recoverWriter{ResponseWriter: stub}

	if rw.wrote {
		t.Fatalf("wrote = true before any Write, want false")
	}

	bodies := [][]byte{[]byte("aaaa"), []byte("bbbb"), []byte("cccc")}
	for i, body := range bodies {
		n, err := rw.Write(body)
		if err != nil {
			t.Fatalf("Write #%d: unexpected error: %v", i, err)
		}

		if n != len(body) {
			t.Errorf("Write #%d: n = %d, want %d", i, n, len(body))
		}

		if !rw.wrote {
			t.Errorf("Write #%d: wrote = false, want it to stay true", i)
		}
	}

	if len(stub.writes) != len(bodies) {
		t.Fatalf("underlying Write called %d times, want %d", len(stub.writes), len(bodies))
	}

	for i, body := range bodies {
		if !bytes.Equal(stub.writes[i], body) {
			t.Errorf("underlying Write #%d got %q, want %q", i, stub.writes[i], body)
		}
	}
}

// assertWrappedWriteError fails the test unless err wraps (rather than simply
// is) underlying and carries the "writing response: " prefix.
func assertWrappedWriteError(t *testing.T, err, underlying error) {
	t.Helper()

	if err == nil {
		t.Fatalf("err = nil, want an error wrapping %v", underlying)
	}

	if !errors.Is(err, underlying) {
		t.Errorf("errors.Is(err, underlying) = false for err %v", err)
	}

	if errors.Unwrap(err) == nil {
		t.Errorf("err = %v, want the underlying error wrapped, not returned unchanged", err)
	}

	const wantPrefix = "writing response: "
	if !strings.HasPrefix(err.Error(), wantPrefix) {
		t.Errorf("err.Error() = %q, want prefix %q", err.Error(), wantPrefix)
	}
}

// rcwPlainWriter is a minimal http.ResponseWriter that deliberately does NOT
// implement http.Flusher, so tests can exercise the branch where
// recoverWriter.Flush has nothing to forward to.
type rcwPlainWriter struct {
	header http.Header
	codes  []int
	body   []byte
}

func newRCWPlainWriter() *rcwPlainWriter {
	return &rcwPlainWriter{header: make(http.Header)}
}

func (w *rcwPlainWriter) Header() http.Header    { return w.header }
func (w *rcwPlainWriter) WriteHeader(status int) { w.codes = append(w.codes, status) }

func (w *rcwPlainWriter) Write(b []byte) (int, error) {
	w.body = append(w.body, b...)

	return len(b), nil
}

// rcwFlushWriter is an http.ResponseWriter that additionally implements
// http.Flusher and counts every Flush it receives, so tests can prove the
// wrapper forwards flushes rather than swallowing them.
type rcwFlushWriter struct {
	*rcwPlainWriter

	flushes int
}

func newRCWFlushWriter() *rcwFlushWriter {
	return &rcwFlushWriter{rcwPlainWriter: newRCWPlainWriter()}
}

func (w *rcwFlushWriter) Flush() { w.flushes++ }

// TestRecoverWriterFlush pins the whole contract of the method: it forwards to
// the wrapped writer when that writer is an http.Flusher, is a silent no-op
// otherwise, and never touches the wrote flag either way — flushing alone must
// not make Recover treat the response as started.
func TestRecoverWriterFlush(t *testing.T) {
	t.Parallel()

	// Each factory returns the writer to wrap plus an accessor for the number
	// of flushes that writer observed (always 0 when it is not an
	// http.Flusher, since it has no Flush method to call).
	flushable := func() (http.ResponseWriter, func() int) {
		inner := newRCWFlushWriter()

		return inner, func() int { return inner.flushes }
	}
	plain := func() (http.ResponseWriter, func() int) {
		return newRCWPlainWriter(), func() int { return 0 }
	}
	missing := func() (http.ResponseWriter, func() int) {
		return nil, func() int { return 0 }
	}

	tests := []struct {
		name        string
		newWriter   func() (http.ResponseWriter, func() int)
		before      func(rw *recoverWriter)
		flushCalls  int
		wantFlushes int
		wantWrote   bool
	}{
		{
			name:        "forwards a single flush to a flusher-capable writer",
			newWriter:   flushable,
			flushCalls:  1,
			wantFlushes: 1,
			wantWrote:   false,
		},
		{
			name:        "forwards every flush of a streaming response",
			newWriter:   flushable,
			flushCalls:  3,
			wantFlushes: 3,
			wantWrote:   false,
		},
		{
			name:        "is a no-op when the underlying writer is not a flusher",
			newWriter:   plain,
			flushCalls:  1,
			wantFlushes: 0,
			wantWrote:   false,
		},
		{
			name:        "is a no-op when there is no underlying writer at all",
			newWriter:   missing,
			flushCalls:  2,
			wantFlushes: 0,
			wantWrote:   false,
		},
		{
			name:      "keeps wrote true and still forwards after WriteHeader",
			newWriter: flushable,
			before: func(rw *recoverWriter) {
				rw.WriteHeader(http.StatusOK)
			},
			flushCalls:  1,
			wantFlushes: 1,
			wantWrote:   true,
		},
		{
			name:      "keeps wrote true and still forwards after Write",
			newWriter: flushable,
			before: func(rw *recoverWriter) {
				if _, err := rw.Write([]byte("data: hello\n\n")); err != nil {
					panic(err)
				}
			},
			flushCalls:  2,
			wantFlushes: 2,
			wantWrote:   true,
		},
		{
			name:      "stays a no-op after Write when the writer is not a flusher",
			newWriter: plain,
			before: func(rw *recoverWriter) {
				if _, err := rw.Write([]byte("data: hello\n\n")); err != nil {
					panic(err)
				}
			},
			flushCalls:  1,
			wantFlushes: 0,
			wantWrote:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			inner, flushes := tt.newWriter()
			rw := &recoverWriter{ResponseWriter: inner}

			if tt.before != nil {
				tt.before(rw)
			}

			// A missing Flusher must be tolerated silently: no panic, no error,
			// no fallback.
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("Flush() panicked: %v", r)
				}
			}()

			for range tt.flushCalls {
				rw.Flush()
			}

			if got := flushes(); got != tt.wantFlushes {
				t.Errorf("underlying Flush called %d times, want %d", got, tt.wantFlushes)
			}

			if rw.wrote != tt.wantWrote {
				t.Errorf("rw.wrote = %t, want %t", rw.wrote, tt.wantWrote)
			}
		})
	}
}

// TestRecoverWriterSatisfiesFlusher checks the reason the method exists at all:
// a streaming/SSE handler only ever sees an http.ResponseWriter, so the wrapper
// must still type-assert to http.Flusher and reach the real writer through it.
func TestRecoverWriterSatisfiesFlusher(t *testing.T) {
	t.Parallel()

	var _ http.Flusher = (*recoverWriter)(nil)

	rec := httptest.NewRecorder()

	var w http.ResponseWriter = &recoverWriter{ResponseWriter: rec}

	flusher, ok := w.(http.Flusher)
	if !ok {
		t.Fatal("recoverWriter does not satisfy http.Flusher; streaming handlers would lose flushing")
	}

	flusher.Flush()

	if !rec.Flushed {
		t.Error("underlying ResponseWriter was not flushed, want flush forwarded")
	}
}

// whStubWriter is a minimal http.ResponseWriter used by the WriteHeader
// contract tests. It records every status it is handed, without the
// validation or de-duplication a real writer performs, and can be told to
// panic on delegation so the ordering of rw.wrote versus the delegated call
// can be observed.
type whStubWriter struct {
	header    http.Header
	codes     []int
	bodyBytes int
	panicWith any
}

func (w *whStubWriter) Header() http.Header {
	if w.header == nil {
		w.header = make(http.Header)
	}

	return w.header
}

func (w *whStubWriter) Write(b []byte) (int, error) {
	w.bodyBytes += len(b)

	return len(b), nil
}

func (w *whStubWriter) WriteHeader(status int) {
	w.codes = append(w.codes, status)
	if w.panicWith != nil {
		panic(w.panicWith)
	}
}

// TestRecoverWriterWriteHeaderContract pins the whole documented contract in
// one table: every call sets wrote (before delegating, so a panicking
// underlying writer still leaves the flag true), the status reaches the
// wrapped writer unmodified for any int, and the wrapper neither de-duplicates
// repeated calls nor writes a body of its own.
func TestRecoverWriterWriteHeaderContract(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		calls     []int
		panicWith any
		wantCodes []int
		wantPanic any
	}{
		{
			name:      "ok is forwarded unchanged",
			calls:     []int{http.StatusOK},
			wantCodes: []int{200},
		},
		{
			name:      "informational status still marks the response started",
			calls:     []int{http.StatusContinue},
			wantCodes: []int{100},
		},
		{
			name:      "server error status still marks the response started",
			calls:     []int{http.StatusInternalServerError},
			wantCodes: []int{500},
		},
		{
			name:      "upper boundary code passes through verbatim",
			calls:     []int{599},
			wantCodes: []int{599},
		},
		{
			name:      "wrapper performs no de-duplication",
			calls:     []int{http.StatusAccepted, http.StatusAccepted, http.StatusAccepted},
			wantCodes: []int{202, 202, 202},
		},
		{
			name:      "each distinct call is delegated in order",
			calls:     []int{http.StatusOK, http.StatusTeapot, http.StatusBadGateway},
			wantCodes: []int{200, 418, 502},
		},
		{
			name:      "wrote is set before a panicking delegation",
			calls:     []int{http.StatusServiceUnavailable},
			panicWith: "underlying exploded",
			wantCodes: []int{503},
			wantPanic: "underlying exploded",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			stub := &whStubWriter{panicWith: tt.panicWith}
			rw := &recoverWriter{ResponseWriter: stub}

			if rw.wrote {
				t.Fatalf("wrote = true before any call, want false")
			}

			//nolint:nonamedreturns // defer/recover requires a named return to capture the panic value
			recovered := func() (r any) {
				defer func() { r = recover() }()

				for _, status := range tt.calls {
					rw.WriteHeader(status)
				}

				return nil
			}()

			if recovered != tt.wantPanic {
				t.Errorf("recovered = %v, want %v", recovered, tt.wantPanic)
			}

			if !rw.wrote {
				t.Errorf("wrote = false after WriteHeader, want true")
			}

			if !slices.Equal(stub.codes, tt.wantCodes) {
				t.Errorf("delegated codes = %v, want %v", stub.codes, tt.wantCodes)
			}

			if stub.bodyBytes != 0 {
				t.Errorf("underlying received %d body bytes, want 0", stub.bodyBytes)
			}
		})
	}
}

// TestRecoverWriterWriteHeaderNilUnderlyingStillMarksWrote documents the
// consequence of the set-then-delegate ordering when the constructor
// invariant (Recover always supplies a real writer) is violated: the nil
// dereference happens only after wrote has already flipped, so Recover would
// re-panic rather than try to write a problem body onto a broken writer.
func TestRecoverWriterWriteHeaderNilUnderlyingStillMarksWrote(t *testing.T) {
	t.Parallel()

	rw := &recoverWriter{}

	func() {
		defer func() {
			if recover() == nil {
				t.Errorf("WriteHeader with a nil ResponseWriter did not panic")
			}
		}()

		rw.WriteHeader(http.StatusOK)
	}()

	if !rw.wrote {
		t.Errorf("wrote = false after nil delegation panic, want true")
	}
}

// TestRecoverWriterWriteHeaderAgainstRecorder checks the wrapper against a
// real net/http-style writer: the status reaches it, no body is produced by a
// header-only response, headers set through the wrapper survive, and the
// wrapper's lack of de-duplication leaves the underlying writer's own
// superfluous-WriteHeader handling in charge of the final status.
func TestRecoverWriterWriteHeaderAgainstRecorder(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	rw := &recoverWriter{ResponseWriter: rec}

	rw.Header().Set("X-Trace", "abc123")
	rw.WriteHeader(http.StatusTeapot)

	if !rw.wrote {
		t.Fatalf("wrote = false after WriteHeader, want true")
	}

	// A second call is forwarded by the wrapper; the recorder, like a real
	// writer, keeps the first status it saw.
	rw.WriteHeader(http.StatusInternalServerError)

	if !rw.wrote {
		t.Errorf("wrote = false after second WriteHeader, want true")
	}

	res := rec.Result()
	defer func() { _ = res.Body.Close() }()

	if res.StatusCode != http.StatusTeapot {
		t.Errorf("status = %d, want %d", res.StatusCode, http.StatusTeapot)
	}

	if got := res.Header.Get("X-Trace"); got != "abc123" {
		t.Errorf("X-Trace = %q, want %q", got, "abc123")
	}

	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("reading body: %v", err)
	}

	if len(body) != 0 {
		t.Errorf("body = %q, want empty for a header-only response", body)
	}
}

// rcwWriteStub is a minimal http.ResponseWriter that records exactly what
// recoverWriter forwards and answers Write with a scripted (n, err) pair, so
// the pass-through behaviour can be observed without a real connection.
type rcwWriteStub struct {
	header http.Header
	writes [][]byte
	codes  []int

	// n and err are what Write reports back to its caller.
	n   int
	err error

	// onWrite, when set, runs at the start of Write. It lets a test inspect
	// the wrapping recoverWriter at the exact moment delegation happens.
	onWrite func()
}

func (w *rcwWriteStub) Header() http.Header {
	if w.header == nil {
		w.header = make(http.Header)
	}

	return w.header
}

func (w *rcwWriteStub) Write(b []byte) (int, error) {
	if w.onWrite != nil {
		w.onWrite()
	}

	w.writes = append(w.writes, b)

	return w.n, w.err
}

func (w *rcwWriteStub) WriteHeader(status int) {
	w.codes = append(w.codes, status)
}

// errRecoverWriterContractConnReset stands in for the transport error
// TestRecoverWriterWriteContract exercises below.
var errRecoverWriterContractConnReset = errors.New("connection reset by peer")

func TestRecoverWriterWriteContract(t *testing.T) {
	t.Parallel()

	errUnderlying := errRecoverWriterContractConnReset

	tests := []struct {
		name string
		// body is handed to recoverWriter.Write.
		body []byte
		// stubN and stubErr are what the wrapped writer answers with.
		stubN   int
		stubErr error
		// wantN is the count recoverWriter.Write must report back.
		wantN int
		// wantErrIs, when non-nil, must stay reachable through errors.Is.
		wantErrIs error
	}{
		{
			name:  "successful write reports the underlying count",
			body:  []byte("hello"),
			stubN: 5,
			wantN: 5,
		},
		{
			name:  "empty slice still succeeds with zero bytes",
			body:  []byte{},
			stubN: 0,
			wantN: 0,
		},
		{
			name:  "nil slice still succeeds with zero bytes",
			body:  nil,
			stubN: 0,
			wantN: 0,
		},
		{
			name:      "underlying failure is wrapped, not replaced",
			body:      []byte("hello"),
			stubErr:   errUnderlying,
			wantN:     0,
			wantErrIs: errUnderlying,
		},
		{
			name:      "short write passes the partial count through",
			body:      []byte("hello"),
			stubN:     3,
			stubErr:   io.ErrShortWrite,
			wantN:     3,
			wantErrIs: io.ErrShortWrite,
		},
		{
			name:      "empty slice that fails still reports the wrapped error",
			body:      []byte{},
			stubErr:   errUnderlying,
			wantN:     0,
			wantErrIs: errUnderlying,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			stub := &rcwWriteStub{n: tt.stubN, err: tt.stubErr}
			rw := &recoverWriter{ResponseWriter: stub}

			// The flag must be set before delegation: Recover reads it from a
			// deferred handler that may run while the write is still in
			// flight further down the stack.
			var wroteDuringDelegation bool

			stub.onWrite = func() { wroteDuringDelegation = rw.wrote }

			if rw.wrote {
				t.Fatalf("wrote = true before any Write, want false")
			}

			n, err := rw.Write(tt.body)

			if n != tt.wantN {
				t.Errorf("n = %d, want %d", n, tt.wantN)
			}

			if tt.wantErrIs == nil {
				if err != nil {
					t.Errorf("err = %v, want nil", err)
				}
			} else {
				assertRecoverWriteErrorWrapped(t, err, tt.wantErrIs)
			}

			// True even for a failed or zero-byte write: the response counts
			// as started, so Recover must re-panic instead of emitting a
			// problem body over it.
			if !rw.wrote {
				t.Errorf("wrote = false after Write, want true")
			}

			if !wroteDuringDelegation {
				t.Errorf("wrote = false while the underlying Write ran, want it set before delegating")
			}

			if len(stub.writes) != 1 {
				t.Fatalf("underlying Write called %d times, want 1", len(stub.writes))
			}

			if !bytes.Equal(stub.writes[0], tt.body) {
				t.Errorf("underlying Write got %q, want %q", stub.writes[0], tt.body)
			}

			// Sending the implicit 200 is the embedded writer's job; the
			// wrapper must not synthesise a WriteHeader of its own.
			if len(stub.codes) != 0 {
				t.Errorf("underlying WriteHeader called with %v, want no calls", stub.codes)
			}
		})
	}
}

// rcwOp is a single call made against the wrapper: a WriteHeader(status) when
// status is non-zero, otherwise a Write(body).
type rcwOp struct {
	status int
	body   []byte
}

func TestRecoverWriterWriteCallSequences(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		ops  []rcwOp
		// wantCodes are the statuses forwarded to the underlying writer.
		wantCodes []int
		// wantBodies are the payloads forwarded to the underlying writer.
		wantBodies [][]byte
	}{
		{
			name:       "write without a prior WriteHeader",
			ops:        []rcwOp{{body: []byte("implicit 200")}},
			wantBodies: [][]byte{[]byte("implicit 200")},
		},
		{
			name:       "WriteHeader then Write keeps the flag set",
			ops:        []rcwOp{{status: http.StatusAccepted}, {body: []byte("body")}},
			wantCodes:  []int{http.StatusAccepted},
			wantBodies: [][]byte{[]byte("body")},
		},
		{
			name:       "Write then WriteHeader keeps the flag set",
			ops:        []rcwOp{{body: []byte("body")}, {status: http.StatusTeapot}},
			wantCodes:  []int{http.StatusTeapot},
			wantBodies: [][]byte{[]byte("body")},
		},
		{
			name:       "an empty first write already starts the response",
			ops:        []rcwOp{{body: []byte{}}, {body: []byte("later")}},
			wantBodies: [][]byte{{}, []byte("later")},
		},
		{
			name: "repeated writes never toggle the flag back",
			ops: []rcwOp{
				{body: []byte("aaa")},
				{body: []byte("bbb")},
				{body: []byte("ccc")},
			},
			wantBodies: [][]byte{[]byte("aaa"), []byte("bbb"), []byte("ccc")},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			stub := &rcwWriteStub{}
			rw := &recoverWriter{ResponseWriter: stub}

			if rw.wrote {
				t.Fatalf("wrote = true before any call, want false")
			}

			for i, op := range tt.ops {
				if op.status != 0 {
					rw.WriteHeader(op.status)
				} else {
					stub.n = len(op.body)

					n, err := rw.Write(op.body)
					if err != nil {
						t.Fatalf("op %d: unexpected error: %v", i, err)
					}

					if n != len(op.body) {
						t.Errorf("op %d: n = %d, want %d", i, n, len(op.body))
					}
				}

				if !rw.wrote {
					t.Errorf("op %d: wrote = false, want true from the first call onwards", i)
				}
			}

			if !slices.Equal(stub.codes, tt.wantCodes) {
				t.Errorf("forwarded statuses = %v, want %v", stub.codes, tt.wantCodes)
			}

			if len(stub.writes) != len(tt.wantBodies) {
				t.Fatalf("underlying Write called %d times, want %d", len(stub.writes), len(tt.wantBodies))
			}

			for i, want := range tt.wantBodies {
				if !bytes.Equal(stub.writes[i], want) {
					t.Errorf("underlying Write #%d got %q, want %q", i, stub.writes[i], want)
				}
			}
		})
	}
}

// assertRecoverWriteErrorWrapped fails unless err wraps (rather than simply
// is) underlying and carries the "writing response: " prefix.
func assertRecoverWriteErrorWrapped(t *testing.T, err, underlying error) {
	t.Helper()

	if err == nil {
		t.Fatalf("err = nil, want an error wrapping %v", underlying)
	}

	if !errors.Is(err, underlying) {
		t.Errorf("errors.Is(err, %v) = false for err %v", underlying, err)
	}

	if errors.Unwrap(err) == nil {
		t.Errorf("err = %v, want the underlying error wrapped, not returned unchanged", err)
	}

	const wantPrefix = "writing response: "
	if !strings.HasPrefix(err.Error(), wantPrefix) {
		t.Errorf("err.Error() = %q, want prefix %q", err.Error(), wantPrefix)
	}
}

// flushTrackingWriter is an http.ResponseWriter that additionally implements
// http.Flusher and counts every Flush it receives, so a test can prove the
// wrapper forwards rather than swallows the call.
type flushTrackingWriter struct {
	http.ResponseWriter

	flushes int
}

func (w *flushTrackingWriter) Flush() { w.flushes++ }

// TestRecoverWriterFlushLeavesWroteUnchanged pins that Flush never touches the
// flag in either direction: it does not set it on its own, and it does not
// clear it once a Write has set it.
func TestRecoverWriterFlushLeavesWroteUnchanged(t *testing.T) {
	t.Parallel()

	inner := &flushTrackingWriter{ResponseWriter: httptest.NewRecorder()}
	rw := &recoverWriter{ResponseWriter: inner}

	rw.Flush()

	if rw.wrote {
		t.Errorf("rw.wrote = true after a flush-only response, want false")
	}

	if _, err := rw.Write([]byte("data: hello\n\n")); err != nil {
		t.Fatalf("Write returned unexpected error: %v", err)
	}

	rw.Flush()

	if !rw.wrote {
		t.Errorf("rw.wrote = false after Write then Flush, want it left true")
	}

	if inner.flushes != 2 {
		t.Errorf("underlying Flush called %d times, want 2", inner.flushes)
	}
}
