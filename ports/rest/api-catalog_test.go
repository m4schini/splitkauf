// SPDX-License-Identifier: CC0-1.0

package rest_test

import (
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/m4schini/splitkauf/ports/rest"
	v1 "github.com/m4schini/splitkauf/ports/rest/v1"
)

// catalogPath is where APIDocsHandler mounts the handler returned by
// apiCatalogHandler.
const catalogPath = "/.well-known/api-catalog"

// catalogContentType is the exact RFC 9727 media type the handler must send.
const catalogContentType = `application/linkset+json; profile="https://www.rfc-editor.org/info/rfc9727"`

// catalogLink mirrors one link target of the served linkset document.
type catalogLink struct {
	Href  string `json:"href"`
	Type  string `json:"type"`
	Title string `json:"title"`
}

// catalogEntry mirrors one anchored entry of the served linkset document.
type catalogEntry struct {
	Anchor      string        `json:"anchor"`
	Item        []catalogLink `json:"item"`
	ServiceDesc []catalogLink `json:"service-desc"` //nolint:tagliatelle // RFC 8631 kebab-case relation name
	ServiceDoc  []catalogLink `json:"service-doc"`  //nolint:tagliatelle // RFC 8631 kebab-case relation name
}

// catalogDoc mirrors the {"linkset":[...]} envelope.
type catalogDoc struct {
	Linkset []catalogEntry `json:"linkset"`
}

// getAPICatalog drives the mounted api-catalog handler with a hand-built
// request so scheme derivation inputs (TLS, X-Forwarded-Proto, Host) can be
// varied precisely, and returns the recorded response.
func getAPICatalog(t *testing.T, host string, useTLS bool, fwdProto string) *httptest.ResponseRecorder {
	t.Helper()

	handler := devHandler(t, &v1.V1{DB: nil, Service: nil, Events: nil})

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, catalogPath, nil)
	req.Host = host

	if useTLS {
		req.TLS = &tls.ConnectionState{}
	}

	if fwdProto != "" {
		req.Header.Set("X-Forwarded-Proto", fwdProto)
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	return rec
}

// decodeCatalog decodes a recorded api-catalog response into the linkset shape.
func decodeCatalog(t *testing.T, rec *httptest.ResponseRecorder) catalogDoc {
	t.Helper()

	var doc catalogDoc
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatalf("decoding linkset body %q: %v", rec.Body.String(), err)
	}

	return doc
}

// TestAPICatalogBaseURLDerivation pins requestBaseURL: TLS wins over the
// forwarded-proto header, the header is trusted verbatim (including garbage),
// and the fallback is plain http. Host, port included, is passed through
// unchanged.
func TestAPICatalogBaseURLDerivation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		host     string
		useTLS   bool
		fwdProto string
		wantBase string
	}{
		{
			name:     "plain http",
			host:     "example.test",
			wantBase: "http://example.test",
		},
		{
			name:     "tls wins over forwarded proto",
			host:     "example.test",
			useTLS:   true,
			fwdProto: "http",
			wantBase: "https://example.test",
		},
		{
			name:     "forwarded proto https without tls",
			host:     "example.test",
			fwdProto: "https",
			wantBase: "https://example.test",
		},
		{
			name:     "empty forwarded proto falls back to http",
			host:     "example.test",
			fwdProto: "",
			wantBase: "http://example.test",
		},
		{
			name:     "forwarded proto is unvalidated",
			host:     "example.test",
			fwdProto: "javascript",
			wantBase: "javascript://example.test",
		},
		{
			name:     "host keeps its port",
			host:     "localhost:8080",
			wantBase: "http://localhost:8080",
		},
		{
			name:     "empty host is unguarded",
			host:     "",
			wantBase: "http://",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rec := getAPICatalog(t, tt.host, tt.useTLS, tt.fwdProto)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d (body %q)", rec.Code, http.StatusOK, rec.Body.String())
			}

			doc := decodeCatalog(t, rec)

			if len(doc.Linkset) != 2 {
				t.Fatalf("linkset has %d entries, want 2", len(doc.Linkset))
			}

			gotHrefs := []string{
				doc.Linkset[0].Anchor,
				doc.Linkset[0].Item[0].Href,
				doc.Linkset[1].Anchor,
				doc.Linkset[1].ServiceDesc[0].Href,
				doc.Linkset[1].ServiceDesc[1].Href,
				doc.Linkset[1].ServiceDoc[0].Href,
			}
			wantHrefs := []string{
				tt.wantBase + catalogPath,
				tt.wantBase + "/api/v1",
				tt.wantBase + "/api/v1",
				tt.wantBase + "/openapi.yaml",
				tt.wantBase + "/openapi.json",
				tt.wantBase + "/docs",
			}

			for i, want := range wantHrefs {
				if gotHrefs[i] != want {
					t.Errorf("href[%d] = %q, want %q", i, gotHrefs[i], want)
				}
			}
		})
	}
}

// TestAPICatalogDocument pins the full observable document for one base URL:
// status, media type, trailing newline, entry order, link types and titles.
func TestAPICatalogDocument(t *testing.T) {
	t.Parallel()

	rec := getAPICatalog(t, "example.test", false, "")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	if got := rec.Header().Get("Content-Type"); got != catalogContentType {
		t.Errorf("Content-Type = %q, want %q", got, catalogContentType)
	}

	if body := rec.Body.String(); len(body) == 0 || body[len(body)-1] != '\n' {
		t.Errorf("body does not end in the newline json.Encoder appends: %q", body)
	}

	doc := decodeCatalog(t, rec)

	want := catalogDoc{
		Linkset: []catalogEntry{
			{
				Anchor: "http://example.test" + catalogPath,
				Item: []catalogLink{
					{Href: "http://example.test/api/v1", Title: "API"},
				},
			},
			{
				Anchor: "http://example.test/api/v1",
				ServiceDesc: []catalogLink{
					{Href: "http://example.test/openapi.yaml", Type: "application/yaml"},
					{Href: "http://example.test/openapi.json", Type: "application/json"},
				},
				ServiceDoc: []catalogLink{
					{Href: "http://example.test/docs", Type: "text/html"},
				},
			},
		},
	}

	gotJSON, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("re-marshalling decoded document: %v", err)
	}

	wantJSON, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("marshalling expected document: %v", err)
	}

	if string(gotJSON) != string(wantJSON) {
		t.Errorf("document =\n%s\nwant\n%s", gotJSON, wantJSON)
	}
}

// TestAPICatalogOmitsEmptyFields pins the omitempty contract on the wire: the
// catalog entry carries only "item" (whose link has no "type"), and the API
// entry carries only the service links, with no titles on them.
func TestAPICatalogOmitsEmptyFields(t *testing.T) {
	t.Parallel()

	rec := getAPICatalog(t, "example.test", false, "")

	var raw struct {
		Linkset []map[string]json.RawMessage `json:"linkset"`
	}

	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decoding raw linkset body %q: %v", rec.Body.String(), err)
	}

	if len(raw.Linkset) != 2 {
		t.Fatalf("linkset has %d entries, want 2", len(raw.Linkset))
	}

	tests := []struct {
		name    string
		entry   int
		present []string
		absent  []string
	}{
		{
			name:    "catalog entry",
			entry:   0,
			present: []string{"anchor", "item"},
			absent:  []string{"service-desc", "service-doc"},
		},
		{
			name:    "api entry",
			entry:   1,
			present: []string{"anchor", "service-desc", "service-doc"},
			absent:  []string{"item"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			entry := raw.Linkset[tt.entry]

			for _, key := range tt.present {
				if _, ok := entry[key]; !ok {
					t.Errorf("entry %d is missing key %q", tt.entry, key)
				}
			}

			for _, key := range tt.absent {
				if _, ok := entry[key]; ok {
					t.Errorf("entry %d must omit empty key %q", tt.entry, key)
				}
			}
		})
	}

	var items []map[string]json.RawMessage
	if err := json.Unmarshal(raw.Linkset[0]["item"], &items); err != nil {
		t.Fatalf("decoding item links: %v", err)
	}

	if len(items) != 1 {
		t.Fatalf("catalog entry has %d item links, want 1", len(items))
	}

	if _, ok := items[0]["type"]; ok {
		t.Errorf("item link must omit its empty %q field", "type")
	}

	if _, ok := items[0]["title"]; !ok {
		t.Errorf("item link must keep its %q field", "title")
	}

	for _, key := range []string{"service-desc", "service-doc"} {
		var links []map[string]json.RawMessage
		if err := json.Unmarshal(raw.Linkset[1][key], &links); err != nil {
			t.Fatalf("decoding %s links: %v", key, err)
		}

		for i, link := range links {
			if _, ok := link["title"]; ok {
				t.Errorf("%s[%d] must omit its empty %q field", key, i, "title")
			}
		}
	}
}

// TestAPICatalogForwardedProtoMultiHop pins the (surprising) proxy-chain
// behaviour: Header.Get hands back the first X-Forwarded-Proto line verbatim,
// so a comma-joined multi-hop value is spliced into the base URL as-is instead
// of being split at the comma.
func TestAPICatalogForwardedProtoMultiHop(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		fwdProto string
		wantBase string
	}{
		{
			name:     "two hops",
			fwdProto: "https, http",
			wantBase: "https, http://example.test",
		},
		{
			name:     "two hops without space",
			fwdProto: "https,http",
			wantBase: "https,http://example.test",
		},
		{
			name:     "three hops",
			fwdProto: "http, https, http",
			wantBase: "http, https, http://example.test",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			doc := decodeCatalog(t, getAPICatalog(t, "example.test", false, tt.fwdProto))

			if len(doc.Linkset) != 2 {
				t.Fatalf("linkset has %d entries, want 2", len(doc.Linkset))
			}

			if got, want := doc.Linkset[0].Anchor, tt.wantBase+catalogPath; got != want {
				t.Errorf("catalog anchor = %q, want %q", got, want)
			}

			if got, want := doc.Linkset[1].Anchor, tt.wantBase+"/api/v1"; got != want {
				t.Errorf("api anchor = %q, want %q", got, want)
			}
		})
	}
}

// TestAPICatalogIgnoresRequestDetails pins that the catalog handler reads
// nothing but scheme and Host: query string, request body and Accept header all
// leave status, Content-Type and body untouched. Repeating the plain request
// through the same handler instance also yields a byte-identical body, since
// the closure keeps no per-request state.
func TestAPICatalogIgnoresRequestDetails(t *testing.T) {
	t.Parallel()

	handler := devHandler(t, &v1.V1{DB: nil, Service: nil, Events: nil})

	serve := func(t *testing.T, target, body string, headers map[string]string) *httptest.ResponseRecorder {
		t.Helper()

		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, target, strings.NewReader(body))
		req.Host = "example.test"

		for k, v := range headers {
			req.Header.Set(k, v)
		}

		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		return rec
	}

	want := serve(t, catalogPath, "", nil)
	if want.Code != http.StatusOK {
		t.Fatalf("baseline status = %d, want %d", want.Code, http.StatusOK)
	}

	tests := []struct {
		name    string
		target  string
		body    string
		headers map[string]string
	}{
		{
			name:   "repeated plain get",
			target: catalogPath,
		},
		{
			name:   "query string ignored",
			target: catalogPath + "?pretty=1&format=yaml",
		},
		{
			name:   "request body ignored",
			target: catalogPath,
			body:   "this body is not read",
		},
		{
			name:    "accept header ignored",
			target:  catalogPath,
			headers: map[string]string{"Accept": "text/html"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rec := serve(t, tt.target, tt.body, tt.headers)

			if rec.Code != want.Code {
				t.Errorf("status = %d, want %d", rec.Code, want.Code)
			}

			if got, wantCT := rec.Header().Get("Content-Type"), catalogContentType; got != wantCT {
				t.Errorf("Content-Type = %q, want %q", got, wantCT)
			}

			if got, wantBody := rec.Body.String(), want.Body.String(); got != wantBody {
				t.Errorf("body =\n%s\nwant\n%s", got, wantBody)
			}
		})
	}
}

// errClientDisconnected is what failingWriter reports on every write.
var errClientDisconnected = errors.New("client disconnected")

// failingWriter is a ResponseWriter whose body writes always fail, simulating a
// client that disconnects mid-response. It records how often WriteHeader ran.
type failingWriter struct {
	header       http.Header
	status       int
	headerWrites int
}

func (w *failingWriter) Header() http.Header {
	if w.header == nil {
		w.header = make(http.Header)
	}

	return w.header
}

func (w *failingWriter) Write([]byte) (int, error) {
	return 0, errClientDisconnected
}

func (w *failingWriter) WriteHeader(status int) {
	w.headerWrites++
	w.status = status
}

// TestAPICatalogEncodeFailureIsOnlyLogged proves the handler has no error
// response path: a failing write is logged, not turned into a second status.
func TestAPICatalogEncodeFailureIsOnlyLogged(t *testing.T) {
	t.Parallel()

	handler := devHandler(t, &v1.V1{DB: nil, Service: nil, Events: nil})

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, catalogPath, nil)
	req.Host = "example.test"

	w := &failingWriter{}

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("handler panicked on a failing writer: %v", r)
		}
	}()

	handler.ServeHTTP(w, req)

	if w.status != http.StatusOK {
		t.Errorf("status = %d, want %d", w.status, http.StatusOK)
	}

	if w.headerWrites != 1 {
		t.Errorf("WriteHeader called %d times, want exactly 1", w.headerWrites)
	}

	if got := w.Header().Get("Content-Type"); got != catalogContentType {
		t.Errorf("Content-Type = %q, want %q", got, catalogContentType)
	}
}

// setOpenAPISpecForTest registers spec as the package-level OpenAPI spec and
// restores the real project spec once the test finishes. The spec is global
// mutable state shared with every other test in the binary — including
// parallel ones that assume it is configured — so tests that touch it must
// not call t.Parallel.
func setOpenAPISpecForTest(t *testing.T, spec []byte) {
	t.Helper()

	rest.SetOpenAPISpec(spec)
	t.Cleanup(func() { rest.SetOpenAPISpec(projectOpenAPISpec(t)) })
}

// openAPIYAMLRequest sends req through a freshly built api-docs handler and
// returns the recorded response.
func openAPIYAMLRequest(t *testing.T, req *http.Request) *http.Response {
	t.Helper()

	handler := rest.APIDocsHandler()

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	return rec.Result()
}

// assertYAMLSpecResponse checks the three things the handler promises on every
// request: 200, an exact application/yaml content type, and a verbatim body.
func assertYAMLSpecResponse(t *testing.T, res *http.Response, wantBody string) {
	t.Helper()

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

	if string(body) != wantBody {
		t.Errorf("body = %q, want %q", string(body), wantBody)
	}
}

// TestOpenAPISpecHandlerServesRegisteredSpec pins that /openapi.yaml echoes the
// registered spec bytes unmodified, and that a missing or empty spec is served
// as an empty 200 body rather than 404/500.
//
//nolint:paralleltest // mutates the package-level openAPISpec; must not run beside other tests
func TestOpenAPISpecHandlerServesRegisteredSpec(t *testing.T) {
	const validYAML = "openapi: 3.1.0\ninfo:\n  title: splitkauf\n  version: 1.0.0\npaths: {}\n"

	tests := []struct {
		name string
		spec []byte
		want string
	}{
		{
			name: "spec never registered",
			spec: nil,
			want: "",
		},
		{
			name: "empty spec registered",
			spec: []byte{},
			want: "",
		},
		{
			name: "yaml spec registered",
			spec: []byte(validYAML),
			want: validYAML,
		},
		{
			name: "spec served verbatim without trailing newline",
			spec: []byte("openapi: 3.1.0"),
			want: "openapi: 3.1.0",
		},
		{
			name: "non-yaml bytes are not validated",
			spec: []byte("\x00\x01not yaml at all"),
			want: "\x00\x01not yaml at all",
		},
	}

	//nolint:paralleltest // mutates the package-level openAPISpec; must not run beside other tests
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setOpenAPISpecForTest(t, tt.spec)

			//nolint:bodyclose // assertYAMLSpecResponse closes res.Body; bodyclose can't see across the call
			res := openAPIYAMLRequest(t, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/openapi.yaml", nil))
			assertYAMLSpecResponse(t, res, tt.want)
		})
	}
}

// TestOpenAPISpecHandlerReadsSpecAtRequestTime pins that the returned closure
// reads the global spec per request, so a SetOpenAPISpec call made after the
// handler was built is still reflected.
//
//nolint:paralleltest // mutates the package-level openAPISpec; must not run beside other tests
func TestOpenAPISpecHandlerReadsSpecAtRequestTime(t *testing.T) {
	setOpenAPISpecForTest(t, []byte("openapi: 3.1.0\n"))

	handler := rest.APIDocsHandler()

	rest.SetOpenAPISpec([]byte("openapi: 3.1.0\ninfo:\n  title: later\n"))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/openapi.yaml", nil))

	assertYAMLSpecResponse(t, rec.Result(), "openapi: 3.1.0\ninfo:\n  title: later\n")
}

// TestOpenAPISpecHandlerIgnoresRequestDetails pins that the handler ignores the
// request entirely: query, body and headers make no difference to the response.
//
//nolint:paralleltest // mutates the package-level openAPISpec; must not run beside other tests
func TestOpenAPISpecHandlerIgnoresRequestDetails(t *testing.T) {
	const spec = "openapi: 3.1.0\npaths: {}\n"

	tests := []struct {
		name    string
		target  string
		body    string
		headers map[string]string
	}{
		{
			name:   "plain get",
			target: "/openapi.yaml",
		},
		{
			name:   "query string ignored",
			target: "/openapi.yaml?format=json&pretty=1",
		},
		{
			name:   "request body ignored",
			target: "/openapi.yaml",
			body:   "this body is not read",
		},
		{
			name:    "accept header ignored",
			target:  "/openapi.yaml",
			headers: map[string]string{"Accept": "application/json"},
		},
	}

	//nolint:paralleltest // mutates the package-level openAPISpec; must not run beside other tests
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setOpenAPISpecForTest(t, []byte(spec))

			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, tt.target, strings.NewReader(tt.body))
			for k, v := range tt.headers {
				req.Header.Set(k, v)
			}

			//nolint:bodyclose // assertYAMLSpecResponse closes res.Body; bodyclose can't see across the call
			assertYAMLSpecResponse(t, openAPIYAMLRequest(t, req), spec)
		})
	}
}

// failingSpecWriter is a ResponseWriter whose Write always fails, standing in
// for a client that disconnected after the response was committed. It records
// how often WriteHeader was called so the test can prove the handler does not
// try to change an already-sent status.
type failingSpecWriter struct {
	headers          http.Header
	status           int
	writeHeaderCalls int
	writeCalls       int
}

func (w *failingSpecWriter) Header() http.Header {
	if w.headers == nil {
		w.headers = make(http.Header)
	}

	return w.headers
}

func (w *failingSpecWriter) Write(_ []byte) (int, error) {
	w.writeCalls++

	return 0, io.ErrClosedPipe
}

func (w *failingSpecWriter) WriteHeader(statusCode int) {
	w.writeHeaderCalls++

	if w.writeHeaderCalls == 1 {
		w.status = statusCode
	}
}

// TestOpenAPISpecHandlerWriteFailure pins that a failed body write is only
// logged: the handler must not panic and must not send a second status.
//
//nolint:paralleltest // mutates the package-level openAPISpec; must not run beside other tests
func TestOpenAPISpecHandlerWriteFailure(t *testing.T) {
	setOpenAPISpecForTest(t, []byte("openapi: 3.1.0\npaths: {}\n"))

	handler := rest.APIDocsHandler()

	writer := &failingSpecWriter{}

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("handler panicked on write error: %v", r)
		}
	}()

	handler.ServeHTTP(writer, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/openapi.yaml", nil))

	if writer.writeCalls == 0 {
		t.Error("handler never attempted to write the spec body")
	}

	if writer.writeHeaderCalls != 1 {
		t.Errorf("WriteHeader called %d times, want exactly 1", writer.writeHeaderCalls)
	}

	if writer.status != http.StatusOK {
		t.Errorf("status = %d, want %d", writer.status, http.StatusOK)
	}

	if got := writer.Header().Get("Content-Type"); got != "application/yaml" {
		t.Errorf("Content-Type = %q, want %q", got, "application/yaml")
	}
}

// openAPISpecJSONHandler is unexported, so it is exercised through the API
// docs router that mounts it at GET /openapi.json. The handler reads the
// package-global spec registered by rest.SetOpenAPISpec, so these tests must
// not run in parallel with each other or with the /openapi.yaml tests.

// serveOpenAPIJSON registers spec as the package-global OpenAPI spec and
// performs a single GET /openapi.json against the docs router. The real
// project spec is restored when the (sub)test finishes so cases cannot leak
// into each other, or into the parallel tests that assume it is configured.
func serveOpenAPIJSON(t *testing.T, spec []byte) *httptest.ResponseRecorder {
	t.Helper()

	rest.SetOpenAPISpec(spec)
	t.Cleanup(func() { rest.SetOpenAPISpec(projectOpenAPISpec(t)) })

	h := rest.APIDocsHandler()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/openapi.json", nil))

	return rec
}

// openAPITestSpec is a small but representative spec: it mixes strings, an
// int, a bool, a null and a nested mapping so scalar typing survives the
// YAML -> JSON conversion visibly.
const openAPITestSpec = "openapi: 3.0.0\n" +
	"enabled: true\n" +
	"nothing: null\n" +
	"info:\n" +
	"  title: splitkauf\n" +
	"  version: 1\n"

// openAPITestSpecJSON is openAPITestSpec re-marshalled with
// json.MarshalIndent(v, "", "  "): map keys sorted, two-space indent, no
// trailing newline.
const openAPITestSpecJSON = `{
  "enabled": true,
  "info": {
    "title": "splitkauf",
    "version": 1
  },
  "nothing": null,
  "openapi": "3.0.0"
}`

//nolint:paralleltest // every case mutates the package-level openAPISpec; must not run beside other tests
func TestOpenAPISpecJSONHandler(t *testing.T) {
	tests := []struct {
		name            string
		spec            []byte
		wantStatus      int
		wantContentType string
		wantBody        string
	}{
		{
			// A spec that was never registered is not treated as an error:
			// nil YAML decodes to nil, which marshals to the JSON literal
			// null. Pinning the current (arguably surprising) behaviour.
			name:            "spec never set",
			spec:            nil,
			wantStatus:      http.StatusOK,
			wantContentType: "application/json",
			wantBody:        "null",
		},
		{
			name:            "empty spec",
			spec:            []byte{},
			wantStatus:      http.StatusOK,
			wantContentType: "application/json",
			wantBody:        "null",
		},
		{
			name:            "valid spec",
			spec:            []byte(openAPITestSpec),
			wantStatus:      http.StatusOK,
			wantContentType: "application/json",
			wantBody:        openAPITestSpecJSON,
		},
		{
			// yaml.Unmarshal rejects the bytes: the error is swallowed and
			// reported as a generic plain-text 500.
			name:            "malformed yaml",
			spec:            []byte("{unclosed"),
			wantStatus:      http.StatusInternalServerError,
			wantContentType: "text/plain; charset=utf-8",
			wantBody:        "internal server error\n",
		},
		{
			name:            "yaml with bad indentation",
			spec:            []byte("info:\n  title: a\n version: 1\n"),
			wantStatus:      http.StatusInternalServerError,
			wantContentType: "text/plain; charset=utf-8",
			wantBody:        "internal server error\n",
		},
		{
			// Decodes fine as YAML but the non-string key yields a
			// map[interface{}]interface{}, which encoding/json refuses:
			// the second failure branch of the conversion.
			name:            "yaml with non-string keys",
			spec:            []byte("1: a\n2: b\n"),
			wantStatus:      http.StatusInternalServerError,
			wantContentType: "text/plain; charset=utf-8",
			wantBody:        "internal server error\n",
		},
	}

	//nolint:paralleltest // every case mutates the package-level openAPISpec; must not run beside other tests
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := serveOpenAPIJSON(t, tt.spec)

			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}

			if got := rec.Header().Get("Content-Type"); got != tt.wantContentType {
				t.Errorf("Content-Type = %q, want %q", got, tt.wantContentType)
			}

			if got := rec.Body.String(); got != tt.wantBody {
				t.Errorf("body = %q, want %q", got, tt.wantBody)
			}
		})
	}
}

// TestOpenAPISpecJSONHandlerRoundTrip checks that the served body is valid
// JSON that is semantically equivalent to the YAML input, including scalar
// typing: YAML booleans, integers and nulls must not arrive as strings.
//
//nolint:paralleltest // serveOpenAPIJSON mutates the process-wide OpenAPI spec via rest.SetOpenAPISpec
func TestOpenAPISpecJSONHandlerRoundTrip(t *testing.T) {
	rec := serveOpenAPIJSON(t, []byte(openAPITestSpec))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	body := rec.Body.Bytes()
	if !json.Valid(body) {
		t.Fatalf("body is not valid JSON: %q", body)
	}

	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}

	if v, ok := got["enabled"].(bool); !ok || !v {
		t.Errorf(`got["enabled"] = %#v, want bool true`, got["enabled"])
	}

	if v, ok := got["nothing"]; !ok || v != nil {
		t.Errorf(`got["nothing"] = %#v (present=%t), want nil`, v, ok)
	}

	if v, ok := got["openapi"].(string); !ok || v != "3.0.0" {
		t.Errorf(`got["openapi"] = %#v, want string "3.0.0"`, got["openapi"])
	}

	info, ok := got["info"].(map[string]any)
	if !ok {
		t.Fatalf(`got["info"] = %#v, want object`, got["info"])
	}

	if v, ok := info["version"].(float64); !ok || v != 1 {
		t.Errorf(`got["info"]["version"] = %#v, want number 1`, info["version"])
	}

	if v, ok := info["title"].(string); !ok || v != "splitkauf" {
		t.Errorf(`got["info"]["title"] = %#v, want string "splitkauf"`, info["title"])
	}

	// Pretty-printed with a two-space indent.
	if !strings.Contains(rec.Body.String(), "\n  \"info\": {") {
		t.Errorf("body is not two-space indented:\n%s", rec.Body.String())
	}
}

// TestOpenAPISpecJSONHandlerRepeatedRequests documents that the conversion
// runs per request without caching or mutating the global spec: repeated
// requests, on the same handler and on a freshly built one, are byte-identical.
//
//nolint:paralleltest // mutates the package-level openAPISpec; must not run beside other tests
func TestOpenAPISpecJSONHandlerRepeatedRequests(t *testing.T) {
	rest.SetOpenAPISpec([]byte(openAPITestSpec))
	t.Cleanup(func() { rest.SetOpenAPISpec(projectOpenAPISpec(t)) })

	h := rest.APIDocsHandler()

	get := func() string {
		t.Helper()

		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/openapi.json", nil))

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
		}

		return rec.Body.String()
	}

	first, second := get(), get()
	if first != second {
		t.Errorf("consecutive responses differ:\nfirst:  %q\nsecond: %q", first, second)
	}

	if got := serveOpenAPIJSON(t, []byte(openAPITestSpec)).Body.String(); got != first {
		t.Errorf("fresh handler body = %q, want %q", got, first)
	}
}

// TestOpenAPISpecJSONHandlerConcurrent exercises the read-only access to the
// global spec from many goroutines; meaningful under -race.
//
//nolint:paralleltest // mutates the package-level openAPISpec; must not run beside other tests
func TestOpenAPISpecJSONHandlerConcurrent(t *testing.T) {
	rest.SetOpenAPISpec([]byte(openAPITestSpec))
	t.Cleanup(func() { rest.SetOpenAPISpec(projectOpenAPISpec(t)) })

	h := rest.APIDocsHandler()

	const requests = 16

	var (
		wg     sync.WaitGroup
		bodies = make([]string, requests)
		codes  = make([]int, requests)
	)

	wg.Add(requests)

	for i := range requests {
		go func() {
			defer wg.Done()

			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/openapi.json", nil))

			codes[i] = rec.Code
			bodies[i] = rec.Body.String()
		}()
	}

	wg.Wait()

	for i := range requests {
		if codes[i] != http.StatusOK {
			t.Errorf("request %d: status = %d, want %d", i, codes[i], http.StatusOK)
		}

		if bodies[i] != openAPITestSpecJSON {
			t.Errorf("request %d: body = %q, want %q", i, bodies[i], openAPITestSpecJSON)
		}
	}
}

// jsonSpecWriter is a ResponseWriter that records the exact order in which the
// handler touched it: which Content-Type was already set when WriteHeader ran,
// how often WriteHeader and Write were called, and what was written. A non-nil
// writeErr makes every body write fail, standing in for a client that
// disconnected after the response was committed.
type jsonSpecWriter struct {
	headers            http.Header
	writeErr           error
	body               strings.Builder
	status             int
	writeHeaderCalls   int
	writeCalls         int
	ctypeAtWriteHeader string
}

func (w *jsonSpecWriter) Header() http.Header {
	if w.headers == nil {
		w.headers = make(http.Header)
	}

	return w.headers
}

func (w *jsonSpecWriter) Write(data []byte) (int, error) {
	w.writeCalls++

	if w.writeErr != nil {
		return 0, w.writeErr
	}

	n, _ := w.body.Write(data) // strings.Builder.Write never fails.

	return n, nil
}

func (w *jsonSpecWriter) WriteHeader(statusCode int) {
	w.writeHeaderCalls++

	if w.writeHeaderCalls == 1 {
		w.status = statusCode
		w.ctypeAtWriteHeader = w.Header().Get("Content-Type")
	}
}

// TestOpenAPISpecJSONHandlerWriteSequence pins the write sequence of
// GET /openapi.json for both outcomes of the body write. Content-Type must be
// set before the 200 goes out, exactly one status must be written, and a failed
// write must only be logged: no panic and no second (error) status once the
// headers are on the wire.
//
//nolint:paralleltest // mutates the package-level OpenAPI spec; must not run beside other tests
func TestOpenAPISpecJSONHandlerWriteSequence(t *testing.T) {
	tests := []struct {
		name     string
		writeErr error
		wantBody string
	}{
		{
			name:     "successful write",
			writeErr: nil,
			wantBody: openAPITestSpecJSON,
		},
		{
			// The write error is swallowed by a telemetry log; the 200 has
			// already been sent, so the handler cannot fall back to a 500.
			name:     "client disconnected mid-write",
			writeErr: io.ErrClosedPipe,
			wantBody: "",
		},
	}

	//nolint:paralleltest // every case mutates the package-level openAPISpec; must not run beside other tests
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setOpenAPISpecForTest(t, []byte(openAPITestSpec))

			handler := rest.APIDocsHandler()

			writer := new(jsonSpecWriter)
			writer.writeErr = tt.writeErr

			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("handler panicked on write: %v", r)
				}
			}()

			handler.ServeHTTP(writer, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/openapi.json", nil))

			if writer.writeCalls == 0 {
				t.Error("handler never attempted to write the spec body")
			}

			if writer.writeHeaderCalls != 1 {
				t.Errorf("WriteHeader called %d times, want exactly 1", writer.writeHeaderCalls)
			}

			if writer.status != http.StatusOK {
				t.Errorf("status = %d, want %d", writer.status, http.StatusOK)
			}

			// Set before the status went out, not after.
			if writer.ctypeAtWriteHeader != "application/json" {
				t.Errorf("Content-Type at WriteHeader = %q, want %q", writer.ctypeAtWriteHeader, "application/json")
			}

			if got := writer.Header().Get("Content-Type"); got != "application/json" {
				t.Errorf("Content-Type = %q, want %q", got, "application/json")
			}

			if got := writer.body.String(); got != tt.wantBody {
				t.Errorf("body = %q, want %q", got, tt.wantBody)
			}
		})
	}
}
