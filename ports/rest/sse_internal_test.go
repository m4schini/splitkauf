// SPDX-License-Identifier: CC0-1.0

package rest

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

	"github.com/m4schini/splitkauf/events"
)

// hbWriter is the contract the writeHeartbeat stubs share: a minimal
// http.ResponseWriter that records what was written and how often the
// http.ResponseController managed to flush it.
type hbWriter interface {
	http.ResponseWriter
	written() string
	writes() int
	flushes() int
}

type hbBase struct {
	hdr      http.Header
	buf      []byte
	nWrites  int
	shortN   int // when > 0, Write reports this count instead of len(p)
	writeErr error
}

func (w *hbBase) Header() http.Header {
	if w.hdr == nil {
		w.hdr = make(http.Header)
	}

	return w.hdr
}

func (w *hbBase) Write(p []byte) (int, error) {
	w.nWrites++
	if w.writeErr != nil {
		return 0, w.writeErr
	}

	w.buf = append(w.buf, p...)
	if w.shortN > 0 {
		return w.shortN, nil
	}

	return len(p), nil
}

func (w *hbBase) WriteHeader(int) {}

func (w *hbBase) written() string { return string(w.buf) }

func (w *hbBase) writes() int { return w.nWrites }

// hbFlushWriter supports flushing through http.ResponseController.
type hbFlushWriter struct {
	hbBase

	nFlushes int
	flushErr error
}

func (w *hbFlushWriter) FlushError() error {
	w.nFlushes++

	return w.flushErr
}

func (w *hbFlushWriter) flushes() int { return w.nFlushes }

// hbNoFlushWriter deliberately implements neither FlushError nor Flush, so
// http.ResponseController.Flush reports http.ErrNotSupported.
type hbNoFlushWriter struct {
	hbBase
}

func (w *hbNoFlushWriter) flushes() int { return 0 }

// errHeartbeatConnReset stands in for the transport error a broken heartbeat
// writer reports below.
var errHeartbeatConnReset = errors.New("connection reset by peer")

func TestWriteHeartbeat(t *testing.T) {
	t.Parallel()

	// An SSE comment line plus the blank line that terminates the frame.
	const wantFrame = ": ping\n\n"

	errBroken := errHeartbeatConnReset

	tests := []struct {
		name        string
		newWriter   func() hbWriter
		want        bool
		wantBody    string
		wantWrites  int
		wantFlushes int
	}{
		{
			name:        "write and flush succeed",
			newWriter:   func() hbWriter { return &hbFlushWriter{} },
			want:        true,
			wantBody:    wantFrame,
			wantWrites:  1,
			wantFlushes: 1,
		},
		{
			name: "write failure short-circuits flush",
			newWriter: func() hbWriter {
				return &hbFlushWriter{hbBase: hbBase{writeErr: errBroken}}
			},
			want:        false,
			wantBody:    "",
			wantWrites:  1,
			wantFlushes: 0,
		},
		{
			name:        "flush failure ends stream",
			newWriter:   func() hbWriter { return &hbFlushWriter{flushErr: errBroken} },
			want:        false,
			wantBody:    wantFrame,
			wantWrites:  1,
			wantFlushes: 1,
		},
		{
			name:        "writer without flush support",
			newWriter:   func() hbWriter { return &hbNoFlushWriter{} },
			want:        false,
			wantBody:    wantFrame,
			wantWrites:  1,
			wantFlushes: 0,
		},
		{
			// The byte count returned by Write is ignored; only the error matters.
			name: "short write without error still succeeds",
			newWriter: func() hbWriter {
				return &hbFlushWriter{hbBase: hbBase{shortN: 3}}
			},
			want:        true,
			wantBody:    wantFrame,
			wantWrites:  1,
			wantFlushes: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			w := tt.newWriter()

			got := writeHeartbeat(w, http.NewResponseController(w), zap.NewNop())

			if got != tt.want {
				t.Errorf("writeHeartbeat() = %v, want %v", got, tt.want)
			}

			if body := w.written(); body != tt.wantBody {
				t.Errorf("written body = %q, want %q", body, tt.wantBody)
			}

			if n := w.writes(); n != tt.wantWrites {
				t.Errorf("Write called %d times, want %d", n, tt.wantWrites)
			}

			if n := w.flushes(); n != tt.wantFlushes {
				t.Errorf("Flush called %d times, want %d", n, tt.wantFlushes)
			}
		})
	}
}

// errFrameWrite and errFrameFlush stand in for the "client went away" errors
// the real transport produces.
var (
	errFrameWrite = errors.New("connection reset by peer")
	errFrameFlush = errors.New("flush failed")
)

// eventFrameRecorder records every Write and Flush the function performs, in
// order, so tests can assert both the exact bytes and that the frame is fully
// written before it is flushed.
type eventFrameRecorder struct {
	header   http.Header
	writes   [][]byte
	calls    []string
	writeErr error
	flushErr error
}

func newEventFrameRecorder() *eventFrameRecorder {
	return &eventFrameRecorder{header: make(http.Header)}
}

func (r *eventFrameRecorder) Header() http.Header { return r.header }

func (r *eventFrameRecorder) WriteHeader(int) {}

func (r *eventFrameRecorder) Write(p []byte) (int, error) {
	r.calls = append(r.calls, "write")

	r.writes = append(r.writes, bytes.Clone(p))
	if r.writeErr != nil {
		return 0, r.writeErr
	}

	return len(p), nil
}

// frame returns the single frame the function wrote, failing the test if it did
// not write exactly one.
func (r *eventFrameRecorder) frame(t *testing.T) string {
	t.Helper()

	if len(r.writes) != 1 {
		t.Fatalf("Write calls = %d, want exactly 1 (frame must be written in one call): %q", len(r.writes), r.writes)
	}

	return string(r.writes[0])
}

// eventFrameFlushingWriter flushes successfully.
type eventFrameFlushingWriter struct{ *eventFrameRecorder }

func (w eventFrameFlushingWriter) Flush() { w.calls = append(w.calls, "flush") }

// eventFrameFailingFlushWriter reports a flush error. http.ResponseController
// prefers FlushError over Flush, which is the only way to surface a failure.
type eventFrameFailingFlushWriter struct{ *eventFrameRecorder }

func (w eventFrameFailingFlushWriter) FlushError() error {
	w.calls = append(w.calls, "flush")

	return w.flushErr
}

// eventFrameUnflushableWriter implements neither Flush nor Unwrap, so
// http.ResponseController.Flush fails with http.ErrNotSupported.
type eventFrameUnflushableWriter struct{ *eventFrameRecorder }

// newEventFrameLogger returns a logger plus the entries it captured.
func newEventFrameLogger() (*zap.Logger, *observer.ObservedLogs) {
	core, logs := observer.New(zapcore.DebugLevel)

	return zap.New(core), logs
}

func TestWriteEventFrameWritesSSEFrame(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		event events.Event
		want  string
	}{
		{
			name:  "zero value omits list id",
			event: events.Event{},
			want:  "data: {\"type\":\"\"}\n\n",
		},
		{
			name:  "type only",
			event: events.Event{Type: "lists"},
			want:  "data: {\"type\":\"lists\"}\n\n",
		},
		{
			name:  "type and list id",
			event: events.Event{Type: "items", ListID: "abc"},
			want:  "data: {\"type\":\"items\",\"listId\":\"abc\"}\n\n",
		},
		{
			name:  "list id without type still emits type",
			event: events.Event{ListID: "abc"},
			want:  "data: {\"type\":\"\",\"listId\":\"abc\"}\n\n",
		},
		{
			name:  "json significant characters are escaped onto one line",
			event: events.Event{Type: "it\"ems", ListID: "a\nb\tc\\d"},
			want:  "data: {\"type\":\"it\\\"ems\",\"listId\":\"a\\nb\\tc\\\\d\"}\n\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rec := newEventFrameRecorder()
			writer := eventFrameFlushingWriter{rec}
			log, _ := newEventFrameLogger()

			ok := writeEventFrame(writer, http.NewResponseController(writer), log, tt.event)
			if !ok {
				t.Fatalf("writeEventFrame() = false, want true (stream should stay usable)")
			}

			got := rec.frame(t)
			if got != tt.want {
				t.Errorf("frame = %q, want %q", got, tt.want)
			}

			// The frame must be written first and flushed afterwards, so the
			// client never sees a partial event.
			if want := []string{"write", "flush"}; !slices.Equal(rec.calls, want) {
				t.Errorf("call order = %v, want %v", rec.calls, want)
			}

			// Exactly two newlines: the blank-line terminator. Any additional
			// newline would split the payload across SSE lines.
			if n := strings.Count(got, "\n"); n != 2 {
				t.Errorf("newline count = %d, want 2 (payload must stay on one data line)", n)
			}

			payload, found := strings.CutPrefix(got, "data: ")
			if !found {
				t.Fatalf("frame %q does not start with %q", got, "data: ")
			}

			payload = strings.TrimSuffix(payload, "\n\n")

			var roundTrip events.Event
			if err := json.Unmarshal([]byte(payload), &roundTrip); err != nil {
				t.Fatalf("json.Unmarshal(%q) failed: %v", payload, err)
			}

			if roundTrip != tt.event {
				t.Errorf("round-tripped event = %+v, want %+v", roundTrip, tt.event)
			}
		})
	}
}

func TestWriteEventFrameEndsStreamOnFailure(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		newWriter func(rec *eventFrameRecorder) http.ResponseWriter
		writeErr  error
		flushErr  error
		wantCalls []string
		wantFrame bool // whether the frame bytes reached the writer
	}{
		{
			name: "write failure skips the flush",
			newWriter: func(rec *eventFrameRecorder) http.ResponseWriter {
				return eventFrameFlushingWriter{rec}
			},
			writeErr:  errFrameWrite,
			wantCalls: []string{"write"},
			wantFrame: true,
		},
		{
			name: "flush failure after the frame was written",
			newWriter: func(rec *eventFrameRecorder) http.ResponseWriter {
				return eventFrameFailingFlushWriter{rec}
			},
			flushErr:  errFrameFlush,
			wantCalls: []string{"write", "flush"},
			wantFrame: true,
		},
		{
			name: "writer that cannot be flushed at all",
			newWriter: func(rec *eventFrameRecorder) http.ResponseWriter {
				return eventFrameUnflushableWriter{rec}
			},
			wantCalls: []string{"write"},
			wantFrame: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rec := newEventFrameRecorder()
			rec.writeErr = tt.writeErr
			rec.flushErr = tt.flushErr
			writer := tt.newWriter(rec)
			log, logs := newEventFrameLogger()

			ok := writeEventFrame(writer, http.NewResponseController(writer), log, events.Event{Type: "items", ListID: "abc"})
			if ok {
				t.Errorf("writeEventFrame() = true, want false (stream should end)")
			}

			if !slices.Equal(rec.calls, tt.wantCalls) {
				t.Errorf("call order = %v, want %v", rec.calls, tt.wantCalls)
			}

			if tt.wantFrame {
				if got, want := rec.frame(t), "data: {\"type\":\"items\",\"listId\":\"abc\"}\n\n"; got != want {
					t.Errorf("frame = %q, want %q", got, want)
				}
			}

			// A disconnected client is routine, not an application error.
			if logs.Len() == 0 {
				t.Errorf("no log entry recorded, want the failure logged at debug level")
			}

			for _, entry := range logs.All() {
				if entry.Level != zapcore.DebugLevel {
					t.Errorf("log entry %q at level %v, want %v", entry.Message, entry.Level, zapcore.DebugLevel)
				}
			}
		})
	}
}

// heartbeatRecorder is a minimal http.ResponseWriter that records everything
// written to it. It deliberately does NOT implement http.Flusher or
// FlushError, so an http.ResponseController wrapping it fails to flush.
type heartbeatRecorder struct {
	header     http.Header
	body       strings.Builder
	writeErr   error
	flushCalls int
}

func (r *heartbeatRecorder) Header() http.Header {
	if r.header == nil {
		r.header = make(http.Header)
	}

	return r.header
}

func (r *heartbeatRecorder) Write(p []byte) (int, error) {
	if r.writeErr != nil {
		return 0, r.writeErr
	}

	return r.body.Write(p)
}

func (r *heartbeatRecorder) WriteHeader(int) {}

// flushableRecorder adds a recording FlushError implementation, which
// http.ResponseController prefers over http.Flusher.
type flushableRecorder struct {
	*heartbeatRecorder

	flushErr error
}

func (f *flushableRecorder) FlushError() error {
	f.flushCalls++

	return f.flushErr
}

// errHeartbeatWriteBroken and errHeartbeatFlushFailed stand in for the write
// and flush failures TestWriteHeartbeatStreamContract exercises below.
var (
	errHeartbeatWriteBroken = errors.New("broken pipe")
	errHeartbeatFlushFailed = errors.New("flush failed")
)

func TestWriteHeartbeatStreamContract(t *testing.T) {
	t.Parallel()

	errWrite := errHeartbeatWriteBroken
	errFlush := errHeartbeatFlushFailed

	tests := []struct {
		name           string
		flushable      bool
		writeErr       error
		flushErr       error
		want           bool
		wantBody       string
		wantFlushCalls int
	}{
		{
			name:           "write and flush succeed",
			flushable:      true,
			want:           true,
			wantBody:       ": ping\n\n",
			wantFlushCalls: 1,
		},
		{
			name:           "write error short-circuits flush",
			flushable:      true,
			writeErr:       errWrite,
			want:           false,
			wantBody:       "",
			wantFlushCalls: 0,
		},
		{
			name:           "flush unsupported by writer",
			flushable:      false,
			want:           false,
			wantBody:       ": ping\n\n",
			wantFlushCalls: 0,
		},
		{
			name:           "flush error after successful write",
			flushable:      true,
			flushErr:       errFlush,
			want:           false,
			wantBody:       ": ping\n\n",
			wantFlushCalls: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rec := &heartbeatRecorder{writeErr: tt.writeErr}

			var writer http.ResponseWriter = rec
			if tt.flushable {
				writer = &flushableRecorder{heartbeatRecorder: rec, flushErr: tt.flushErr}
			}

			got := writeHeartbeat(writer, http.NewResponseController(writer), zap.NewNop())
			if got != tt.want {
				t.Errorf("writeHeartbeat() = %v, want %v", got, tt.want)
			}

			if body := rec.body.String(); body != tt.wantBody {
				t.Errorf("response body = %q, want %q", body, tt.wantBody)
			}

			if rec.flushCalls != tt.wantFlushCalls {
				t.Errorf("flush calls = %d, want %d", rec.flushCalls, tt.wantFlushCalls)
			}
		})
	}
}

// TestWriteHeartbeatWithResponseRecorder covers the happy path against the
// stdlib httptest.ResponseRecorder, which implements http.Flusher.
func TestWriteHeartbeatWithResponseRecorder(t *testing.T) {
	t.Parallel()

	recorder := httptest.NewRecorder()

	if got := writeHeartbeat(recorder, http.NewResponseController(recorder), zap.NewNop()); !got {
		t.Fatalf("writeHeartbeat() = false, want true")
	}

	if body := recorder.Body.String(); body != ": ping\n\n" {
		t.Errorf("response body = %q, want %q", body, ": ping\n\n")
	}

	if !recorder.Flushed {
		t.Error("recorder was not flushed, want flushed")
	}
}

// wefRecorder is a minimal http.ResponseWriter that records the payload of
// every successful Write and interleaves "write"/"flush" markers, so a test
// can assert both the exact frame bytes and that the frame was fully written
// before it was flushed.
type wefRecorder struct {
	hdr      http.Header
	payloads [][]byte
	calls    []string
	writeErr error
	flushErr error
}

func newWEFRecorder() *wefRecorder {
	return &wefRecorder{hdr: make(http.Header)}
}

func (r *wefRecorder) Header() http.Header { return r.hdr }

func (r *wefRecorder) WriteHeader(int) {}

func (r *wefRecorder) Write(p []byte) (int, error) {
	r.calls = append(r.calls, "write")
	if r.writeErr != nil {
		return 0, r.writeErr
	}

	r.payloads = append(r.payloads, bytes.Clone(p))

	return len(p), nil
}

// frame returns the one and only frame written, failing the test when the
// function split it across calls: an SSE frame must reach the client whole.
func (r *wefRecorder) frame(t *testing.T) string {
	t.Helper()

	if len(r.payloads) != 1 {
		t.Fatalf("recorded %d write payloads, want exactly 1 (frame must be a single write): %q", len(r.payloads), r.payloads)
	}

	return string(r.payloads[0])
}

// wefFlushable flushes through FlushError, which http.ResponseController
// prefers over Flush and which is the only way to surface a flush failure.
type wefFlushable struct{ *wefRecorder }

func (w wefFlushable) FlushError() error {
	w.calls = append(w.calls, "flush")

	return w.flushErr
}

// wefUnflushable implements neither FlushError, Flush, nor Unwrap, so
// http.ResponseController.Flush reports http.ErrNotSupported.
type wefUnflushable struct{ *wefRecorder }

// errEventFrameConnReset stands in for the transport error
// TestWriteEventFrameStreamContract exercises below.
var errEventFrameConnReset = errors.New("connection reset by peer")

func TestWriteEventFrameStreamContract(t *testing.T) {
	t.Parallel()

	errGone := errEventFrameConnReset

	tests := []struct {
		name        string
		event       events.Event
		writeErr    error
		flushErr    error
		unflushable bool // writer supports no flushing at all
		want        bool
		wantFrame   string // "" means nothing reached the writer
		wantCalls   []string
		wantLogs    int
	}{
		{
			// ListID is omitempty, so a zero event carries only "type".
			name:      "zero value event omits list id",
			event:     events.Event{},
			want:      true,
			wantFrame: "data: {\"type\":\"\"}\n\n",
			wantCalls: []string{"write", "flush"},
		},
		{
			name:      "both fields set",
			event:     events.Event{Type: "items", ListID: "abc"},
			want:      true,
			wantFrame: "data: {\"type\":\"items\",\"listId\":\"abc\"}\n\n",
			wantCalls: []string{"write", "flush"},
		},
		{
			name:      "write failure ends stream and skips flush",
			event:     events.Event{Type: "lists"},
			writeErr:  errGone,
			want:      false,
			wantCalls: []string{"write"},
			wantLogs:  1,
		},
		{
			// The bytes already landed; only the return value tells the loop to stop.
			name:      "flush failure ends stream after frame landed",
			event:     events.Event{Type: "lists"},
			flushErr:  errGone,
			want:      false,
			wantFrame: "data: {\"type\":\"lists\"}\n\n",
			wantCalls: []string{"write", "flush"},
			wantLogs:  1,
		},
		{
			name:        "writer without flush support ends stream",
			event:       events.Event{Type: "lists"},
			unflushable: true,
			want:        false,
			wantFrame:   "data: {\"type\":\"lists\"}\n\n",
			wantCalls:   []string{"write"},
			wantLogs:    1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rec := newWEFRecorder()
			rec.writeErr = tt.writeErr
			rec.flushErr = tt.flushErr

			var writer http.ResponseWriter = wefFlushable{rec}
			if tt.unflushable {
				writer = wefUnflushable{rec}
			}

			core, logs := observer.New(zapcore.DebugLevel)

			got := writeEventFrame(writer, http.NewResponseController(writer), zap.New(core), tt.event)
			if got != tt.want {
				t.Errorf("writeEventFrame() = %v, want %v", got, tt.want)
			}

			if !slices.Equal(rec.calls, tt.wantCalls) {
				t.Errorf("call sequence = %v, want %v", rec.calls, tt.wantCalls)
			}

			if tt.wantFrame == "" {
				if len(rec.payloads) != 0 {
					t.Errorf("writer received %q, want nothing", rec.payloads)
				}
			} else if frame := rec.frame(t); frame != tt.wantFrame {
				t.Errorf("frame = %q, want %q", frame, tt.wantFrame)
			}

			if logs.Len() != tt.wantLogs {
				t.Errorf("log entries = %d, want %d", logs.Len(), tt.wantLogs)
			}
			// A vanished client is routine bookkeeping, not an application error.
			for _, entry := range logs.All() {
				if entry.Level != zapcore.DebugLevel {
					t.Errorf("log entry %q at level %v, want %v", entry.Message, entry.Level, zapcore.DebugLevel)
				}
			}
		})
	}
}
