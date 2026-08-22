// SPDX-License-Identifier: CC0-1.0

package v1

import (
	"encoding/json"
	"errors"
	"math"
	"net/http"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/m4schini/splitkauf/auth"
	"github.com/m4schini/splitkauf/lists"
	"github.com/m4schini/splitkauf/ports/rest/problem"
	"github.com/m4schini/splitkauf/telemetry"
)

// actor returns the id of the authenticated user for a mutation that records
// attribution. RequireAuth puts the user in the context before any handler
// runs, so its absence is a wiring bug, not a client error — hence the same
// Internal problem GetMe writes. On false the caller must stop; the response is
// already written.
func actor(writer http.ResponseWriter, req *http.Request) (uuid.UUID, bool) {
	user, ok := auth.UserFrom(req.Context())
	if !ok {
		problem.Write(writer, req, problem.New(problem.Internal, "no authenticated user in context"))

		return uuid.Nil, false
	}

	return user.ID, true
}

// decodeBody decodes the JSON request body into dst. On failure it writes a
// validation problem and returns false so the caller stops. The OpenAPI
// validation middleware normally rejects malformed bodies before the handler
// runs; this is a defensive backstop.
func decodeBody(writer http.ResponseWriter, req *http.Request, dst any) bool {
	if err := json.NewDecoder(req.Body).Decode(dst); err != nil {
		problem.Write(writer, req, problem.New(problem.Validation, "request body is not valid JSON"))

		return false
	}

	return true
}

// writeJSON encodes payload as an application/json response with the given
// status.
func writeJSON(writer http.ResponseWriter, status int, payload any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)

	if err := json.NewEncoder(writer).Encode(payload); err != nil {
		telemetry.Logger("api", "v1").Error("encoding response", zap.Error(err))
	}
}

// writeError maps a domain error to an RFC 9457 problem response: ErrNotFound →
// NotFound, ValidationError → Validation (with a FieldError when a field is
// named), anything else → Internal.
func writeError(writer http.ResponseWriter, req *http.Request, err error) {
	switch {
	case errors.Is(err, lists.ErrNotFound):
		problem.Write(writer, req, problem.New(problem.NotFound, "the requested resource does not exist"))
	default:
		var verr *lists.ValidationError
		if errors.As(err, &verr) {
			prob := problem.New(problem.Validation, verr.Message)
			if verr.Field != "" {
				prob.Errors = []problem.FieldError{{
					Detail:  verr.Message,
					Pointer: "/" + verr.Field,
				}}
			}

			problem.Write(writer, req, prob)

			return
		}

		telemetry.Logger("api", "v1").Error("unhandled handler error", zap.Error(err))
		problem.Write(writer, req, problem.New(problem.Internal, problem.Internal.Description))
	}
}

// toInt32 converts a domain count/quantity to the int32 used by the generated
// models, clamping to the int32 range. The bounds check keeps the conversion
// safe (item counts never approach the limit in practice).
func toInt32(value int) int32 {
	switch {
	case value > math.MaxInt32:
		return math.MaxInt32
	case value < math.MinInt32:
		return math.MinInt32
	default:
		return int32(value)
	}
}

// toAttribution maps a domain actor to its generated response model. A nil
// actor stays nil, so the field is omitted entirely — "absent" is how the API
// says the attribution is unknown. An actor whose name did not resolve keeps a
// null name; the client can still recognise its own id.
func toAttribution(act *lists.Actor) *Attribution {
	if act == nil {
		return nil
	}

	out := &Attribution{Id: act.ID, Name: nil}
	if act.Name != "" {
		name := act.Name
		out.Name = &name
	}

	return out
}

// toList maps a domain list to its generated response model.
func toList(list lists.List) List {
	return List{
		Id:               list.ID,
		Name:             list.Name,
		OpenItemCount:    toInt32(list.OpenItemCount),
		CheckedItemCount: toInt32(list.CheckedItemCount),
		CreatedBy:        toAttribution(list.CreatedBy),
		CreatedAt:        list.CreatedAt,
		UpdatedAt:        list.UpdatedAt,
	}
}

// toListWithItems maps a domain list plus its items to the generated model.
func toListWithItems(list lists.List, items []lists.Item) ListWithItems {
	out := ListWithItems{
		Id:               list.ID,
		Name:             list.Name,
		OpenItemCount:    toInt32(list.OpenItemCount),
		CheckedItemCount: toInt32(list.CheckedItemCount),
		CreatedBy:        toAttribution(list.CreatedBy),
		CreatedAt:        list.CreatedAt,
		UpdatedAt:        list.UpdatedAt,
		Items:            make([]Item, 0, len(items)),
	}
	for _, item := range items {
		out.Items = append(out.Items, toItem(item))
	}

	return out
}

// toItem maps a domain item to its generated response model.
func toItem(item lists.Item) Item {
	return Item{
		Id:        item.ID,
		ListId:    item.ListID,
		Name:      item.Name,
		Quantity:  toInt32(item.Quantity),
		Unit:      Unit(item.Unit),
		Note:      item.Note,
		Checked:   item.Checked,
		CheckedAt: item.CheckedAt,
		AddedBy:   toAttribution(item.AddedBy),
		BoughtBy:  toAttribution(item.BoughtBy),
		CreatedAt: item.CreatedAt,
		UpdatedAt: item.UpdatedAt,
	}
}
