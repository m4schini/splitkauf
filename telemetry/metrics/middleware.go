// SPDX-License-Identifier: CC0-1.0

package metrics

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
)

// Middleware returns chi middleware that records request count, duration,
// response size and in-flight gauge for every handled request. Route labels
// come from chi.RouteContext so cardinality stays bounded to the registered
// path patterns rather than the raw URL.
func Middleware(next http.Handler) http.Handler {
	ensureRegistered()

	return http.HandlerFunc(func(respWriter http.ResponseWriter, req *http.Request) {
		start := time.Now()

		httpInFlight.Inc()
		defer httpInFlight.Dec()

		recorder := &recordingWriter{ResponseWriter: respWriter, status: http.StatusOK, bytes: 0}
		next.ServeHTTP(recorder, req)

		route := ""
		if routeCtx := chi.RouteContext(req.Context()); routeCtx != nil {
			route = routeCtx.RoutePattern()
		}
		// Fall back to a stable label when the request did not match a route
		// (e.g. 404), so we do not blow up cardinality with raw paths.
		if route == "" {
			route = "unmatched"
		}

		status := strconv.Itoa(recorder.status)
		httpRequestsTotal.WithLabelValues(req.Method, route, status).Inc()
		httpRequestDuration.WithLabelValues(req.Method, route, status).Observe(time.Since(start).Seconds())
		httpResponseSize.WithLabelValues(req.Method, route).Observe(float64(recorder.bytes))
	})
}

type recordingWriter struct {
	http.ResponseWriter

	status int
	bytes  int
}

func (rw *recordingWriter) WriteHeader(status int) {
	rw.status = status
	rw.ResponseWriter.WriteHeader(status)
}

func (rw *recordingWriter) Write(b []byte) (int, error) {
	written, err := rw.ResponseWriter.Write(b)
	rw.bytes += written

	if err != nil {
		return written, fmt.Errorf("writing response body: %w", err)
	}

	return written, nil
}

// Flush forwards to the underlying ResponseWriter when it supports
// http.Flusher, so streaming/SSE responses are not broken by this wrapper.
func (rw *recordingWriter) Flush() {
	if f, ok := rw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
