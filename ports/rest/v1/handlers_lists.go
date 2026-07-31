// SPDX-License-Identifier: TODO

package v1

import (
	"net/http"

	"github.com/m4schini/splitkauf/ports/rest/problem"
)

// TODO(M1 Phase 5): these are temporary stubs so *V1 satisfies the generated
// ServerInterface and the binary builds. Phase 5 replaces each with a real
// handler that decodes the request, calls the lists.Service, maps domain
// errors to problems, and encodes the response. Until then every lists/items
// operation returns a 501 Internal problem.

// notImplemented writes an RFC 9457 Internal problem with a 501 status. It is a
// placeholder for the not-yet-implemented lists/items handlers.
func notImplemented(w http.ResponseWriter, r *http.Request) {
	p := problem.New(problem.Internal, "this operation is not implemented yet")
	p.Status = http.StatusNotImplemented
	problem.Write(w, r, p)
}

// GetMe is a Phase 5 stub. See notImplemented.
func (v *V1) GetMe(w http.ResponseWriter, r *http.Request) { notImplemented(w, r) }

// ListLists is a Phase 5 stub. See notImplemented.
func (v *V1) ListLists(w http.ResponseWriter, r *http.Request) { notImplemented(w, r) }

// CreateList is a Phase 5 stub. See notImplemented.
func (v *V1) CreateList(w http.ResponseWriter, r *http.Request) { notImplemented(w, r) }

// GetList is a Phase 5 stub. See notImplemented.
func (v *V1) GetList(w http.ResponseWriter, r *http.Request, _ ListId) { notImplemented(w, r) }

// RenameList is a Phase 5 stub. See notImplemented.
func (v *V1) RenameList(w http.ResponseWriter, r *http.Request, _ ListId) { notImplemented(w, r) }

// DeleteList is a Phase 5 stub. See notImplemented.
func (v *V1) DeleteList(w http.ResponseWriter, r *http.Request, _ ListId) { notImplemented(w, r) }

// AddItem is a Phase 5 stub. See notImplemented.
func (v *V1) AddItem(w http.ResponseWriter, r *http.Request, _ ListId) { notImplemented(w, r) }

// UpdateItem is a Phase 5 stub. See notImplemented.
func (v *V1) UpdateItem(w http.ResponseWriter, r *http.Request, _ ListId, _ ItemId) {
	notImplemented(w, r)
}

// DeleteItem is a Phase 5 stub. See notImplemented.
func (v *V1) DeleteItem(w http.ResponseWriter, r *http.Request, _ ListId, _ ItemId) {
	notImplemented(w, r)
}

// CheckItem is a Phase 5 stub. See notImplemented.
func (v *V1) CheckItem(w http.ResponseWriter, r *http.Request, _ ListId, _ ItemId) {
	notImplemented(w, r)
}

// UncheckItem is a Phase 5 stub. See notImplemented.
func (v *V1) UncheckItem(w http.ResponseWriter, r *http.Request, _ ListId, _ ItemId) {
	notImplemented(w, r)
}
