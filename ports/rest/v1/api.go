// SPDX-License-Identifier: CC0-1.0

//go:generate go tool oapi-codegen -config config.yaml ../../../splitkauf.openapi.yaml
package v1

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/m4schini/splitkauf/events"
	"github.com/m4schini/splitkauf/lists"
	"github.com/m4schini/splitkauf/telemetry"
)

const healthPingTimeout = time.Second

func New(si ServerInterface, options ChiServerOptions) http.Handler {
	return HandlerWithOptions(si, options)
}

// ListService is the subset of the lists domain the REST handlers depend on. It
// mirrors *lists.Service; declaring it as an interface here lets tests inject a
// fake without a database. The concrete *lists.Service satisfies it.
type ListService interface {
	CreateList(ctx context.Context, name string, actor uuid.UUID) (lists.List, error)
	Lists(ctx context.Context) ([]lists.List, error)
	GetList(ctx context.Context, id uuid.UUID) (lists.List, []lists.Item, error)
	RenameList(ctx context.Context, id uuid.UUID, name string) (lists.List, error)
	DeleteList(ctx context.Context, id uuid.UUID) error
	CopyList(ctx context.Context, id uuid.UUID, name string, actor uuid.UUID) (lists.List, error)
	AddItem(ctx context.Context, listID uuid.UUID, name string, quantity int, unit string, note *string, checked bool, actor uuid.UUID) (lists.Item, error)
	UpdateItem(ctx context.Context, listID, itemID uuid.UUID, update lists.ItemUpdate) (lists.Item, error)
	DeleteItem(ctx context.Context, listID, itemID uuid.UUID) error
	RestoreItem(ctx context.Context, listID, itemID uuid.UUID) (lists.Item, error)
	CheckItem(ctx context.Context, listID, itemID, actor uuid.UUID) (lists.Item, error)
	UncheckItem(ctx context.Context, listID, itemID, actor uuid.UUID) (lists.Item, error)
}

// V1 implements the generated ServerInterface. It carries the process-level
// dependencies (the database handle for health checks and the lists service)
// shared by all handlers.
type V1 struct {
	DB      *sql.DB
	Service ListService
	// Events broadcasts real-time reload hints after a successful mutation. It
	// is optional: when nil (as in most handler unit tests) publish is a no-op,
	// so handlers never depend on a live broker being wired.
	Events events.Publisher
}

// publish broadcasts e via the configured Events publisher. It is nil-safe: a
// V1 with no Events (e.g. in tests) simply does not broadcast. Handlers call it
// only after a mutating service call has succeeded.
func (v *V1) publish(e events.Event) {
	if v.Events == nil {
		return
	}
	v.Events.Publish(e)
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
