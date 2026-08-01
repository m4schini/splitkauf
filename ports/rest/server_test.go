// SPDX-License-Identifier: TODO

package rest_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/m4schini/splitkauf/config"
	"github.com/m4schini/splitkauf/ports/rest"
	"github.com/m4schini/splitkauf/ports/rest/problem"
	v1 "github.com/m4schini/splitkauf/ports/rest/v1"
)

func TestMain(m *testing.M) {
	if err := config.Load(); err != nil {
		panic(err)
	}
	spec, err := os.ReadFile("../../splitkauf.openapi.yaml")
	if err != nil {
		panic(err)
	}
	rest.SetOpenAPISpec(spec)
	os.Exit(m.Run())
}

// panicServer is a ServerInterface stub whose handler always panics, to
// exercise the Recover middleware. It embeds ServerInterface so the other
// operations are satisfied without stubbing each one (they are never called
// here); only GetHealth is overridden.
type panicServer struct{ v1.ServerInterface }

func (panicServer) GetHealth(http.ResponseWriter, *http.Request) {
	panic("boom: this must never leak into the response body")
}

func decodeProblem(t *testing.T, resp *http.Response) problem.Problem {
	t.Helper()
	var p problem.Problem
	if err := json.NewDecoder(resp.Body).Decode(&p); err != nil {
		t.Fatalf("decoding problem body: %v", err)
	}
	return p
}

func TestUnknownAPIRouteReturnsNotFoundProblem(t *testing.T) {
	srv := httptest.NewServer(devHandler(t, &v1.V1{}))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/nope")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusNotFound)
	}
	if ct := resp.Header.Get("Content-Type"); ct != problem.ContentType {
		t.Errorf("Content-Type = %q, want %q", ct, problem.ContentType)
	}
	p := decodeProblem(t, resp)
	if p.Type != "/problems/not-found" {
		t.Errorf("type = %q, want %q", p.Type, "/problems/not-found")
	}
	if p.Instance != "/api/v1/nope" {
		t.Errorf("instance = %q, want %q", p.Instance, "/api/v1/nope")
	}
}

func TestWrongMethodReturnsMethodNotAllowedProblem(t *testing.T) {
	srv := httptest.NewServer(devHandler(t, &v1.V1{}))
	defer srv.Close()

	req, err := http.NewRequest(http.MethodDelete, srv.URL+"/api/v1/health", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusMethodNotAllowed)
	}
	if ct := resp.Header.Get("Content-Type"); ct != problem.ContentType {
		t.Errorf("Content-Type = %q, want %q", ct, problem.ContentType)
	}
	p := decodeProblem(t, resp)
	if p.Type != "/problems/method-not-allowed" {
		t.Errorf("type = %q, want %q", p.Type, "/problems/method-not-allowed")
	}
}

func TestPanicReturnsInternalProblemWithoutLeaking(t *testing.T) {
	srv := httptest.NewServer(devHandler(t, panicServer{}))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/health")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusInternalServerError)
	}
	if ct := resp.Header.Get("Content-Type"); ct != problem.ContentType {
		t.Errorf("Content-Type = %q, want %q", ct, problem.ContentType)
	}
	p := decodeProblem(t, resp)
	if p.Type != "/problems/internal" {
		t.Errorf("type = %q, want %q", p.Type, "/problems/internal")
	}
	if strings.Contains(p.Detail, "boom") || strings.Contains(p.Detail, "must never leak") {
		t.Errorf("detail leaks panic message: %q", p.Detail)
	}
}

// TestStaticAssetsBypassSessionMiddleware pins that static frontend assets are
// served OUTSIDE the scs session middleware: scs adds `Vary: Cookie` to every
// response it wraps, so its absence on a static asset proves the bypass. The
// JSON API still runs through the session (health carries Vary: Cookie).
func TestStaticAssetsBypassSessionMiddleware(t *testing.T) {
	srv := httptest.NewServer(devHandler(t, &v1.V1{}))
	defer srv.Close()

	staticResp, err := http.Get(srv.URL + "/some-spa-route")
	if err != nil {
		t.Fatalf("GET /some-spa-route: %v", err)
	}
	staticResp.Body.Close()
	if staticResp.StatusCode != http.StatusOK {
		t.Fatalf("static status = %d, want 200", staticResp.StatusCode)
	}
	if v := staticResp.Header.Get("Vary"); strings.Contains(v, "Cookie") {
		t.Errorf("static response has Vary: %q — assets must bypass the session middleware", v)
	}
	if c := staticResp.Header.Get("Set-Cookie"); c != "" {
		t.Errorf("static response set a cookie %q — assets must bypass the session middleware", c)
	}

	apiResp, err := http.Get(srv.URL + "/api/v1/health")
	if err != nil {
		t.Fatalf("GET /api/v1/health: %v", err)
	}
	apiResp.Body.Close()
	if v := apiResp.Header.Get("Vary"); !strings.Contains(v, "Cookie") {
		t.Errorf("API response Vary = %q, want it to contain Cookie (session middleware still applies)", v)
	}
}

// TestConcurrentStaticRequestsDoNotCrash hammers the embedded file server with
// parallel requests (as the PWA service worker's precache does). Before static
// serving was moved out of scs.LoadAndSave, this raced on the response header
// map and killed the process with a fatal "concurrent map writes".
func TestConcurrentStaticRequestsDoNotCrash(t *testing.T) {
	srv := httptest.NewServer(devHandler(t, &v1.V1{}))
	defer srv.Close()

	const n = 100
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			resp, err := http.Get(srv.URL + "/some-spa-route")
			if err != nil {
				return
			}
			_, _ = io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}()
	}
	wg.Wait()
}

// TestAuthConfigEndpointReportsMode verifies the public auth-config endpoint
// returns the resolved mode as JSON, without requiring a session (no
// Vary: Cookie, since it bypasses the session middleware).
func TestAuthConfigEndpointReportsMode(t *testing.T) {
	srv := httptest.NewServer(devHandler(t, &v1.V1{}))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/auth/config")
	if err != nil {
		t.Fatalf("GET /api/auth/config: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	var body struct {
		Mode string `json:"mode"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	// TestMain loads config with no OIDC/password env, so the mode is dev.
	if body.Mode != "dev" {
		t.Errorf("mode = %q, want dev", body.Mode)
	}
	if v := resp.Header.Get("Vary"); strings.Contains(v, "Cookie") {
		t.Errorf("auth-config carries Vary: %q — it should bypass the session middleware", v)
	}
}
