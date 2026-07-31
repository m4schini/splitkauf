// SPDX-License-Identifier: TODO

package web_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/m4schini/splitkauf/config"
	"github.com/m4schini/splitkauf/ports/rest"
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

func TestRootServesIndexHTML(t *testing.T) {
	srv := httptest.NewServer(rest.New(&v1.V1{}))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type = %q, want text/html prefix", ct)
	}
}

func TestSPARouteFallsBackToIndexHTML(t *testing.T) {
	srv := httptest.NewServer(rest.New(&v1.V1{}))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/some/spa/route")
	if err != nil {
		t.Fatalf("GET /some/spa/route: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type = %q, want text/html prefix", ct)
	}
}

func TestMissingFileWithExtensionReturns404(t *testing.T) {
	srv := httptest.NewServer(rest.New(&v1.V1{}))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/does-not-exist.js")
	if err != nil {
		t.Fatalf("GET /does-not-exist.js: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusNotFound)
	}
}

func TestAPIHealthStillReachable(t *testing.T) {
	srv := httptest.NewServer(rest.New(&v1.V1{}))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/health")
	if err != nil {
		t.Fatalf("GET /api/v1/health: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var got v1.HealthStatus
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if got.Status == "" {
		t.Error("status is empty, want non-empty")
	}
}
