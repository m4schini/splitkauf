// SPDX-License-Identifier: TODO

// Package lists is the pure-Go domain for shopping lists and their items. It
// owns the entities, their validation rules, and a Service that orchestrates
// the eleven M1 operations over a Repository port. The Postgres implementation
// of Repository lives in adapters/db; the Service is unit-tested against an
// in-memory fake, giving domain coverage without a live database.
package lists

import (
	"context"
	"errors"
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

// units is the canonical, curated German/European grocery unit set. It is the
// single source of truth for the valid unit tokens: the OpenAPI Unit enum and
// the items.unit CHECK constraint mirror it, and a drift test pins the spec to
// it so the three never diverge. Order matches the spec enum.
var units = []string{
	"amount", "g", "kg", "ml", "l", "pack", "bottle", "can", "jar", "cup", "bunch", "bag",
}

// Units returns the canonical list of valid unit tokens (a fresh copy so callers
// cannot mutate the source of truth).
func Units() []string {
	out := make([]string, len(units))
	copy(out, units)
	return out
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

// User is an authenticated user. In M1 it is always the hardcoded dev user.
type User struct {
	ID   uuid.UUID
	Name string
}

// List is a shopping list together with a summary of its item counts. The
// counts are derived by the repository; a zero-item list has both at zero.
type List struct {
	ID               uuid.UUID
	Name             string
	OpenItemCount    int
	CheckedItemCount int
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// Item is a single entry on a shopping list. Note is optional (nil when
// unset); CheckedAt is set only while Checked is true.
type Item struct {
	ID        uuid.UUID
	ListID    uuid.UUID
	Name      string
	Quantity  int
	Unit      string
	Note      *string
	Checked   bool
	CheckedAt *time.Time
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
type Repository interface {
	CreateList(ctx context.Context, name string) (List, error)
	Lists(ctx context.Context) ([]List, error)
	List(ctx context.Context, id uuid.UUID) (List, error)
	ListItems(ctx context.Context, listID uuid.UUID) ([]Item, error)
	RenameList(ctx context.Context, id uuid.UUID, name string) (List, error)
	DeleteList(ctx context.Context, id uuid.UUID) error

	AddItem(ctx context.Context, listID uuid.UUID, name string, quantity int, unit string, note *string, checked bool) (Item, error)
	Item(ctx context.Context, listID, itemID uuid.UUID) (Item, error)
	UpdateItem(ctx context.Context, listID, itemID uuid.UUID, update ItemUpdate) (Item, error)
	DeleteItem(ctx context.Context, listID, itemID uuid.UUID) error
	RestoreItem(ctx context.Context, listID, itemID uuid.UUID) (Item, error)
	SetItemChecked(ctx context.Context, listID, itemID uuid.UUID, checked bool, checkedAt *time.Time) (Item, error)
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
		return "", &ValidationError{Field: "name", Message: "name must not be empty"}
	}
	if len(trimmed) > maxNameLength {
		return "", &ValidationError{Field: "name", Message: "name is too long"}
	}
	return trimmed, nil
}

// normalizeQuantity applies the domain default and bound for a new item's
// quantity: zero means "unset" and defaults to 1; any other value below 1 is a
// ValidationError on the "quantity" field.
func normalizeQuantity(q int) (int, error) {
	if q == 0 {
		return 1, nil
	}
	if q < 1 {
		return 0, &ValidationError{Field: "quantity", Message: "quantity must be at least 1"}
	}
	return q, nil
}

// validateUnit normalises and validates a unit token. An empty string defaults
// to "amount"; any value not in Units() is a ValidationError on the "unit"
// field. The returned value is always one of the canonical tokens.
func validateUnit(u string) (string, error) {
	if u == "" {
		return defaultUnit, nil
	}
	for _, valid := range units {
		if u == valid {
			return u, nil
		}
	}
	return "", &ValidationError{Field: "unit", Message: "unit is not a recognised value"}
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
