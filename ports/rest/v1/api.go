// SPDX-License-Identifier: TODO

//go:generate go tool oapi-codegen -config config.yaml ../../../splitkauf.openapi.yaml
package v1

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/m4schini/splitkauf/lists"
	"github.com/m4schini/splitkauf/telemetry"
	"go.uber.org/zap"
)

const healthPingTimeout = time.Second

func New(si ServerInterface, options ChiServerOptions) http.Handler {
	return HandlerWithOptions(si, options)
}

// ListService is the subset of the lists domain the REST handlers depend on. It
// mirrors *lists.Service; declaring it as an interface here lets tests inject a
// fake without a database. The concrete *lists.Service satisfies it.
type ListService interface {
	CreateList(ctx context.Context, name string) (lists.List, error)
	Lists(ctx context.Context) ([]lists.List, error)
	GetList(ctx context.Context, id uuid.UUID) (lists.List, []lists.Item, error)
	RenameList(ctx context.Context, id uuid.UUID, name string) (lists.List, error)
	DeleteList(ctx context.Context, id uuid.UUID) error
	AddItem(ctx context.Context, listID uuid.UUID, name string, quantity int, note *string) (lists.Item, error)
	UpdateItem(ctx context.Context, listID, itemID uuid.UUID, update lists.ItemUpdate) (lists.Item, error)
	DeleteItem(ctx context.Context, listID, itemID uuid.UUID) error
	CheckItem(ctx context.Context, listID, itemID uuid.UUID) (lists.Item, error)
	UncheckItem(ctx context.Context, listID, itemID uuid.UUID) (lists.Item, error)
}

// V1 implements the generated ServerInterface. It carries the process-level
// dependencies (the database handle for health checks and the lists service)
// shared by all handlers.
type V1 struct {
	DB      *sql.DB
	Service ListService
}

// GetHealth reports overall service health and the status of downstream
// dependencies. It always returns HTTP 200; inspect the payload. The database
// check pings the handle with a short timeout and reports "ok" or "error";
// overall status is "ok" only when every check passes, otherwise "degraded".
func (v *V1) GetHealth(w http.ResponseWriter, r *http.Request) {
	log := telemetry.Logger("api", "health")

	dbStatus := "ok"
	if err := v.pingDB(r.Context()); err != nil {
		dbStatus = "error"
	}

	status := "ok"
	if dbStatus != "ok" {
		status = "degraded"
	}

	resp := HealthStatus{
		Status: status,
		Checks: HealthChecks{
			Database: dbStatus,
		},
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Error("serving health", zap.Error(err))
	}
}

// pingDB verifies database reachability. A nil handle (DB not configured) is
// reported as an error so health degrades rather than panicking.
func (v *V1) pingDB(ctx context.Context) error {
	if v.DB == nil {
		return sql.ErrConnDone
	}
	ctx, cancel := context.WithTimeout(ctx, healthPingTimeout)
	defer cancel()
	return v.DB.PingContext(ctx)
}
