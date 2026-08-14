// SPDX-License-Identifier: CC0-1.0

package metrics

import (
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
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		httpInFlight.Inc()
		defer httpInFlight.Dec()

		rw := &recordingWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rw, r)

		route := ""
		if rc := chi.RouteContext(r.Context()); rc != nil {
			route = rc.RoutePattern()
		}
		// Fall back to a stable label when the request did not match a route
		// (e.g. 404), so we do not blow up cardinality with raw paths.
		if route == "" {
			route = "unmatched"
		}

		status := strconv.Itoa(rw.status)
		httpRequestsTotal.WithLabelValues(r.Method, route, status).Inc()
		httpRequestDuration.WithLabelValues(r.Method, route, status).Observe(time.Since(start).Seconds())
		httpResponseSize.WithLabelValues(r.Method, route).Observe(float64(rw.bytes))
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
	n, err := rw.ResponseWriter.Write(b)
	rw.bytes += n
	return n, err
}

// Flush forwards to the underlying ResponseWriter when it supports
// http.Flusher, so streaming/SSE responses are not broken by this wrapper.
func (rw *recordingWriter) Flush() {
	if f, ok := rw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
