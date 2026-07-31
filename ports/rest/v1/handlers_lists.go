// SPDX-License-Identifier: TODO

package v1

import (
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/google/uuid"
	"github.com/m4schini/splitkauf/auth"
	"github.com/m4schini/splitkauf/lists"
	"github.com/m4schini/splitkauf/ports/rest/problem"
	"github.com/m4schini/splitkauf/telemetry"
	"go.uber.org/zap"
)

// GetMe returns the authenticated user placed in the request context by the
// authenticator's RequireAuth middleware — the OIDC session user in OIDC mode,
// or the fixed dev user in dev-auth mode. The email is omitted when the
// provider does not supply one (and always in dev mode).
func (v *V1) GetMe(w http.ResponseWriter, r *http.Request) {
	u, ok := auth.UserFrom(r.Context())
	if !ok {
		problem.Write(w, r, problem.New(problem.Internal, "no authenticated user in context"))
		return
	}
	out := User{Id: u.ID, Name: u.Name}
	if u.Email != "" {
		email := openapi_types.Email(u.Email)
		out.Email = &email
	}
	writeJSON(w, r, http.StatusOK, out)
}

// ListLists returns every list with its item-count summary.
func (v *V1) ListLists(w http.ResponseWriter, r *http.Request) {
	all, err := v.Service.Lists(r.Context())
	if err != nil {
		writeError(w, r, err)
		return
	}
	out := make([]List, 0, len(all))
	for _, l := range all {
		out = append(out, toList(l))
	}
	writeJSON(w, r, http.StatusOK, out)
}

// CreateList creates a new list from the request body.
func (v *V1) CreateList(w http.ResponseWriter, r *http.Request) {
	var body CreateListJSONRequestBody
	if !decodeBody(w, r, &body) {
		return
	}
	l, err := v.Service.CreateList(r.Context(), body.Name)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusCreated, toList(l))
}

// GetList returns a single list together with all of its items.
func (v *V1) GetList(w http.ResponseWriter, r *http.Request, listId ListId) {
	l, items, err := v.Service.GetList(r.Context(), uuid.UUID(listId))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, toListWithItems(l, items))
}

// RenameList renames a list from the request body.
func (v *V1) RenameList(w http.ResponseWriter, r *http.Request, listId ListId) {
	var body RenameListJSONRequestBody
	if !decodeBody(w, r, &body) {
		return
	}
	l, err := v.Service.RenameList(r.Context(), uuid.UUID(listId), body.Name)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, toList(l))
}

// DeleteList deletes a list and (via cascade) all of its items.
func (v *V1) DeleteList(w http.ResponseWriter, r *http.Request, listId ListId) {
	if err := v.Service.DeleteList(r.Context(), uuid.UUID(listId)); err != nil {
		writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// AddItem adds an item to a list from the request body.
func (v *V1) AddItem(w http.ResponseWriter, r *http.Request, listId ListId) {
	var body AddItemJSONRequestBody
	if !decodeBody(w, r, &body) {
		return
	}
	quantity := 0
	if body.Quantity != nil {
		quantity = int(*body.Quantity)
	}
	item, err := v.Service.AddItem(r.Context(), uuid.UUID(listId), body.Name, quantity, body.Note)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusCreated, toItem(item))
}

// UpdateItem applies a partial update to an item. The note is special: because
// it is nullable, "note" being present in the body (even as null) means "set
// the note" (null clears it), whereas its absence leaves the note unchanged.
// This distinction requires inspecting the raw body for the key's presence.
func (v *V1) UpdateItem(w http.ResponseWriter, r *http.Request, listId ListId, itemId ItemId) {
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		problem.Write(w, r, problem.New(problem.Validation, "could not read request body"))
		return
	}
	var body UpdateItemJSONRequestBody
	if err := json.Unmarshal(raw, &body); err != nil {
		problem.Write(w, r, problem.New(problem.Validation, "request body is not valid JSON"))
		return
	}
	var keys map[string]json.RawMessage
	_ = json.Unmarshal(raw, &keys)

	update := lists.ItemUpdate{Name: body.Name}
	if body.Quantity != nil {
		q := int(*body.Quantity)
		update.Quantity = &q
	}
	if _, present := keys["note"]; present {
		update.NoteSet = true
		update.Note = body.Note
	}

	item, err := v.Service.UpdateItem(r.Context(), uuid.UUID(listId), uuid.UUID(itemId), update)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, toItem(item))
}

// DeleteItem removes an item from a list.
func (v *V1) DeleteItem(w http.ResponseWriter, r *http.Request, listId ListId, itemId ItemId) {
	if err := v.Service.DeleteItem(r.Context(), uuid.UUID(listId), uuid.UUID(itemId)); err != nil {
		writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// CheckItem marks an item as checked (idempotent).
func (v *V1) CheckItem(w http.ResponseWriter, r *http.Request, listId ListId, itemId ItemId) {
	item, err := v.Service.CheckItem(r.Context(), uuid.UUID(listId), uuid.UUID(itemId))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, toItem(item))
}

// UncheckItem returns a checked item to the open list (idempotent).
func (v *V1) UncheckItem(w http.ResponseWriter, r *http.Request, listId ListId, itemId ItemId) {
	item, err := v.Service.UncheckItem(r.Context(), uuid.UUID(listId), uuid.UUID(itemId))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, toItem(item))
}

// decodeBody decodes the JSON request body into dst. On failure it writes a
// validation problem and returns false so the caller stops. The OpenAPI
// validation middleware normally rejects malformed bodies before the handler
// runs; this is a defensive backstop.
func decodeBody(w http.ResponseWriter, r *http.Request, dst any) bool {
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		problem.Write(w, r, problem.New(problem.Validation, "request body is not valid JSON"))
		return false
	}
	return true
}

// writeJSON encodes v as an application/json response with the given status.
func writeJSON(w http.ResponseWriter, r *http.Request, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		telemetry.Logger("api", "v1").Error("encoding response", zap.Error(err))
	}
}

// writeError maps a domain error to an RFC 9457 problem response: ErrNotFound →
// NotFound, ValidationError → Validation (with a FieldError when a field is
// named), anything else → Internal.
func writeError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, lists.ErrNotFound):
		problem.Write(w, r, problem.New(problem.NotFound, "the requested resource does not exist"))
	default:
		var verr *lists.ValidationError
		if errors.As(err, &verr) {
			p := problem.New(problem.Validation, verr.Message)
			if verr.Field != "" {
				p.Errors = []problem.FieldError{{
					Detail:  verr.Message,
					Pointer: "/" + verr.Field,
				}}
			}
			problem.Write(w, r, p)
			return
		}
		telemetry.Logger("api", "v1").Error("unhandled handler error", zap.Error(err))
		problem.Write(w, r, problem.New(problem.Internal, problem.Internal.Description))
	}
}

// toInt32 converts a domain count/quantity to the int32 used by the generated
// models, clamping to the int32 range. The bounds check keeps the conversion
// safe (item counts never approach the limit in practice).
func toInt32(n int) int32 {
	switch {
	case n > math.MaxInt32:
		return math.MaxInt32
	case n < math.MinInt32:
		return math.MinInt32
	default:
		return int32(n)
	}
}

// toList maps a domain list to its generated response model.
func toList(l lists.List) List {
	return List{
		Id:               openapi_types.UUID(l.ID),
		Name:             l.Name,
		OpenItemCount:    toInt32(l.OpenItemCount),
		CheckedItemCount: toInt32(l.CheckedItemCount),
		CreatedAt:        l.CreatedAt,
		UpdatedAt:        l.UpdatedAt,
	}
}

// toListWithItems maps a domain list plus its items to the generated model.
func toListWithItems(l lists.List, items []lists.Item) ListWithItems {
	out := ListWithItems{
		Id:               openapi_types.UUID(l.ID),
		Name:             l.Name,
		OpenItemCount:    toInt32(l.OpenItemCount),
		CheckedItemCount: toInt32(l.CheckedItemCount),
		CreatedAt:        l.CreatedAt,
		UpdatedAt:        l.UpdatedAt,
		Items:            make([]Item, 0, len(items)),
	}
	for _, it := range items {
		out.Items = append(out.Items, toItem(it))
	}
	return out
}

// toItem maps a domain item to its generated response model.
func toItem(it lists.Item) Item {
	return Item{
		Id:        openapi_types.UUID(it.ID),
		ListId:    openapi_types.UUID(it.ListID),
		Name:      it.Name,
		Quantity:  toInt32(it.Quantity),
		Note:      it.Note,
		Checked:   it.Checked,
		CheckedAt: it.CheckedAt,
		CreatedAt: it.CreatedAt,
		UpdatedAt: it.UpdatedAt,
	}
}
