// SPDX-License-Identifier: CC0-1.0

// Package middleware provides the shared HTTP middleware (request logging,
// panic recovery, body-size limiting) used by the REST port.
package middleware

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"

	"github.com/m4schini/splitkauf/telemetry"
)

type responseWriter struct {
	http.ResponseWriter

	status int
}

func (rw *responseWriter) WriteHeader(status int) {
	rw.status = status
	rw.ResponseWriter.WriteHeader(status)
}

// Logging wraps next, logging one structured line per request: method,
// path, matched route, status, remote address and duration.
func Logging(next http.Handler) http.Handler {
	log := telemetry.Logger("api")

	return http.HandlerFunc(func(writer http.ResponseWriter, req *http.Request) {
		start := time.Now()
		wrapped := &responseWriter{ResponseWriter: writer, status: http.StatusOK}

		next.ServeHTTP(wrapped, req)

		route := ""
		if routeCtx := chi.RouteContext(req.Context()); routeCtx != nil {
			route = routeCtx.RoutePattern()
		}

		log.Info("request handled",
			zap.String("method", req.Method),
			zap.String("path", req.URL.Path),
			zap.String("route", route),
			zap.String("remote_addr", req.RemoteAddr),
			zap.Int("status", wrapped.status),
			zap.Duration("duration", time.Since(start)),
		)
	})
}
