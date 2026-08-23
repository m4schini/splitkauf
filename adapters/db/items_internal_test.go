// SPDX-License-Identifier: CC0-1.0

package db

import (
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/m4schini/splitkauf/lists"
)

// Static bases for the dynamic driver-shaped errors rowScanner and
// assignScanDest fabricate below; wrapped with %w so the detail stays dynamic
// without defining a fresh dynamic error at each call site.
var (
	errScanDestMismatch = errors.New("sql: mismatched destination arguments in Scan")
	errNonPointerDest   = errors.New("destination is not a non-nil pointer")
	errUnassignableDest = errors.New("cannot assign source to destination")
)

// TestToActor pins how the LEFT-JOINed attribution columns collapse into a
// *lists.Actor: the id's Valid flag alone decides whether there is an actor at
// all, and a NULL name still yields an Actor so the client can recognise its
// own id without one.
func TestToActor(t *testing.T) {
	t.Parallel()

	actorID := uuid.MustParse("11111111-1111-1111-1111-111111111111")

	tests := []struct {
		name      string
		id        uuid.NullUUID
		actorName sql.NullString
		want      *lists.Actor
	}{
		{
			name:      "null id and null name is unattributed",
			id:        uuid.NullUUID{},
			actorName: sql.NullString{},
			want:      nil,
		},
		{
			name:      "null id discards a joined name",
			id:        uuid.NullUUID{UUID: actorID, Valid: false},
			actorName: sql.NullString{String: "alex", Valid: true},
			want:      nil,
		},
		{
			name:      "valid id with null name reports the id alone",
			id:        uuid.NullUUID{UUID: actorID, Valid: true},
			actorName: sql.NullString{},
			want:      &lists.Actor{ID: actorID, Name: ""},
		},
		{
			name:      "valid id with empty name matches the null name case",
			id:        uuid.NullUUID{UUID: actorID, Valid: true},
			actorName: sql.NullString{String: "", Valid: true},
			want:      &lists.Actor{ID: actorID, Name: ""},
		},
		{
			name:      "zero uuid is still an actor when valid",
			id:        uuid.NullUUID{UUID: uuid.Nil, Valid: true},
			actorName: sql.NullString{String: "alex", Valid: true},
			want:      &lists.Actor{ID: uuid.Nil, Name: "alex"},
		},
		{
			name:      "valid id and name are copied verbatim",
			id:        uuid.NullUUID{UUID: actorID, Valid: true},
			actorName: sql.NullString{String: "alex", Valid: true},
			want:      &lists.Actor{ID: actorID, Name: "alex"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := toActor(tt.id, tt.actorName)

			switch {
			case tt.want == nil:
				if got != nil {
					t.Fatalf("toActor() = %+v, want nil", *got)
				}
			case got == nil:
				t.Fatalf("toActor() = nil, want %+v", *tt.want)
			case *got != *tt.want:
				t.Errorf("toActor() = %+v, want %+v", *got, *tt.want)
			}
		})
	}
}

// TestToActorReturnsFreshPointer guards the two calls scanItem makes per row:
// added_by and bought_by must not end up aliasing the same Actor, or writing
// through one attribution would rewrite the other.
func TestToActorReturnsFreshPointer(t *testing.T) {
	t.Parallel()

	id := uuid.NullUUID{UUID: uuid.MustParse("11111111-1111-1111-1111-111111111111"), Valid: true}
	name := sql.NullString{String: "alex", Valid: true}

	first := toActor(id, name)
	second := toActor(id, name)

	if first == nil || second == nil {
		t.Fatalf("toActor() = (%v, %v), want two non-nil actors", first, second)
	}

	if first == second {
		t.Fatalf("toActor() returned the same pointer twice (%p)", first)
	}

	if *first != *second {
		t.Fatalf("toActor() = %+v and %+v, want equal values", *first, *second)
	}

	first.Name = "renamed"

	if second.Name != "alex" {
		t.Errorf("second.Name = %q after mutating the first actor, want %q", second.Name, "alex")
	}
}

// TestToActorCopiesNameStringRegardlessOfValid pins the undocumented invariant
// toActor leans on: it reads name.String without consulting name.Valid, so the
// empty Name for a missing member row is a consequence of database/sql zeroing
// String on NULL, not of a check in the mapper. A hand-built NullString that
// carries a leftover string with Valid=false therefore still reaches the Actor.
// database/sql never produces such a value; the test exists so that changing
// the contract to "empty unless Valid" is a deliberate, visible decision rather
// than a silent one.
func TestToActorCopiesNameStringRegardlessOfValid(t *testing.T) {
	t.Parallel()

	actorID := uuid.MustParse("55555555-5555-5555-5555-555555555555")

	got := toActor(
		uuid.NullUUID{UUID: actorID, Valid: true},
		sql.NullString{String: "ghost", Valid: false},
	)

	if got == nil {
		t.Fatalf("toActor() = nil, want an actor for a valid id")
	}

	want := lists.Actor{ID: actorID, Name: "ghost"}
	if *got != want {
		t.Errorf("toActor() = %+v, want %+v (name.String is copied verbatim, name.Valid is not consulted)",
			*got, want)
	}
}

// Fixture values for the scanItem row-mapper tests. Every column gets a
// distinct value so a silently transposed Scan destination shows up as a
// mismatch rather than an accidental pass.
//
//nolint:gochecknoglobals // shared fixtures for the scanItem row-mapper tests
var (
	scanItemID       = uuid.MustParse("aaaaaaaa-0000-4000-8000-000000000001")
	scanItemListID   = uuid.MustParse("bbbbbbbb-0000-4000-8000-000000000002")
	scanItemAddedBy  = uuid.MustParse("cccccccc-0000-4000-8000-000000000003")
	scanItemBoughtBy = uuid.MustParse("dddddddd-0000-4000-8000-000000000004")

	scanItemCheckedAt = time.Date(2024, time.March, 1, 10, 11, 12, 0, time.UTC)
	scanItemCreatedAt = time.Date(2024, time.January, 2, 3, 4, 5, 0, time.UTC)
	scanItemUpdatedAt = time.Date(2024, time.February, 3, 4, 5, 6, 0, time.UTC)
)

var errScanItemBoom = errors.New("driver exploded")

// itemRow is the driver-level shape of one itemSelect row: the nullable columns
// are `any` so a case can spell either a concrete value or an SQL NULL (nil).
type itemRow struct {
	note         any
	checked      bool
	checkedAt    any
	addedBy      any
	addedByName  any
	boughtBy     any
	boughtByName any
}

// columns spells the itemSelect projection in its documented order:
// id, list_id, name, quantity, unit, note, checked, checked_at, created_at,
// updated_at, added_by, added_by_name, bought_by, bought_by_name.
func (r itemRow) columns() []any {
	return []any{
		scanItemID.String(),
		scanItemListID.String(),
		"Milk",
		3,
		"l",
		r.note,
		r.checked,
		r.checkedAt,
		scanItemCreatedAt,
		scanItemUpdatedAt,
		r.addedBy,
		r.addedByName,
		r.boughtBy,
		r.boughtByName,
	}
}

// rowScanner is a hand-rolled scanner: it feeds a fixed list of driver-level
// column values into scanItem's Scan destinations the way database/sql would,
// so the tests exercise the real destination types (sql.NullString,
// sql.NullTime, uuid values) without needing a database. It can be scanned
// repeatedly, which models the *sql.Rows iterating read.
type rowScanner struct {
	values []any
	err    error

	gotDest int // number of destinations the last Scan call was given
}

func (r *rowScanner) Scan(dest ...any) error {
	r.gotDest = len(dest)
	if r.err != nil {
		return r.err
	}

	if len(dest) != len(r.values) {
		return fmt.Errorf("%w: expected %d, got %d", errScanDestMismatch, len(r.values), len(dest))
	}

	for i, d := range dest {
		if err := assignScanDest(d, r.values[i]); err != nil {
			return fmt.Errorf("column %d: %w", i, err)
		}
	}

	return nil
}

// assignScanDest mimics database/sql's conversion step: sql.Scanner
// destinations (sql.NullString, sql.NullTime, uuid.UUID, uuid.NullUUID, ...)
// get the raw driver value, everything else is assigned reflectively.
func assignScanDest(dest, src any) error {
	if s, ok := dest.(sql.Scanner); ok {
		return s.Scan(src) //nolint:wrapcheck // deliberate passthrough: mimics database/sql's own unwrapped Scan error
	}

	rv := reflect.ValueOf(dest)
	if rv.Kind() != reflect.Pointer || rv.IsNil() {
		return fmt.Errorf("%w: %T", errNonPointerDest, dest)
	}

	elem := rv.Elem()
	if src == nil {
		elem.Set(reflect.Zero(elem.Type()))

		return nil
	}

	sv := reflect.ValueOf(src)
	switch {
	case sv.Type().AssignableTo(elem.Type()):
	case sv.Type().ConvertibleTo(elem.Type()):
		sv = sv.Convert(elem.Type())
	default:
		return fmt.Errorf("%w: %T to %T", errUnassignableDest, src, dest)
	}

	elem.Set(sv)

	return nil
}

func formatActor(a *lists.Actor) string {
	if a == nil {
		return "<nil>"
	}

	return fmt.Sprintf("{id=%s name=%q}", a.ID, a.Name)
}

// formatItem renders an Item with its pointer fields dereferenced, so a failure
// message shows values instead of heap addresses.
func formatItem(it lists.Item) string {
	note := "<nil>"
	if it.Note != nil {
		note = strconv.Quote(*it.Note)
	}

	checkedAt := "<nil>"
	if it.CheckedAt != nil {
		checkedAt = it.CheckedAt.Format(time.RFC3339Nano)
	}

	return fmt.Sprintf(
		"ID=%s ListID=%s Name=%q Quantity=%d Unit=%q Note=%s Checked=%t CheckedAt=%s "+
			"AddedBy=%s BoughtBy=%s CreatedAt=%s UpdatedAt=%s",
		it.ID, it.ListID, it.Name, it.Quantity, it.Unit, note, it.Checked, checkedAt,
		formatActor(it.AddedBy), formatActor(it.BoughtBy),
		it.CreatedAt.Format(time.RFC3339Nano), it.UpdatedAt.Format(time.RFC3339Nano),
	)
}

// TestScanItem pins the column-order and NULL translation contract: the 14
// itemSelect columns land on the matching Item fields, and the nullable
// note/checked_at/attribution columns become nil pointers rather than pointers
// to zero values.
func TestScanItem(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		row  itemRow
		want lists.Item
	}{
		{
			name: "every column populated",
			row: itemRow{
				note:         "organic only",
				checked:      true,
				checkedAt:    scanItemCheckedAt,
				addedBy:      scanItemAddedBy.String(),
				addedByName:  "alex",
				boughtBy:     scanItemBoughtBy.String(),
				boughtByName: "robin",
			},
			want: lists.Item{
				ID:        scanItemID,
				ListID:    scanItemListID,
				Name:      "Milk",
				Quantity:  3,
				Unit:      "l",
				Note:      new("organic only"),
				Checked:   true,
				CheckedAt: new(scanItemCheckedAt),
				AddedBy:   &lists.Actor{ID: scanItemAddedBy, Name: "alex"},
				BoughtBy:  &lists.Actor{ID: scanItemBoughtBy, Name: "robin"},
				CreatedAt: scanItemCreatedAt,
				UpdatedAt: scanItemUpdatedAt,
			},
		},
		{
			name: "null note and null checked_at stay nil pointers",
			row: itemRow{
				note:         nil,
				checked:      false,
				checkedAt:    nil,
				addedBy:      scanItemAddedBy.String(),
				addedByName:  "alex",
				boughtBy:     nil,
				boughtByName: nil,
			},
			want: lists.Item{
				ID:        scanItemID,
				ListID:    scanItemListID,
				Name:      "Milk",
				Quantity:  3,
				Unit:      "l",
				Note:      nil,
				Checked:   false,
				CheckedAt: nil,
				AddedBy:   &lists.Actor{ID: scanItemAddedBy, Name: "alex"},
				BoughtBy:  nil,
				CreatedAt: scanItemCreatedAt,
				UpdatedAt: scanItemUpdatedAt,
			},
		},
		{
			name: "empty note is a pointer to the empty string, not nil",
			row: itemRow{
				note:      "",
				checked:   false,
				checkedAt: nil,
			},
			want: lists.Item{
				ID:        scanItemID,
				ListID:    scanItemListID,
				Name:      "Milk",
				Quantity:  3,
				Unit:      "l",
				Note:      new(""),
				CreatedAt: scanItemCreatedAt,
				UpdatedAt: scanItemUpdatedAt,
			},
		},
		{
			name: "no attribution at all",
			row: itemRow{
				note:         nil,
				checked:      false,
				checkedAt:    nil,
				addedBy:      nil,
				addedByName:  nil,
				boughtBy:     nil,
				boughtByName: nil,
			},
			want: lists.Item{
				ID:        scanItemID,
				ListID:    scanItemListID,
				Name:      "Milk",
				Quantity:  3,
				Unit:      "l",
				AddedBy:   nil,
				BoughtBy:  nil,
				CreatedAt: scanItemCreatedAt,
				UpdatedAt: scanItemUpdatedAt,
			},
		},
		{
			name: "added_by set while bought_by is null",
			row: itemRow{
				checked:      false,
				checkedAt:    nil,
				addedBy:      scanItemAddedBy.String(),
				addedByName:  "alex",
				boughtBy:     nil,
				boughtByName: nil,
			},
			want: lists.Item{
				ID:        scanItemID,
				ListID:    scanItemListID,
				Name:      "Milk",
				Quantity:  3,
				Unit:      "l",
				AddedBy:   &lists.Actor{ID: scanItemAddedBy, Name: "alex"},
				BoughtBy:  nil,
				CreatedAt: scanItemCreatedAt,
				UpdatedAt: scanItemUpdatedAt,
			},
		},
		{
			name: "bought_by set while added_by is null",
			row: itemRow{
				checked:      true,
				checkedAt:    scanItemCheckedAt,
				addedBy:      nil,
				addedByName:  nil,
				boughtBy:     scanItemBoughtBy.String(),
				boughtByName: "robin",
			},
			want: lists.Item{
				ID:        scanItemID,
				ListID:    scanItemListID,
				Name:      "Milk",
				Quantity:  3,
				Unit:      "l",
				Checked:   true,
				CheckedAt: new(scanItemCheckedAt),
				AddedBy:   nil,
				BoughtBy:  &lists.Actor{ID: scanItemBoughtBy, Name: "robin"},
				CreatedAt: scanItemCreatedAt,
				UpdatedAt: scanItemUpdatedAt,
			},
		},
		{
			name: "attribution id with a missing member row keeps the id and an empty name",
			row: itemRow{
				checked:      true,
				checkedAt:    scanItemCheckedAt,
				addedBy:      scanItemAddedBy.String(),
				addedByName:  nil,
				boughtBy:     scanItemBoughtBy.String(),
				boughtByName: nil,
			},
			want: lists.Item{
				ID:        scanItemID,
				ListID:    scanItemListID,
				Name:      "Milk",
				Quantity:  3,
				Unit:      "l",
				Checked:   true,
				CheckedAt: new(scanItemCheckedAt),
				AddedBy:   &lists.Actor{ID: scanItemAddedBy, Name: ""},
				BoughtBy:  &lists.Actor{ID: scanItemBoughtBy, Name: ""},
				CreatedAt: scanItemCreatedAt,
				UpdatedAt: scanItemUpdatedAt,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := scanItem(&rowScanner{values: tt.row.columns()})
			if err != nil {
				t.Fatalf("scanItem() returned error: %v", err)
			}

			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("scanItem() mismatch\n got: %s\nwant: %s", formatItem(got), formatItem(tt.want))
			}
		})
	}
}

// TestScanItemPassesEveryItemSelectColumn guards the count half of the
// column-order contract: itemSelect projects 14 columns, so scanItem must hand
// Scan exactly 14 destinations.
func TestScanItemPassesEveryItemSelectColumn(t *testing.T) {
	t.Parallel()

	const wantColumns = 14

	scanner := &rowScanner{values: itemRow{}.columns()}
	if _, err := scanItem(scanner); err != nil {
		t.Fatalf("scanItem() returned error: %v", err)
	}

	if scanner.gotDest != wantColumns {
		t.Errorf("scanItem() passed %d Scan destinations, want %d (itemSelect projects %d columns)",
			scanner.gotDest, wantColumns, wantColumns)
	}
}

// TestScanItemError pins the failure contract: a zero Item, a "scan item: "
// prefix, and an unwrappable original error — the last of which is what lets
// callers map sql.ErrNoRows to lists.ErrNotFound with errors.Is.
func TestScanItemError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		scanErr error
	}{
		{name: "no rows from a single-row read", scanErr: sql.ErrNoRows},
		{name: "arbitrary driver failure", scanErr: errScanItemBoom},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := scanItem(&rowScanner{err: tt.scanErr})
			if err == nil {
				t.Fatalf("scanItem() returned nil error, want %v", tt.scanErr)
			}

			if !errors.Is(err, tt.scanErr) {
				t.Errorf("errors.Is(err, %v) = false, err = %v", tt.scanErr, err)
			}

			if !strings.HasPrefix(err.Error(), "scan item: ") {
				t.Errorf("error %q does not start with %q", err.Error(), "scan item: ")
			}

			if !reflect.DeepEqual(got, lists.Item{}) {
				t.Errorf("scanItem() returned %s on error, want the zero Item", formatItem(got))
			}
		})
	}
}

// TestScanItemDoesNotAliasAcrossRows covers the iterating read: scanning two
// rows through the same scanner (as ListItems does over *sql.Rows) must hand
// each Item its own note/checked_at storage, never a pointer into a buffer the
// next row overwrites.
func TestScanItemDoesNotAliasAcrossRows(t *testing.T) {
	t.Parallel()

	row := itemRow{
		note:      "organic only",
		checked:   true,
		checkedAt: scanItemCheckedAt,
	}
	scanner := &rowScanner{values: row.columns()}

	first, err := scanItem(scanner)
	if err != nil {
		t.Fatalf("scanItem() first row returned error: %v", err)
	}

	second, err := scanItem(scanner)
	if err != nil {
		t.Fatalf("scanItem() second row returned error: %v", err)
	}

	if first.Note == nil || second.Note == nil {
		t.Fatalf("Note pointers = (%v, %v), want both non-nil", first.Note, second.Note)
	}

	if first.CheckedAt == nil || second.CheckedAt == nil {
		t.Fatalf("CheckedAt pointers = (%v, %v), want both non-nil", first.CheckedAt, second.CheckedAt)
	}

	if first.Note == second.Note {
		t.Error("both rows share one *string for Note; each item must own its copy")
	}

	if first.CheckedAt == second.CheckedAt {
		t.Error("both rows share one *time.Time for CheckedAt; each item must own its copy")
	}

	// Mutating one item's pointee must not be visible through the other's.
	*first.Note = "mutated"
	*first.CheckedAt = scanItemCheckedAt.Add(48 * time.Hour)

	if *second.Note != "organic only" {
		t.Errorf("second row Note = %q after mutating the first, want %q", *second.Note, "organic only")
	}

	if !second.CheckedAt.Equal(scanItemCheckedAt) {
		t.Errorf("second row CheckedAt = %s after mutating the first, want %s",
			second.CheckedAt.Format(time.RFC3339Nano), scanItemCheckedAt.Format(time.RFC3339Nano))
	}
}

// TestScanItemPassesCheckedStateThrough pins that scanItem is a plain mapper:
// it translates the nullable checked_at column and nothing else. An
// inconsistent row (checked without a timestamp, or a timestamp on an open
// item) must survive the round trip verbatim rather than being "repaired" here
// — invariant enforcement belongs to the write path, not the row scanner.
func TestScanItemPassesCheckedStateThrough(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		row  itemRow
		want lists.Item
	}{
		{
			name: "checked with a null checked_at stays nil",
			row: itemRow{
				checked:   true,
				checkedAt: nil,
			},
			want: lists.Item{
				ID:        scanItemID,
				ListID:    scanItemListID,
				Name:      "Milk",
				Quantity:  3,
				Unit:      "l",
				Checked:   true,
				CheckedAt: nil,
				CreatedAt: scanItemCreatedAt,
				UpdatedAt: scanItemUpdatedAt,
			},
		},
		{
			name: "unchecked with a checked_at timestamp keeps the timestamp",
			row: itemRow{
				checked:   false,
				checkedAt: scanItemCheckedAt,
			},
			want: lists.Item{
				ID:        scanItemID,
				ListID:    scanItemListID,
				Name:      "Milk",
				Quantity:  3,
				Unit:      "l",
				Checked:   false,
				CheckedAt: new(scanItemCheckedAt),
				CreatedAt: scanItemCreatedAt,
				UpdatedAt: scanItemUpdatedAt,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := scanItem(&rowScanner{values: tt.row.columns()})
			if err != nil {
				t.Fatalf("scanItem() returned error: %v", err)
			}

			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("scanItem() mismatch\n got: %s\nwant: %s", formatItem(got), formatItem(tt.want))
			}
		})
	}
}

// TestScanItemActorsAreIndependent covers the worst case for the two
// attribution pairs: the same member both added and bought the item, so a
// scanItem that reused one *Actor for both fields would still compare equal by
// value. Each field must own its own allocation.
func TestScanItemActorsAreIndependent(t *testing.T) {
	t.Parallel()

	row := itemRow{
		checked:      true,
		checkedAt:    scanItemCheckedAt,
		addedBy:      scanItemAddedBy.String(),
		addedByName:  "alex",
		boughtBy:     scanItemAddedBy.String(),
		boughtByName: "alex",
	}

	got, err := scanItem(&rowScanner{values: row.columns()})
	if err != nil {
		t.Fatalf("scanItem() returned error: %v", err)
	}

	if got.AddedBy == nil || got.BoughtBy == nil {
		t.Fatalf("actors = (%s, %s), want both non-nil", formatActor(got.AddedBy), formatActor(got.BoughtBy))
	}

	if got.AddedBy == got.BoughtBy {
		t.Fatal("AddedBy and BoughtBy share one *lists.Actor; each field must own its own")
	}

	got.AddedBy.Name = "mutated"
	if got.BoughtBy.Name != "alex" {
		t.Errorf("BoughtBy.Name = %q after mutating AddedBy, want %q", got.BoughtBy.Name, "alex")
	}

	if got.BoughtBy.ID != scanItemAddedBy {
		t.Errorf("BoughtBy.ID = %s, want %s", got.BoughtBy.ID, scanItemAddedBy)
	}
}

func TestNullString(t *testing.T) {
	t.Parallel()

	longString := strings.Repeat("a", 10_000)

	tests := []struct {
		name string
		in   *string
		want any
	}{
		{
			name: "nil pointer becomes SQL NULL",
			in:   nil,
			want: nil,
		},
		{
			name: "pointer to empty string is preserved, not NULL",
			in:   new(""),
			want: "",
		},
		{
			name: "pointer to ordinary string is dereferenced",
			in:   new("milk"),
			want: "milk",
		},
		{
			name: "whitespace is not trimmed",
			in:   new("  spaced  "),
			want: "  spaced  ",
		},
		{
			name: "unicode passes through unchanged",
			//nolint:gosmopolitan // deliberate fixture, not user text
			in: new("Öl, Käse & 🧀 — 日本語"),
			//nolint:gosmopolitan // deliberate fixture, not user text
			want: "Öl, Käse & 🧀 — 日本語",
		},
		{
			name: "quotes and backslashes are not escaped",
			in:   new(`O'Brien "note" \n`),
			want: `O'Brien "note" \n`,
		},
		{
			name: "newlines and tabs pass through unchanged",
			in:   new("line1\nline2\tend"),
			want: "line1\nline2\tend",
		},
		{
			name: "very long string passes through unchanged",
			in:   &longString,
			want: longString,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := nullString(tt.in)
			if got != tt.want {
				t.Errorf("nullString() = %#v, want %#v", got, tt.want)
			}

			if tt.in == nil && got != nil {
				t.Errorf("nullString(nil) = %#v (type %T), want an untyped nil interface", got, got)
			}
		})
	}
}

// TestNullStringReturnsCopy documents that the dereferenced value is returned by
// value: mutating the pointed-to string afterwards must not change the argument
// that was already handed to the driver.
func TestNullStringReturnsCopy(t *testing.T) {
	t.Parallel()

	note := "before"

	got := nullString(&note)

	note = "after"

	if got != "before" {
		t.Errorf("nullString() result changed after mutating the source string: got %#v, want %#v", got, "before")
	}
}

// TestNullTime pins the one job of the helper: a nil *time.Time collapses to
// an untyped nil interface (which database/sql binds as SQL NULL), while any
// non-nil pointer — including one pointing at the zero time — is dereferenced
// to a plain time.Time value. Callers must not expect a zero time to become
// NULL.
func TestNullTime(t *testing.T) {
	t.Parallel()

	var (
		zero    = time.Time{}
		utc     = time.Date(2026, time.August, 22, 13, 45, 6, 123456789, time.UTC)
		berlin  = time.Date(2026, time.August, 22, 15, 45, 6, 0, time.FixedZone("CEST", 2*60*60))
		now     = time.Now()
		nilTime *time.Time
	)

	tests := []struct {
		name string
		in   *time.Time
		want any
	}{
		{
			name: "nil pointer becomes SQL NULL",
			in:   nil,
			want: nil,
		},
		{
			name: "typed nil variable becomes SQL NULL",
			in:   nilTime,
			want: nil,
		},
		{
			name: "pointer to zero time stays the zero time",
			in:   &zero,
			want: zero,
		},
		{
			name: "pointer to UTC timestamp is dereferenced",
			in:   &utc,
			want: utc,
		},
		{
			name: "non-UTC location is preserved",
			in:   &berlin,
			want: berlin,
		},
		{
			name: "wall clock reading from time.Now round-trips",
			in:   &now,
			want: now,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := nullTime(tt.in)

			if tt.want == nil {
				if got != nil {
					t.Fatalf("nullTime(nil) = %#v (type %T), want untyped nil", got, got)
				}

				return
			}

			gotTime, ok := got.(time.Time)
			if !ok {
				t.Fatalf("nullTime(%v) returned %#v of type %T, want time.Time", tt.in, got, got)
			}

			wantTime, ok := tt.want.(time.Time)
			if !ok {
				t.Fatalf("test setup: want %#v is not a time.Time", tt.want)
			}

			if gotTime != wantTime {
				t.Errorf("nullTime(%v) = %v, want %v", tt.in, gotTime, wantTime)
			}

			if !gotTime.Equal(wantTime) {
				t.Errorf("nullTime(%v) = %v, not Equal to %v", tt.in, gotTime, wantTime)
			}

			if gotTime.Location() != wantTime.Location() {
				t.Errorf("nullTime(%v) location = %v, want %v",
					tt.in, gotTime.Location(), wantTime.Location())
			}
		})
	}
}

// TestNullTimeReturnsCopy pins the value semantics: the dereference happens at
// call time, so mutating the pointed-to time afterwards cannot change what was
// already bound as a query argument.
func TestNullTimeReturnsCopy(t *testing.T) {
	t.Parallel()

	original := time.Date(2026, time.August, 22, 13, 45, 6, 0, time.UTC)
	arg := original

	got, ok := nullTime(&arg).(time.Time)
	if !ok {
		t.Fatalf("nullTime returned a non-time.Time value")
	}

	arg = arg.Add(72 * time.Hour)

	if got != original {
		t.Errorf("returned time changed to %v after mutating the source pointer, want %v",
			got, original)
	}
}

// TestNullUUID pins the two-outcome contract of nullUUID: a nil pointer maps
// to an untyped nil interface (which database/sql binds as SQL NULL), and any
// non-nil pointer is dereferenced to a uuid.UUID value.
func TestNullUUID(t *testing.T) {
	t.Parallel()

	valid := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	zero := uuid.Nil

	tests := []struct {
		name    string
		id      *uuid.UUID
		wantNil bool
		want    uuid.UUID
	}{
		{
			name:    "nil pointer becomes SQL NULL",
			id:      nil,
			wantNil: true,
		},
		{
			name: "non-nil pointer is dereferenced to its value",
			id:   &valid,
			want: valid,
		},
		{
			// A zero UUID is a legitimate value, not an absent one: callers
			// that expect uuid.Nil to become NULL are relying on behaviour
			// this function deliberately does not have.
			name: "pointer to zero uuid stays a zero uuid value",
			id:   &zero,
			want: uuid.Nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := nullUUID(tt.id)

			if tt.wantNil {
				// Must be an untyped nil interface. A typed nil such as
				// (*uuid.UUID)(nil) boxed in any would compare != nil here
				// and would not be bound as SQL NULL.
				if got != nil {
					t.Fatalf("nullUUID(nil) = %#v (dynamic type %T), want untyped nil", got, got)
				}

				return
			}

			id, ok := got.(uuid.UUID)
			if !ok {
				t.Fatalf("nullUUID(%v) returned %#v of dynamic type %T, want uuid.UUID", *tt.id, got, got)
			}

			if id != tt.want {
				t.Errorf("nullUUID(%v) = %v, want %v", *tt.id, id, tt.want)
			}
		})
	}
}

// TestNullUUIDReturnsCopy checks the value semantics of the dereference: the
// returned UUID is a copy, so later mutation of the source pointer's pointee
// cannot change an argument that was already handed to the driver.
func TestNullUUIDReturnsCopy(t *testing.T) {
	t.Parallel()

	original := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	id := original

	got := nullUUID(&id)

	id = uuid.MustParse("44444444-4444-4444-4444-444444444444")

	if got != any(original) {
		t.Errorf("nullUUID returned %v after mutating the source pointer, want unchanged copy %v", got, original)
	}
}
