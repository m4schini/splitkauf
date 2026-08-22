// SPDX-License-Identifier: CC0-1.0

package auth

import (
	"net/url"
	"strings"
)

// defaultReturnTo is where a sign-in returns when no safe return_to is given.
const defaultReturnTo = "/"

// safeReturnTo validates a caller-supplied post-login redirect target against an
// open-redirect allowlist and returns a safe destination. Only same-origin,
// relative *paths* are accepted; everything else falls back to defaultReturnTo.
//
// Rejected: absolute URLs ("http://evil", "https://evil"), scheme-relative URLs
// ("//evil", and the backslash variants browsers normalise like "/\evil" or
// "\\evil"), anything that parses to a non-empty scheme or host, and values not
// beginning with a single "/". This prevents the login/callback endpoints from
// being used as an open redirect.
func safeReturnTo(raw string) string {
	if raw == "" {
		return defaultReturnTo
	}
	// Normalise backslashes to forward slashes first: some browsers treat
	// "\" as "/", so "/\evil.com" or "\\evil.com" would otherwise escape the
	// origin. After this, scheme-relative checks below catch them.
	normalised := strings.ReplaceAll(raw, "\\", "/")

	// Must be a rooted path and not scheme-relative ("//host").
	if !strings.HasPrefix(normalised, "/") || strings.HasPrefix(normalised, "//") {
		return defaultReturnTo
	}

	// Reject any parse error, or any value carrying a scheme or host: a valid
	// relative path has neither.
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "" || parsed.Host != "" {
		return defaultReturnTo
	}

	// Re-check the parsed path shape (guards odd inputs like "/\\evil").
	if !strings.HasPrefix(parsed.Path, "/") || strings.HasPrefix(parsed.Path, "//") {
		return defaultReturnTo
	}

	// Rebuild from the parsed path (and query) so only the safe components
	// survive; drop any fragment/host that slipped through.
	out := parsed.Path
	if parsed.RawQuery != "" {
		out += "?" + parsed.RawQuery
	}

	return out
}
