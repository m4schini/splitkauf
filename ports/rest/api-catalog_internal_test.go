// SPDX-License-Identifier: CC0-1.0

package rest

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// newBaseURLRequest builds a minimal server-side request with the given Host,
// optional TLS connection state and X-Forwarded-Proto header values.
func newBaseURLRequest(t *testing.T, host string, withTLS bool, protoValues []string) *http.Request {
	t.Helper()

	req := &http.Request{
		Method: http.MethodGet,
		URL:    &url.URL{Path: "/.well-known/api-catalog"},
		Host:   host,
		Header: make(http.Header),
	}

	for _, v := range protoValues {
		// Add (not Set) so the multi-value case keeps every value.
		req.Header.Add("X-Forwarded-Proto", v)
	}

	if withTLS {
		req.TLS = &tls.ConnectionState{}
	}

	return req
}

func TestRequestBaseURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		host string
		tls  bool
		// protoValues are added one by one as X-Forwarded-Proto headers.
		protoValues []string
		want        string
	}{
		{
			name: "plain http without forwarding headers",
			host: "example.com",
			want: "http://example.com",
		},
		{
			name: "direct tls connection",
			host: "example.com",
			tls:  true,
			want: "https://example.com",
		},
		{
			name:        "tls wins over x-forwarded-proto",
			host:        "example.com",
			tls:         true,
			protoValues: []string{"http"},
			want:        "https://example.com",
		},
		{
			name:        "x-forwarded-proto https on non-tls request",
			host:        "example.com",
			protoValues: []string{"https"},
			want:        "https://example.com",
		},
		{
			name:        "x-forwarded-proto is used verbatim without validation",
			host:        "example.com",
			protoValues: []string{"wss"},
			want:        "wss://example.com",
		},
		{
			name:        "x-forwarded-proto garbage value is not sanitized",
			host:        "example.com",
			protoValues: []string{"ht tp"},
			want:        "ht tp://example.com",
		},
		{
			name:        "empty x-forwarded-proto falls back to http",
			host:        "example.com",
			protoValues: []string{""},
			want:        "http://example.com",
		},
		{
			name:        "only the first x-forwarded-proto value is used",
			host:        "example.com",
			protoValues: []string{"https", "http"},
			want:        "https://example.com",
		},
		{
			name:        "comma joined x-forwarded-proto is used verbatim",
			host:        "example.com",
			protoValues: []string{"https,http"},
			want:        "https,http://example.com",
		},
		{
			name:        "host keeps its explicit port",
			host:        "example.com:8443",
			protoValues: []string{"https"},
			want:        "https://example.com:8443",
		},
		{
			name: "host with port on plain http",
			host: "127.0.0.1:8080",
			want: "http://127.0.0.1:8080",
		},
		{
			name: "empty host yields degenerate but non-panicking output",
			host: "",
			want: "http://",
		},
		{
			name:        "lowercase header name is canonicalized",
			host:        "example.com",
			protoValues: []string{"https"},
			want:        "https://example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := newBaseURLRequest(t, tt.host, tt.tls, tt.protoValues)

			if got := requestBaseURL(req); got != tt.want {
				t.Errorf("requestBaseURL() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestRequestBaseURLTLSStateOnlyCheckedForNilness pins that the scheme choice
// looks at whether req.TLS is a nil pointer, not at what the connection state
// contains: a zero-value, never-handshaked state still selects https, and only
// a nil pointer falls through to X-Forwarded-Proto. Hand-built requests (proxy
// shims, tests) can carry either.
func TestRequestBaseURLTLSStateOnlyCheckedForNilness(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		state *tls.ConnectionState
		want  string
	}{
		{
			name:  "nil connection state falls through to the forwarded proto",
			state: nil,
			want:  "gopher://example.com",
		},
		{
			name:  "zero value connection state selects https",
			state: &tls.ConnectionState{},
			want:  "https://example.com",
		},
		{
			name:  "unfinished handshake still selects https",
			state: &tls.ConnectionState{HandshakeComplete: false, Version: tls.VersionTLS12},
			want:  "https://example.com",
		},
		{
			name: "finished handshake selects https",
			state: &tls.ConnectionState{
				HandshakeComplete: true,
				Version:           tls.VersionTLS13,
				ServerName:        "example.com",
			},
			want: "https://example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// A proto value none of the fallbacks can produce, so a pass
			// proves which branch ran.
			req := newBaseURLRequest(t, "example.com", false, []string{"gopher"})
			req.TLS = tt.state

			if got := requestBaseURL(req); got != tt.want {
				t.Errorf("requestBaseURL() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestRequestBaseURLNilRequestPanics documents the single degenerate input:
// requestBaseURL dereferences the request to read req.TLS, so a nil request
// panics instead of returning a fallback string. net/http never hands the only
// caller a nil request, so this pins the contract rather than blessing the
// call.
func TestRequestBaseURLNilRequestPanics(t *testing.T) {
	t.Parallel()

	defer func() {
		if r := recover(); r == nil {
			t.Error("requestBaseURL(nil) returned normally, want a panic")
		}
	}()

	_ = requestBaseURL(nil)
}

// TestYAMLToJSON pins the YAML -> pretty-printed JSON conversion behind
// GET /openapi.json: two-space indentation, no trailing newline, and
// alphabetically sorted object keys (the intermediate value is an untyped
// map, so YAML source order is lost).
func TestYAMLToJSON(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		src  []byte
		want string
	}{
		{
			name: "nil input decodes to nil and marshals to null",
			src:  nil,
			want: "null",
		},
		{
			name: "empty input decodes to nil and marshals to null",
			src:  []byte(""),
			want: "null",
		},
		{
			name: "whitespace only",
			src:  []byte("   \n   \n  \n"),
			want: "null",
		},
		{
			name: "comments only",
			src:  []byte("# just a comment\n# and another\n"),
			want: "null",
		},
		{
			name: "keys are sorted alphabetically, not in source order",
			src:  []byte("zebra: 1\nalpha: 2\nmiddle: 3\n"),
			want: "{\n  \"alpha\": 2,\n  \"middle\": 3,\n  \"zebra\": 1\n}",
		},
		{
			name: "nested mappings use two spaces per level",
			src:  []byte("openapi: 3.0.0\ninfo:\n  title: splitkauf\n  version: \"1\"\n"),
			want: "{\n  \"info\": {\n    \"title\": \"splitkauf\",\n    \"version\": \"1\"\n  },\n  \"openapi\": \"3.0.0\"\n}",
		},
		{
			name: "sequence at the root",
			src:  []byte("- a\n- b\n"),
			want: "[\n  \"a\",\n  \"b\"\n]",
		},
		{
			name: "scalar number at the root",
			src:  []byte("42\n"),
			want: "42",
		},
		{
			name: "scalar string at the root",
			src:  []byte("hello\n"),
			want: "\"hello\"",
		},
		{
			name: "boolean and null scalars",
			src:  []byte("enabled: true\nmissing: null\n"),
			want: "{\n  \"enabled\": true,\n  \"missing\": null\n}",
		},
		{
			name: "json input round-trips because json is a yaml subset",
			src:  []byte(`{"b": 2, "a": {"c": [1, 2]}}`),
			want: "{\n  \"a\": {\n    \"c\": [\n      1,\n      2\n    ]\n  },\n  \"b\": 2\n}",
		},
		{
			name: "anchors and aliases are expanded",
			src:  []byte("a: &ref hello\nb: *ref\n"),
			want: "{\n  \"a\": \"hello\",\n  \"b\": \"hello\"\n}",
		},
		{
			name: "merge keys are expanded",
			src:  []byte("base: &base\n  a: 1\nderived:\n  <<: *base\n  b: 2\n"),
			want: "{\n  \"base\": {\n    \"a\": 1\n  },\n  \"derived\": {\n    \"a\": 1,\n    \"b\": 2\n  }\n}",
		},
		{
			name: "only the first document of a multi-document stream is decoded",
			src:  []byte("first: 1\n---\nsecond: 2\n---\nthird: 3\n"),
			want: "{\n  \"first\": 1\n}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := yamlToJSON(tt.src)
			if err != nil {
				t.Fatalf("yamlToJSON(%q) returned error: %v", tt.src, err)
			}

			if string(got) != tt.want {
				t.Errorf("yamlToJSON(%q) =\n%s\nwant:\n%s", tt.src, got, tt.want)
			}

			if strings.HasSuffix(string(got), "\n") {
				t.Errorf("yamlToJSON(%q) output ends with a newline: %q", tt.src, got)
			}

			if !json.Valid(got) {
				t.Errorf("yamlToJSON(%q) produced invalid JSON: %s", tt.src, got)
			}
		})
	}
}

// TestYAMLToJSONErrors pins the two failure paths, both of which make
// openAPISpecJSONHandler answer with a 500.
func TestYAMLToJSONErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		src        []byte
		wantPrefix string
	}{
		{
			name:       "unclosed flow sequence",
			src:        []byte("key: [unclosed\n"),
			wantPrefix: "unmarshalling openapi spec yaml:",
		},
		{
			name:       "tab indentation",
			src:        []byte("key:\n\tnested: 1\n"),
			wantPrefix: "unmarshalling openapi spec yaml:",
		},
		{
			name:       "unclosed quote",
			src:        []byte("key: \"unterminated\n"),
			wantPrefix: "unmarshalling openapi spec yaml:",
		},
		{
			name:       "nan float is not json encodable",
			src:        []byte("value: .nan\n"),
			wantPrefix: "marshalling openapi spec to json:",
		},
		{
			name:       "inf float is not json encodable",
			src:        []byte("value: .inf\n"),
			wantPrefix: "marshalling openapi spec to json:",
		},
		{
			name:       "integer mapping key falls back to map[interface{}]interface{}",
			src:        []byte("1: one\n"),
			wantPrefix: "marshalling openapi spec to json:",
		},
		{
			name:       "boolean mapping key falls back to map[interface{}]interface{}",
			src:        []byte("true: yes\n"),
			wantPrefix: "marshalling openapi spec to json:",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := yamlToJSON(tt.src)
			if err == nil {
				t.Fatalf("yamlToJSON(%q) = %s, want error", tt.src, got)
			}

			if got != nil {
				t.Errorf("yamlToJSON(%q) returned %q alongside the error, want nil", tt.src, got)
			}

			if !strings.HasPrefix(err.Error(), tt.wantPrefix) {
				t.Errorf("yamlToJSON(%q) error = %q, want prefix %q", tt.src, err, tt.wantPrefix)
			}

			if errors.Unwrap(err) == nil {
				t.Errorf("yamlToJSON(%q) error %q does not wrap the underlying cause", tt.src, err)
			}
		})
	}
}

// TestYAMLToJSONTaggedScalars pins how implicitly and explicitly tagged YAML
// scalars survive the round trip through an untyped `any`: !!binary is decoded
// (not re-encoded as base64) and timestamps are normalised to RFC 3339, so a
// bare date in the spec does not stay the literal string it was written as.
func TestYAMLToJSONTaggedScalars(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		src  []byte
		want string
	}{
		{
			name: "binary tag yields the decoded bytes as a json string, not base64",
			src:  []byte("logo: !!binary aGVsbG8=\n"),
			want: "{\n  \"logo\": \"hello\"\n}",
		},
		{
			name: "binary tag with non-utf8 bytes becomes replacement characters",
			src:  []byte("blob: !!binary //79\n"),
			want: "{\n  \"blob\": \"\\ufffd\\ufffd\\ufffd\"\n}",
		},
		{
			name: "explicit timestamp tag marshals as rfc 3339",
			src:  []byte("when: !!timestamp 2024-01-02T03:04:05Z\n"),
			want: "{\n  \"when\": \"2024-01-02T03:04:05Z\"\n}",
		},
		{
			name: "untagged date is resolved as a timestamp and gains a zero time",
			src:  []byte("when: 2024-01-02\n"),
			want: "{\n  \"when\": \"2024-01-02T00:00:00Z\"\n}",
		},
		{
			name: "quoting a date keeps it a literal string",
			src:  []byte("when: \"2024-01-02\"\n"),
			want: "{\n  \"when\": \"2024-01-02\"\n}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := yamlToJSON(tt.src)
			if err != nil {
				t.Fatalf("yamlToJSON(%q) returned error: %v", tt.src, err)
			}

			if string(got) != tt.want {
				t.Errorf("yamlToJSON(%q) =\n%s\nwant:\n%s", tt.src, got, tt.want)
			}

			if !json.Valid(got) {
				t.Errorf("yamlToJSON(%q) produced invalid JSON: %s", tt.src, got)
			}
		})
	}
}

// TestYAMLToJSONRejectedDocuments covers documents that the YAML decoder
// refuses outright rather than silently accepting: duplicate mapping keys are
// an error (not last-wins), and alias expansion is capped, so a
// billion-laughs style spec fails fast instead of exhausting memory. Both
// reach the caller as the unmarshal error that turns into a plain 500.
func TestYAMLToJSONRejectedDocuments(t *testing.T) {
	t.Parallel()

	// aliasBomb nests anchors nine wide and seven deep, which expands far
	// beyond the decoder's aliasing budget.
	aliasBomb := []byte(
		"a: &a [x,x,x,x,x,x,x,x,x]\n" +
			"b: &b [*a,*a,*a,*a,*a,*a,*a,*a,*a]\n" +
			"c: &c [*b,*b,*b,*b,*b,*b,*b,*b,*b]\n" +
			"d: &d [*c,*c,*c,*c,*c,*c,*c,*c,*c]\n" +
			"e: &e [*d,*d,*d,*d,*d,*d,*d,*d,*d]\n" +
			"f: &f [*e,*e,*e,*e,*e,*e,*e,*e,*e]\n" +
			"g: &g [*f,*f,*f,*f,*f,*f,*f,*f,*f]\n",
	)

	tests := []struct {
		name         string
		src          []byte
		wantContains string
	}{
		{
			name:         "duplicate key in a block mapping is rejected, not last-wins",
			src:          []byte("a: 1\na: 2\n"),
			wantContains: `mapping key "a" already defined at line 1`,
		},
		{
			name:         "duplicate key in a nested mapping is rejected",
			src:          []byte("info:\n  title: one\n  title: two\n"),
			wantContains: `mapping key "title" already defined at line 2`,
		},
		{
			name:         "duplicate key in a flow mapping is rejected",
			src:          []byte("{a: 1, a: 2}\n"),
			wantContains: `mapping key "a" already defined at line 1`,
		},
		{
			name:         "alias bomb is stopped by the aliasing budget",
			src:          aliasBomb,
			wantContains: "excessive aliasing",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := yamlToJSON(tt.src)
			if err == nil {
				t.Fatalf("yamlToJSON(%q) = %s, want error", tt.src, got)
			}

			if got != nil {
				t.Errorf("yamlToJSON(%q) returned %q alongside the error, want nil", tt.src, got)
			}

			if !strings.HasPrefix(err.Error(), "unmarshalling openapi spec yaml:") {
				t.Errorf("yamlToJSON(%q) error = %q, want prefix %q", tt.src, err, "unmarshalling openapi spec yaml:")
			}

			if !strings.Contains(err.Error(), tt.wantContains) {
				t.Errorf("yamlToJSON(%q) error = %q, want it to mention %q", tt.src, err, tt.wantContains)
			}

			if errors.Unwrap(err) == nil {
				t.Errorf("yamlToJSON(%q) error %q does not wrap the underlying cause", tt.src, err)
			}
		})
	}
}

// errClientDisconnected is what failingResponseWriter reports on every write.
var errClientDisconnected = errors.New("client disconnected")

// failingResponseWriter is an http.ResponseWriter whose Write always fails. It
// exercises the handler's write-error path, which may only log — the 200 status
// is already committed by then.
type failingResponseWriter struct {
	header http.Header
	code   int
	writes int
}

func (w *failingResponseWriter) Header() http.Header {
	if w.header == nil {
		w.header = make(http.Header)
	}

	return w.header
}

func (w *failingResponseWriter) Write(p []byte) (int, error) {
	w.writes++

	return 0, errClientDisconnected
}

func (w *failingResponseWriter) WriteHeader(statusCode int) {
	w.code = statusCode
}

// setOpenAPISpecForTest swaps the package-level spec for the duration of the
// test and restores the previous value afterwards. Because openAPISpec is
// global mutable state, tests using this helper must not call t.Parallel().
func setOpenAPISpecForTest(t *testing.T, spec []byte) {
	t.Helper()

	previous := openAPISpec

	t.Cleanup(func() { openAPISpec = previous })

	openAPISpec = spec
}

func TestOpenAPISpecHandlerReturnsHandler(t *testing.T) {
	t.Parallel()

	if openAPISpecHandler() == nil {
		t.Fatal("openAPISpecHandler() returned nil, want non-nil http.HandlerFunc")
	}
}

//nolint:paralleltest // mutates the package-level openAPISpec; must not run beside other tests
func TestOpenAPISpecHandlerServesSpec(t *testing.T) {
	// Mutates the package-level openAPISpec, so no t.Parallel().
	yaml := "openapi: 3.1.0\ninfo:\n  title: splitkauf\n  version: 1.0.0\n"

	tests := []struct {
		name string
		spec []byte
		want []byte
	}{
		{
			name: "spec never registered",
			spec: nil,
			want: nil,
		},
		{
			name: "empty spec registered",
			spec: []byte{},
			want: []byte{},
		},
		{
			name: "yaml spec registered",
			spec: []byte(yaml),
			want: []byte(yaml),
		},
		{
			name: "invalid yaml served verbatim",
			spec: []byte("not: [valid: yaml\n\x00binary"),
			want: []byte("not: [valid: yaml\n\x00binary"),
		},
	}

	//nolint:paralleltest // mutates the package-level openAPISpec; must not run beside other tests
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setOpenAPISpecForTest(t, tt.spec)

			rec := httptest.NewRecorder()
			openAPISpecHandler()(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/openapi.yaml", nil))

			res := rec.Result()
			defer func() { _ = res.Body.Close() }()

			if res.StatusCode != http.StatusOK {
				t.Errorf("status = %d, want %d", res.StatusCode, http.StatusOK)
			}

			if got := res.Header.Get("Content-Type"); got != "application/yaml" {
				t.Errorf("Content-Type = %q, want %q", got, "application/yaml")
			}

			body, err := io.ReadAll(res.Body)
			if err != nil {
				t.Fatalf("read body: %v", err)
			}

			if !bytes.Equal(body, tt.want) {
				t.Errorf("body = %q, want %q", body, tt.want)
			}

			if len(tt.want) == 0 && len(body) != 0 {
				t.Errorf("body length = %d, want 0", len(body))
			}
		})
	}
}

//nolint:paralleltest // mutates the package-level openAPISpec; must not run beside other tests
func TestOpenAPISpecHandlerIgnoresRequest(t *testing.T) {
	// Mutates the package-level openAPISpec, so no t.Parallel().
	spec := []byte("openapi: 3.1.0\n")
	setOpenAPISpecForTest(t, spec)

	// One handler instance, as the router would build it once at construction.
	handler := openAPISpecHandler()

	tests := []struct {
		name   string
		method string
		target string
		body   string
		header http.Header
	}{
		{name: "get", method: http.MethodGet, target: "/openapi.yaml"},
		{name: "post with body", method: http.MethodPost, target: "/openapi.yaml", body: "ignored payload"},
		{name: "delete other path", method: http.MethodDelete, target: "/anything?x=1"},
		{
			name:   "get with accept header",
			method: http.MethodGet,
			target: "/openapi.yaml",
			header: http.Header{"Accept": []string{"application/json"}},
		},
	}

	//nolint:paralleltest // mutates the package-level openAPISpec; must not run beside other tests
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequestWithContext(t.Context(), tt.method, tt.target, strings.NewReader(tt.body))
			for k, vs := range tt.header {
				for _, v := range vs {
					req.Header.Add(k, v)
				}
			}

			rec := httptest.NewRecorder()
			handler(rec, req)

			res := rec.Result()
			defer func() { _ = res.Body.Close() }()

			if res.StatusCode != http.StatusOK {
				t.Errorf("status = %d, want %d", res.StatusCode, http.StatusOK)
			}

			if got := res.Header.Get("Content-Type"); got != "application/yaml" {
				t.Errorf("Content-Type = %q, want %q", got, "application/yaml")
			}

			body, err := io.ReadAll(res.Body)
			if err != nil {
				t.Fatalf("read body: %v", err)
			}

			if !bytes.Equal(body, spec) {
				t.Errorf("body = %q, want %q", body, spec)
			}
		})
	}
}

//nolint:paralleltest // mutates the package-level openAPISpec; must not run beside other tests
func TestOpenAPISpecHandlerReadsSpecPerRequest(t *testing.T) {
	// The handler is built once but must serve whatever openAPISpec holds at
	// request time. Mutates global state, so no t.Parallel().
	setOpenAPISpecForTest(t, nil)

	handler := openAPISpecHandler()

	steps := []struct {
		name string
		spec []byte
	}{
		{name: "before registration", spec: nil},
		{name: "after first registration", spec: []byte("openapi: 3.0.0\n")},
		{name: "after re-registration", spec: []byte("openapi: 3.1.0\n")},
	}

	//nolint:paralleltest // mutates the package-level openAPISpec; must not run beside other tests
	for _, step := range steps {
		t.Run(step.name, func(t *testing.T) {
			SetOpenAPISpec(step.spec)

			rec := httptest.NewRecorder()
			handler(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/openapi.yaml", nil))

			res := rec.Result()
			defer func() { _ = res.Body.Close() }()

			if res.StatusCode != http.StatusOK {
				t.Errorf("status = %d, want %d", res.StatusCode, http.StatusOK)
			}

			body, err := io.ReadAll(res.Body)
			if err != nil {
				t.Fatalf("read body: %v", err)
			}

			if !bytes.Equal(body, step.spec) {
				t.Errorf("body = %q, want %q", body, step.spec)
			}
		})
	}
}

//nolint:paralleltest // mutates the package-level openAPISpec; must not run beside other tests
func TestOpenAPISpecHandlerWriteError(t *testing.T) {
	// Mutates the package-level openAPISpec, so no t.Parallel().
	setOpenAPISpecForTest(t, []byte("openapi: 3.1.0\n"))

	w := &failingResponseWriter{}

	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("handler panicked on write error: %v", r)
			}
		}()

		openAPISpecHandler()(w, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/openapi.yaml", nil))
	}()

	if w.code != http.StatusOK {
		t.Errorf("status = %d, want %d (status is committed before the failed write)", w.code, http.StatusOK)
	}

	if got := w.Header().Get("Content-Type"); got != "application/yaml" {
		t.Errorf("Content-Type = %q, want %q", got, "application/yaml")
	}

	if w.writes != 1 {
		t.Errorf("Write called %d times, want 1 (handler must not retry)", w.writes)
	}
}
