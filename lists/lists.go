// SPDX-License-Identifier: CC0-1.0

// Package lists is the pure-Go domain for shopping lists and their items. It
// owns the entities, their validation rules, and a Service that orchestrates
// the list and item operations over a Repository port. The Postgres implementation
// of Repository lives in adapters/db; the Service is unit-tested against an
// in-memory fake, giving domain coverage without a live database.
package lists

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
)

// maxNameLength bounds list and item names to keep them display-friendly and
// to avoid unbounded storage. It is a domain rule, independent of the database.
const maxNameLength = 200

// defaultUnit is the unit assigned when none is supplied. It is rendered bare
// ("Stück") in the UI and is the schema/DB default for the unit column.
const defaultUnit = "amount"

// Names of the caller-supplied input fields cited in ValidationError values.
const (
	fieldName     = "name"
	fieldQuantity = "quantity"
	fieldUnit     = "unit"
)

// canonicalUnits returns the canonical, curated German/European grocery unit
// set. It is the single source of truth for the valid unit tokens: the OpenAPI
// Unit enum and the items.unit CHECK constraint mirror it, and a drift test
// pins the spec to it so the three never diverge. Order matches the spec enum.
func canonicalUnits() []string {
	return []string{
		defaultUnit, "g", "kg", "ml", "l", "pack", "bottle", "can", "jar", "cup", "bunch", "bag",
	}
}

// Units returns the canonical list of valid unit tokens (a fresh slice so
// callers cannot mutate the source of truth).
func Units() []string {
	return canonicalUnits()
}

// ErrNotFound is returned when a requested list or item does not exist. The
// REST layer maps it to an RFC 9457 not-found problem.
var ErrNotFound = errors.New("resource not found")

// ValidationError signals that caller-supplied input violated a domain rule.
// Field, when non-empty, names the offending input so the REST layer can emit
// a FieldError with a JSON pointer; Message is the human-readable reason.
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string { return e.Message }

// Actor is the user credited with an action (US-L.11): the creator of a list,
// or the member who added or bought an item. Only the ID is stored; Name is
// resolved at read time from the members table, so a rename propagates to every
// past action. Name is empty when no member row matches the id — the UI then
// shows nothing rather than a bare UUID (except for the acting user themselves,
// which the client recognises by id alone).
//
// A nil *Actor means the action predates attribution, or was taken by nobody
// the app can name.
type Actor struct {
	ID   uuid.UUID
	Name string
}

// List is a shopping list together with a summary of its item counts. The
// counts are derived by the repository; a zero-item list has both at zero.
// CreatedBy is nil for lists created before attribution existed.
type List struct {
	ID               uuid.UUID
	Name             string
	OpenItemCount    int
	CheckedItemCount int
	CreatedBy        *Actor
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// Item is a single entry on a shopping list. Note is optional (nil when
// unset); CheckedAt is set only while Checked is true.
//
// AddedBy and BoughtBy are nil when unknown — an item from before attribution
// existed, or, for BoughtBy, an item nobody has bought yet. BoughtBy is cleared
// whenever the item returns to the open list, so it never names a buyer for
// something still to be bought.
type Item struct {
	ID        uuid.UUID
	ListID    uuid.UUID
	Name      string
	Quantity  int
	Unit      string
	Note      *string
	Checked   bool
	CheckedAt *time.Time
	AddedBy   *Actor
	BoughtBy  *Actor
	CreatedAt time.Time
	UpdatedAt time.Time
}

// ItemUpdate is a partial update to an item: a nil field is left unchanged.
// Note is special-cased because it is nullable: NoteSet distinguishes "clear
// the note" (NoteSet true, Note nil) from "leave the note" (NoteSet false).
type ItemUpdate struct {
	Name     *string
	Quantity *int
	Unit     *string
	NoteSet  bool
	Note     *string
}

// Repository is the persistence port for lists and items. The Postgres adapter
// implements it; the Service depends only on this interface. Every method that
// addresses a specific list or item returns ErrNotFound when it does not exist
// (item lookups are always scoped to their list).
// The acting user's id is passed explicitly to every method that records
// attribution, rather than read from the context: the domain stays free of
// transport concerns, and a missing actor becomes a compile error instead of a
// silently unattributed row.
// The method set is grouped into ListRepository (the lists themselves) and
// ItemRepository (the items on them); Repository is the union the Service
// depends on.
type Repository interface {
	ListRepository
	ItemRepository
}

// ListRepository is the list-level half of the persistence port: creating,
// enumerating, renaming, copying, and deleting whole lists.
type ListRepository interface {
	CreateList(ctx context.Context, name string, createdBy uuid.UUID) (List, error)
	Lists(ctx context.Context) ([]List, error)
	List(ctx context.Context, id uuid.UUID) (List, error)
	ListItems(ctx context.Context, listID uuid.UUID) ([]Item, error)
	RenameList(ctx context.Context, id uuid.UUID, name string) (List, error)
	DeleteList(ctx context.Context, id uuid.UUID) error
	// CopyList creates a new list named name holding a copy of every
	// non-deleted item of sourceID, each reset to unchecked. It is atomic: the
	// list and its items are written in one transaction, and a source that
	// does not exist (or vanishes mid-copy) yields ErrNotFound. The copier is
	// credited as the creator of the copy and the adder of every copied item —
	// the copy is their new list, whoever assembled the original.
	CopyList(ctx context.Context, sourceID uuid.UUID, name string, actor uuid.UUID) (List, error)
}

// ItemRepository is the item-level half of the persistence port: the lifecycle
// of the entries on a list. Item lookups are always scoped to their list.
type ItemRepository interface {
	// AddItem adds an item to a list, credited to addedBy. When checked is
	// true the item is also credited to addedBy as its buyer: that combination
	// is an offline check folded into a queued create, so the same actor did
	// both and no SetItemChecked call will ever follow to record it.
	AddItem(
		ctx context.Context,
		listID uuid.UUID,
		name string,
		quantity int,
		unit string,
		note *string,
		checked bool,
		addedBy uuid.UUID,
	) (Item, error)
	Item(ctx context.Context, listID, itemID uuid.UUID) (Item, error)
	UpdateItem(ctx context.Context, listID, itemID uuid.UUID, update ItemUpdate) (Item, error)
	DeleteItem(ctx context.Context, listID, itemID uuid.UUID) error
	RestoreItem(ctx context.Context, listID, itemID uuid.UUID) (Item, error)
	// SetItemChecked writes an item's checked state. checkedBy is the buyer to
	// credit when checking, and nil when unchecking — which clears the stored
	// buyer alongside checkedAt.
	SetItemChecked(
		ctx context.Context,
		listID, itemID uuid.UUID,
		checked bool,
		checkedAt *time.Time,
		checkedBy *uuid.UUID,
	) (Item, error)
}

// validateListName trims and validates a list name, returning the cleaned
// value. An empty (or whitespace-only) name, or one exceeding maxNameLength,
// is a ValidationError on the "name" field.
func validateListName(name string) (string, error) {
	return validateName(name)
}

// validateItemName trims and validates an item name; same rules as list names.
func validateItemName(name string) (string, error) {
	return validateName(name)
}

func validateName(name string) (string, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return "", &ValidationError{Field: fieldName, Message: "name must not be empty"}
	}

	if len(trimmed) > maxNameLength {
		return "", &ValidationError{Field: fieldName, Message: "name is too long"}
	}

	return trimmed, nil
}

// copySuffix is appended to a source list's name to derive the default name of
// its copy.
const copySuffix = " (copy)"

// copyListName derives the default name for a copy of the list named original:
// the original with " (copy)" appended. When that would exceed maxNameLength
// the original is shortened from the end — one whole rune at a time, so a
// multi-byte character is never cut in half — until the suffixed name fits.
func copyListName(original string) string {
	trimmed := strings.TrimSpace(original)
	if len(trimmed)+len(copySuffix) <= maxNameLength {
		return trimmed + copySuffix
	}

	runes := []rune(trimmed)
	for len(runes) > 0 && len(string(runes))+len(copySuffix) > maxNameLength {
		runes = runes[:len(runes)-1]
	}
	// Drop whitespace exposed by the cut so the result reads as "Name (copy)".
	return strings.TrimRight(string(runes), " ") + copySuffix
}

// normalizeQuantity applies the domain default and bound for a new item's
// quantity: zero means "unset" and defaults to 1; any other value below 1 is a
// ValidationError on the "quantity" field.
func normalizeQuantity(quantity int) (int, error) {
	if quantity == 0 {
		return 1, nil
	}

	if quantity < 1 {
		return 0, &ValidationError{Field: fieldQuantity, Message: "quantity must be at least 1"}
	}

	return quantity, nil
}

// validateUnit normalises and validates a unit token. An empty string defaults
// to "amount"; any value not in Units() is a ValidationError on the "unit"
// field. The returned value is always one of the canonical tokens.
func validateUnit(unit string) (string, error) {
	if unit == "" {
		return defaultUnit, nil
	}

	if slices.Contains(canonicalUnits(), unit) {
		return unit, nil
	}

	return "", &ValidationError{Field: fieldUnit, Message: "unit is not a recognised value"}
}

// note trims an optional note. A nil pointer, or one that trims to empty, is
// normalised to nil (no note); otherwise the trimmed value is returned.
func normalizeNote(n *string) *string {
	if n == nil {
		return nil
	}

	trimmed := strings.TrimSpace(*n)
	if trimmed == "" {
		return nil
	}

	return &trimmed
}
