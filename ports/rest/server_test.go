// SPDX-License-Identifier: TODO

package rest_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
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
