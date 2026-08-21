// SPDX-License-Identifier: CC0-1.0

// Package problem implements RFC 9457 (Problem Details for HTTP APIs) as the
// uniform error format for the splitkauf API. It owns a registry of the problem
// types the API emits; every type resolves to a self-hosted HTML explanation
// page at /problems/{slug} (about:blank is never used).
package problem

import (
	"encoding/json"
	"net/http"

	"go.uber.org/zap"

	"github.com/m4schini/splitkauf/telemetry"
)

// ContentType is the RFC 9457 media type for problem responses.
const ContentType = "application/problem+json"

// Type is a registry entry describing one problem type: its URL slug, human
// title (the HTTP reason phrase), HTTP status code, and a one-paragraph
// description rendered on the explanation page.
type Type struct {
	Slug        string
	Title       string
	Status      int
	Description string
}

// URI returns the path-absolute type URI ("/problems/{slug}") emitted in the
// problem body's "type" member and resolvable to the explanation page.
func (t Type) URI() string {
	return "/problems/" + t.Slug
}

// The registered problem types. Each covers one of the API's error surfaces
// and has a self-hosted explanation page.
var (
	// Validation covers request-validation failures and parameter binding.
	Validation = Type{
		Slug:   "validation",
		Title:  http.StatusText(http.StatusBadRequest),
		Status: http.StatusBadRequest,
		Description: "The request did not satisfy the API's validation rules. " +
			"One or more parameters or body fields are missing, malformed, or " +
			"otherwise invalid. Inspect the errors member for the specific " +
			"fields that failed and correct the request before retrying.",
	}
	// NotFound covers unknown routes under /api/v1.
	NotFound = Type{
		Slug:   "not-found",
		Title:  http.StatusText(http.StatusNotFound),
		Status: http.StatusNotFound,
		Description: "No resource exists at the requested path. The URL may be " +
			"misspelled, the resource may have been removed, or the endpoint " +
			"may not be part of this API version.",
	}
	// MethodNotAllowed covers unsupported methods on a known path.
	MethodNotAllowed = Type{
		Slug:   "method-not-allowed",
		Title:  http.StatusText(http.StatusMethodNotAllowed),
		Status: http.StatusMethodNotAllowed,
		Description: "The requested resource exists but does not support this " +
			"HTTP method. Check the API documentation for the methods this " +
			"endpoint accepts.",
	}
	// Internal covers recovered panics and unexpected faults.
	Internal = Type{
		Slug:   "internal",
		Title:  http.StatusText(http.StatusInternalServerError),
		Status: http.StatusInternalServerError,
		Description: "The server encountered an unexpected condition that " +
			"prevented it from fulfilling the request. This is a fault on the " +
			"server side; no details are exposed. Retrying later may succeed.",
	}
	// Unauthorized covers requests to authenticated endpoints made without a
	// valid session (no session, or an expired/revoked one).
	Unauthorized = Type{
		Slug:   "unauthorized",
		Title:  http.StatusText(http.StatusUnauthorized),
		Status: http.StatusUnauthorized,
		Description: "The request was not accompanied by a valid authenticated " +
			"session. The session may be missing, expired, or revoked. Start a " +
			"new sign-in via the login endpoint before retrying.",
	}
	// Unavailable covers dependencies the request needs that are temporarily
	// down, notably the identity provider during sign-in.
	Unavailable = Type{
		Slug:   "unavailable",
		Title:  http.StatusText(http.StatusServiceUnavailable),
		Status: http.StatusServiceUnavailable,
		Description: "The server could not complete the request because a " +
			"dependency it relies on is temporarily unavailable, such as the " +
			"identity provider during sign-in. Retrying later may succeed.",
	}
	// PayloadTooLarge covers request bodies exceeding the API's size cap,
	// whether rejected up front from a declared Content-Length or caught by
	// the http.MaxBytesReader backstop while reading the body.
	PayloadTooLarge = Type{
		Slug:   "payload-too-large",
		Title:  http.StatusText(http.StatusRequestEntityTooLarge),
		Status: http.StatusRequestEntityTooLarge,
		Description: "The request body exceeds the size limit the API enforces " +
			"for every request. Reduce the size of the request body and retry.",
	}
)

// Types returns every registered problem type. It drives the explanation pages
// and the registry drift test (every emitted type must have a page).
func Types() []Type {
	return []Type{Validation, Unauthorized, NotFound, MethodNotAllowed, Internal, Unavailable, PayloadTooLarge}
}

// FromStatus maps an HTTP status code to its registered problem type: 400 →
// Validation, 401 → Unauthorized, 404 → NotFound, 405 → MethodNotAllowed, 413
// → PayloadTooLarge, 503 → Unavailable, and anything else → Internal.
func FromStatus(status int) Type {
	switch status {
	case http.StatusBadRequest:
		return Validation
	case http.StatusUnauthorized:
		return Unauthorized
	case http.StatusNotFound:
		return NotFound
	case http.StatusMethodNotAllowed:
		return MethodNotAllowed
	case http.StatusRequestEntityTooLarge:
		return PayloadTooLarge
	case http.StatusServiceUnavailable:
		return Unavailable
	default:
		return Internal
	}
}

// FieldError is the RFC 9457 canonical validation-error member: a per-field
// detail with a pointer identifying the offending field.
type FieldError struct {
	Detail  string `json:"detail"`
	Pointer string `json:"pointer,omitempty"`
}

// Problem is an RFC 9457 problem details object: the five standard members plus
// the errors extension for field-level validation failures. Empty members are
// omitted from the JSON encoding.
type Problem struct {
	Type     string       `json:"type,omitempty"`
	Title    string       `json:"title,omitempty"`
	Status   int          `json:"status,omitempty"`
	Detail   string       `json:"detail,omitempty"`
	Instance string       `json:"instance,omitempty"`
	Errors   []FieldError `json:"errors,omitempty"`
}

// New builds a Problem for the given type, filling in "type" (the type URI),
// "title", and "status" from the registry and using detail as the "detail"
// member.
func New(t Type, detail string) Problem {
	return Problem{
		Type:   t.URI(),
		Title:  t.Title,
		Status: t.Status,
		Detail: detail,
	}
}

// Write serialises p as an application/problem+json response. It sets the
// Content-Type header and the HTTP status from p.Status (defaulting to 500 if
// unset), defaults the instance member to the request path, and encodes the
// body.
func Write(w http.ResponseWriter, r *http.Request, p Problem) {
	if p.Instance == "" && r != nil {
		p.Instance = r.URL.Path
	}
	status := p.Status
	if status == 0 {
		status = http.StatusInternalServerError
	}

	w.Header().Set("Content-Type", ContentType)
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(p); err != nil {
		telemetry.Logger("api", "problem").Error("encoding problem response", zap.Error(err))
	}
}
