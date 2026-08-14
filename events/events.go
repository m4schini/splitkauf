// SPDX-License-Identifier: CC0-1.0

// Package events is the in-process fan-out for real-time sync. It carries
// coarse reload hints (not state deltas) from the mutating REST handlers to
// every connected SSE stream: a client that receives an event refetches the
// affected resource over REST. Single-process, single-tenant — one global
// stream, every subscriber sees every event.
package events

// Event is a coarse reload hint broadcast to every connected client. Type says
// which resource changed; ListID is set only on TypeItems events to name the
// affected list (empty otherwise). Events are intentionally not state deltas —
// a client reacts by refetching over REST.
type Event struct {
	Type   string `json:"type"`
	ListID string `json:"listId,omitempty"`
}

// Event type constants. TypeLists covers list create/rename/delete; TypeItems
// covers item add/update/delete/check/uncheck (with ListID set).
const (
	TypeLists = "lists"
	TypeItems = "items"
)

// Publisher is the seam the REST handlers depend on to broadcast an event. The
// Broker implements it; handler tests use a capturing fake. Publish must never
// block the caller.
type Publisher interface {
	Publish(Event)
}
