// SPDX-License-Identifier: CC0-1.0

package v1_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/alexedwards/scs/v2"

	"github.com/m4schini/splitkauf/auth"
	"github.com/m4schini/splitkauf/config"
	"github.com/m4schini/splitkauf/events"
	"github.com/m4schini/splitkauf/ports/rest"
	v1 "github.com/m4schini/splitkauf/ports/rest/v1"
)

// devHandler builds the full REST handler in dev-auth mode for tests that do
// not go through newServer.
func devHandler(t *testing.T, impl v1.ServerInterface) http.Handler {
	t.Helper()

	sessions := scs.New()

	var cfg config.Config

	authr, err := auth.New(t.Context(), &cfg, sessions, noopMembers{}, nil)
	if err != nil {
		t.Fatalf("auth.New (dev): %v", err)
	}

	return rest.New(impl, sessions, authr, events.NewBroker())
}

func TestMain(m *testing.M) {
	// The REST handlers construct named loggers (via telemetry.Logger), which
	// reads config.C. Load config (defaults + env) before any handler runs.
	if err := config.Load(); err != nil {
		panic(err)
	}
	// The docs/api-catalog handlers require the OpenAPI spec, normally set from
	// the embedded copy in main. Load it from the repo root for tests.
	spec, err := os.ReadFile("../../../splitkauf.openapi.yaml")
	if err != nil {
		panic(err)
	}

	rest.SetOpenAPISpec(spec)
	os.Exit(m.Run())
}

func TestGetHealth(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(devHandler(t, &v1.V1{DB: nil, Service: nil, Events: nil}))
	defer srv.Close()

	resp := getURL(t, srv.URL+"/api/v1/health")
	defer closeBody(t, resp)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var got v1.HealthStatus
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decoding response: %v", err)
	}

	// With no database handle configured the endpoint still returns HTTP 200
	// but reports degraded status.
	if got.Status != "degraded" {
		t.Errorf("status = %q, want %q", got.Status, "degraded")
	}
}

// TestGetHealthNilDB verifies that a nil (unconfigured) database handle reports
// the database check as "error" and degrades overall status, without panicking.
func TestGetHealthNilDB(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(devHandler(t, &v1.V1{DB: nil, Service: nil, Events: nil}))
	defer srv.Close()

	resp := getURL(t, srv.URL+"/api/v1/health")
	defer closeBody(t, resp)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var got v1.HealthStatus
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decoding response: %v", err)
	}

	if got.Status != "degraded" {
		t.Errorf("status = %q, want %q", got.Status, "degraded")
	}

	if got.Checks.Database != "error" {
		t.Errorf("checks.database = %q, want %q", got.Checks.Database, "error")
	}
}
