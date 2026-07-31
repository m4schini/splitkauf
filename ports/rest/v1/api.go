// SPDX-License-Identifier: TODO

//go:generate go tool oapi-codegen -config config.yaml ../../../splitkauf.openapi.yaml
package v1

import (
	"encoding/json"
	"net/http"

	"github.com/m4schini/splitkauf/telemetry"
	"go.uber.org/zap"
)

func New(si ServerInterface, options ChiServerOptions) http.Handler {
	return HandlerWithOptions(si, options)
}

// V1 implements the generated ServerInterface. It carries the process-level
// dependencies (e.g. the database handle in later phases) shared by all
// handlers.
type V1 struct{}

// GetHealth reports overall service health and the status of downstream
// dependencies. The database check reports "disabled" until database wiring
// is enabled in a later phase.
func (v *V1) GetHealth(w http.ResponseWriter, _ *http.Request) {
	log := telemetry.Logger("api", "health")

	resp := HealthStatus{
		Status: "ok",
		Checks: HealthChecks{
			Database: "disabled",
		},
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Error("serving health", zap.Error(err))
	}
}
