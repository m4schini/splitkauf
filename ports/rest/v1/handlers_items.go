// SPDX-License-Identifier: CC0-1.0

package v1

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/m4schini/splitkauf/events"
	"github.com/m4schini/splitkauf/lists"
	"github.com/m4schini/splitkauf/ports/rest/problem"
)

// AddItem adds an item to a list from the request body.
func (v *V1) AddItem(writer http.ResponseWriter, req *http.Request, listId ListId) {
	var body AddItemJSONRequestBody
	if !decodeBody(writer, req, &body) {
		return
	}

	quantity := 0
	if body.Quantity != nil {
		quantity = int(*body.Quantity)
	}

	unit := ""
	if body.Unit != nil {
		unit = string(*body.Unit)
	}

	checked := body.Checked != nil && *body.Checked

	actorID, ok := actor(writer, req)
	if !ok {
		return
	}

	item, err := v.Service.AddItem(req.Context(), listId, body.Name, quantity, unit, body.Note, checked, actorID)
	if err != nil {
		writeError(writer, req, err)

		return
	}

	v.publish(events.Event{Type: events.TypeItems, ListID: listId.String()})
	writeJSON(writer, http.StatusCreated, toItem(item))
}

// UpdateItem applies a partial update to an item. The note is special: because
// it is nullable, "note" being present in the body (even as null) means "set
// the note" (null clears it), whereas its absence leaves the note unchanged.
// This distinction requires inspecting the raw body for the key's presence.
func (v *V1) UpdateItem(writer http.ResponseWriter, req *http.Request, listId ListId, itemId ItemId) {
	raw, err := io.ReadAll(req.Body)
	if err != nil {
		problem.Write(writer, req, problem.New(problem.Validation, "could not read request body"))

		return
	}

	var body UpdateItemJSONRequestBody
	if err := json.Unmarshal(raw, &body); err != nil {
		problem.Write(writer, req, problem.New(problem.Validation, "request body is not valid JSON"))

		return
	}

	var keys map[string]json.RawMessage

	_ = json.Unmarshal(raw, &keys)

	update := lists.ItemUpdate{Name: body.Name, Quantity: nil, Unit: nil, NoteSet: false, Note: nil}
	if body.Quantity != nil {
		quantity := int(*body.Quantity)
		update.Quantity = &quantity
	}

	if body.Unit != nil {
		unit := string(*body.Unit)
		update.Unit = &unit
	}

	if _, present := keys["note"]; present {
		update.NoteSet = true
		update.Note = body.Note
	}

	item, err := v.Service.UpdateItem(req.Context(), listId, itemId, update)
	if err != nil {
		writeError(writer, req, err)

		return
	}

	v.publish(events.Event{Type: events.TypeItems, ListID: listId.String()})
	writeJSON(writer, http.StatusOK, toItem(item))
}

// DeleteItem removes an item from a list.
func (v *V1) DeleteItem(writer http.ResponseWriter, req *http.Request, listId ListId, itemId ItemId) {
	if err := v.Service.DeleteItem(req.Context(), listId, itemId); err != nil {
		writeError(writer, req, err)

		return
	}

	v.publish(events.Event{Type: events.TypeItems, ListID: listId.String()})
	writer.WriteHeader(http.StatusNoContent)
}

// RestoreItem clears a soft-deleted item's deletion (idempotent), returning it
// to the list. It maps ErrNotFound to a 404 and publishes an items reload hint,
// exactly like the check/uncheck handlers.
func (v *V1) RestoreItem(writer http.ResponseWriter, req *http.Request, listId ListId, itemId ItemId) {
	item, err := v.Service.RestoreItem(req.Context(), listId, itemId)
	if err != nil {
		writeError(writer, req, err)

		return
	}

	v.publish(events.Event{Type: events.TypeItems, ListID: listId.String()})
	writeJSON(writer, http.StatusOK, toItem(item))
}

// CheckItem marks an item as checked (idempotent).
func (v *V1) CheckItem(writer http.ResponseWriter, req *http.Request, listId ListId, itemId ItemId) {
	actorID, ok := actor(writer, req)
	if !ok {
		return
	}

	item, err := v.Service.CheckItem(req.Context(), listId, itemId, actorID)
	if err != nil {
		writeError(writer, req, err)

		return
	}

	v.publish(events.Event{Type: events.TypeItems, ListID: listId.String()})
	writeJSON(writer, http.StatusOK, toItem(item))
}

// UncheckItem returns a checked item to the open list (idempotent).
func (v *V1) UncheckItem(writer http.ResponseWriter, req *http.Request, listId ListId, itemId ItemId) {
	actorID, ok := actor(writer, req)
	if !ok {
		return
	}

	item, err := v.Service.UncheckItem(req.Context(), listId, itemId, actorID)
	if err != nil {
		writeError(writer, req, err)

		return
	}

	v.publish(events.Event{Type: events.TypeItems, ListID: listId.String()})
	writeJSON(writer, http.StatusOK, toItem(item))
}
