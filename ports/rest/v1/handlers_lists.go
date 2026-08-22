// SPDX-License-Identifier: CC0-1.0

package v1

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/m4schini/splitkauf/auth"
	"github.com/m4schini/splitkauf/events"
	"github.com/m4schini/splitkauf/ports/rest/problem"
)

// GetMe returns the authenticated user placed in the request context by the
// authenticator's RequireAuth middleware — the OIDC session user in OIDC mode,
// or the fixed dev user in dev-auth mode. The email is omitted when the
// provider does not supply one (and always in dev mode).
func (v *V1) GetMe(writer http.ResponseWriter, req *http.Request) {
	user, ok := auth.UserFrom(req.Context())
	if !ok {
		problem.Write(writer, req, problem.New(problem.Internal, "no authenticated user in context"))

		return
	}

	out := User{Id: user.ID, Name: user.Name, Email: nil}
	if user.Email != "" {
		email := openapi_types.Email(user.Email)
		out.Email = &email
	}

	writeJSON(writer, http.StatusOK, out)
}

// ListLists returns every list with its item-count summary.
func (v *V1) ListLists(writer http.ResponseWriter, req *http.Request) {
	all, err := v.Service.Lists(req.Context())
	if err != nil {
		writeError(writer, req, err)

		return
	}

	out := make([]List, 0, len(all))
	for _, list := range all {
		out = append(out, toList(list))
	}

	writeJSON(writer, http.StatusOK, out)
}

// CreateList creates a new list from the request body.
func (v *V1) CreateList(writer http.ResponseWriter, req *http.Request) {
	var body CreateListJSONRequestBody
	if !decodeBody(writer, req, &body) {
		return
	}

	actorID, ok := actor(writer, req)
	if !ok {
		return
	}

	list, err := v.Service.CreateList(req.Context(), body.Name, actorID)
	if err != nil {
		writeError(writer, req, err)

		return
	}

	v.publish(events.Event{Type: events.TypeLists, ListID: ""})
	writeJSON(writer, http.StatusCreated, toList(list))
}

// GetList returns a single list together with all of its items.
func (v *V1) GetList(writer http.ResponseWriter, req *http.Request, listId ListId) {
	list, items, err := v.Service.GetList(req.Context(), listId)
	if err != nil {
		writeError(writer, req, err)

		return
	}

	writeJSON(writer, http.StatusOK, toListWithItems(list, items))
}

// RenameList renames a list from the request body.
func (v *V1) RenameList(writer http.ResponseWriter, req *http.Request, listId ListId) {
	var body RenameListJSONRequestBody
	if !decodeBody(writer, req, &body) {
		return
	}

	list, err := v.Service.RenameList(req.Context(), listId, body.Name)
	if err != nil {
		writeError(writer, req, err)

		return
	}

	v.publish(events.Event{Type: events.TypeLists, ListID: ""})
	writeJSON(writer, http.StatusOK, toList(list))
}

// DeleteList deletes a list and (via cascade) all of its items.
func (v *V1) DeleteList(writer http.ResponseWriter, req *http.Request, listId ListId) {
	if err := v.Service.DeleteList(req.Context(), listId); err != nil {
		writeError(writer, req, err)

		return
	}

	v.publish(events.Event{Type: events.TypeLists, ListID: ""})
	writer.WriteHeader(http.StatusNoContent)
}

// CopyList copies a list, resetting every copied item to unchecked. The request
// body is optional: an absent or empty body means "no name supplied", and the
// service derives "«source name» (copy)".
func (v *V1) CopyList(writer http.ResponseWriter, req *http.Request, listId ListId) {
	var body CopyListJSONRequestBody
	// decodeBody would reject the empty body this endpoint explicitly allows,
	// so EOF is handled here as "no body"; anything else is malformed JSON.
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil && !errors.Is(err, io.EOF) {
		problem.Write(writer, req, problem.New(problem.Validation, "request body is not valid JSON"))

		return
	}

	name := ""
	if body.Name != nil {
		name = *body.Name
	}

	actorID, ok := actor(writer, req)
	if !ok {
		return
	}

	list, err := v.Service.CopyList(req.Context(), listId, name, actorID)
	if err != nil {
		writeError(writer, req, err)

		return
	}

	v.publish(events.Event{Type: events.TypeLists, ListID: ""})
	writeJSON(writer, http.StatusCreated, toList(list))
}
