// SPDX-License-Identifier: CC0-1.0

package middleware

import (
	"fmt"
	"net/http"
	"runtime/debug"

	"go.uber.org/zap"

	"github.com/m4schini/splitkauf/ports/rest/problem"
	"github.com/m4schini/splitkauf/telemetry"
)

// Recover is chi middleware that recovers from handler panics. It logs the
// recovered value with a stack trace at error level, then writes an RFC 9457
// Internal problem response. Panic details never leak into the response body
// (RFC 9457 §5). If the response has already started, it re-panics so the chi
// stack can abort the connection rather than corrupting a partial response.
func Recover(next http.Handler) http.Handler {
	log := telemetry.Logger("api")

	return http.HandlerFunc(func(writer http.ResponseWriter, req *http.Request) {
		wrapped := &recoverWriter{ResponseWriter: writer, wrote: false}

		defer func() {
			rec := recover()
			if rec == nil {
				return
			}

			log.Error("panic recovered",
				zap.String("method", req.Method),
				zap.String("path", req.URL.Path),
				zap.Any("panic", rec),
				zap.ByteString("stack", debug.Stack()),
			)
			// If the handler already wrote a response, we cannot safely emit a
			// problem body; re-panic so chi aborts the connection.
			if wrapped.wrote {
				panic(rec)
			}

			problem.Write(writer, req, problem.New(problem.Internal, problem.Internal.Description))
		}()

		next.ServeHTTP(wrapped, req)
	})
}

// recoverWriter tracks whether the response has started so Recover can decide
// whether a problem body can still be written.
type recoverWriter struct {
	http.ResponseWriter

	wrote bool
}

func (rw *recoverWriter) WriteHeader(status int) {
	rw.wrote = true
	rw.ResponseWriter.WriteHeader(status)
}

func (rw *recoverWriter) Write(b []byte) (int, error) {
	rw.wrote = true

	n, err := rw.ResponseWriter.Write(b)
	if err != nil {
		return n, fmt.Errorf("writing response: %w", err)
	}

	return n, nil
}

// Flush forwards to the underlying ResponseWriter when it supports
// http.Flusher, so streaming/SSE responses are not broken by this wrapper.
func (rw *recoverWriter) Flush() {
	if f, ok := rw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
