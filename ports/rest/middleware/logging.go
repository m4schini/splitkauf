// SPDX-License-Identifier: CC0-1.0

package middleware

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/m4schini/splitkauf/telemetry"
	"go.uber.org/zap"
)

type responseWriter struct {
	http.ResponseWriter
	status int
}

func (rw *responseWriter) WriteHeader(status int) {
	rw.status = status
	rw.ResponseWriter.WriteHeader(status)
}

func Logging(next http.Handler) http.Handler {
	log := telemetry.Logger("api")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &responseWriter{ResponseWriter: w, status: http.StatusOK}

		next.ServeHTTP(rw, r)

		route := ""
		if rc := chi.RouteContext(r.Context()); rc != nil {
			route = rc.RoutePattern()
		}

		log.Info("request handled",
			zap.String("method", r.Method),
			zap.String("path", r.URL.Path),
			zap.String("route", route),
			zap.String("remote_addr", r.RemoteAddr),
			zap.Int("status", rw.status),
			zap.Duration("duration", time.Since(start)),
		)
	})
}
