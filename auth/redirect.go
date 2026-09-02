// SPDX-License-Identifier: CC0-1.0

package auth

import (
	"net/url"
	"strings"
	"unicode"
)

// defaultReturnTo is where a sign-in returns when no safe return_to is given.
const defaultReturnTo = "/"

// safeReturnTo validates a caller-supplied post-login redirect target against an
// open-redirect allowlist and returns a safe destination. Only same-origin,
// relative *paths* are accepted; everything else falls back to defaultReturnTo.
//
// Rejected: absolute URLs ("http://evil", "https://evil"), scheme-relative URLs
// ("//evil", and the backslash variants browsers normalise like "/\evil" or
// "\\evil"), their percent-encoded forms ("/%5Cevil", "/%09/evil": browsers
// treat a decoded "\" as "/" and strip tabs and newlines), anything that parses
// to a non-empty scheme or host, and values not beginning with a single "/".
// This prevents the login/callback endpoints from being used as an open
// redirect.
func safeReturnTo(raw string) string {
	if raw == "" {
		return defaultReturnTo
	}
	// Normalise backslashes to forward slashes first: some browsers treat
	// "\" as "/", so "/\evil.com" or "\\evil.com" would otherwise escape the
	// origin. The rooted-path check then catches them as scheme-relative.
	if !isRootedSinglePath(strings.ReplaceAll(raw, "\\", "/")) {
		return defaultReturnTo
	}

	// Reject any parse error, or any value carrying a scheme or host: a valid
	// relative path has neither.
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "" || parsed.Host != "" {
		return defaultReturnTo
	}

	// Re-check the decoded path shape (guards odd inputs like "/\\evil") and
	// reject decoded backslashes and control characters.
	if !isRootedSinglePath(parsed.Path) || hasUnsafePathChars(parsed.Path) {
		return defaultReturnTo
	}

	// Rebuild from the escaped path (and query) so only the safe components
	// survive, still percent-encoded; drop any fragment/host that slipped
	// through.
	out := parsed.EscapedPath()
	if strings.HasPrefix(out, "//") {
		return defaultReturnTo
	}
	if parsed.RawQuery != "" {
		out += "?" + parsed.RawQuery
	}

	return out
}

// isRootedSinglePath reports whether p begins with exactly one "/", i.e. it is
// a rooted path and not a scheme-relative "//host" reference.
func isRootedSinglePath(p string) bool {
	return strings.HasPrefix(p, "/") && !strings.HasPrefix(p, "//")
}

// hasUnsafePathChars reports whether the decoded path contains a backslash or
// a control character. "/%5Cevil.com" or "/%09/evil.com" would otherwise be
// emitted as "/\evil.com" or "/\t/evil.com", which browsers normalise to the
// scheme-relative "//evil.com".
func hasUnsafePathChars(path string) bool {
	for _, r := range path {
		if r == '\\' || unicode.IsControl(r) {
			return true
		}
	}
	return false
}
