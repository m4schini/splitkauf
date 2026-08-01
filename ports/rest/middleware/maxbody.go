// SPDX-License-Identifier: TODO

package middleware

import (
	"net/http"

	"github.com/m4schini/splitkauf/ports/rest/problem"
)

// MaxBody is chi middleware that caps every request body at limit bytes. It
// enforces the cap in two layers:
//
//  1. If the request declares a Content-Length greater than limit, it writes
//     an RFC 9457 PayloadTooLarge problem response immediately and does not
//     call next — the body is never read.
//  2. Otherwise it installs http.MaxBytesReader on r.Body as a backstop
//     before delegating to next, so chunked or streamed requests without a
//     declared Content-Length are still capped once the handler reads the
//     body.
//
// Caveat (see M5 plan Key Decision 2): a body that exceeds the cap WITHOUT a
// declared Content-Length is only caught by the backstop while it is read.
// By that point the OpenAPI request-validator has already started decoding
// the body, so the resulting error surfaces as a 400 Validation problem
// rather than a 413 PayloadTooLarge problem. This is acceptable: the cap
// still held (the oversized body is rejected either way), only the reported
// status differs depending on whether the client declared its size.
func MaxBody(limit int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.ContentLength > limit {
				problem.Write(w, r, problem.New(problem.PayloadTooLarge, "request body exceeds the maximum allowed size"))
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, limit)
			next.ServeHTTP(w, r)
		})
	}
}
