// SPDX-License-Identifier: CC0-1.0

package db

import (
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/m4schini/splitkauf/lists"
)

// errSimulatedTypeConversion stands in for the exact message database/sql
// reports when a driver value cannot convert to its Scan destination type.
var errSimulatedTypeConversion = errors.New(
	`sql: Scan error on column index 1, name "name": converting driver.Value type int64 to a string: invalid syntax`,
)

// listRow is one listSelect row. The value in each field doubles as the type
// the matching Scan destination must point at, so scanListStub can verify the
// projection while it fills the row in.
type listRow struct {
	id            uuid.UUID
	name          string
	createdAt     time.Time
	updatedAt     time.Time
	open          int
	checked       int
	createdBy     uuid.NullUUID
	createdByName sql.NullString
}

func (r listRow) values() []any {
	return []any{r.id, r.name, r.createdAt, r.updatedAt, r.open, r.checked, r.createdBy, r.createdByName}
}

// scanListStub stands in for the *sql.Row and *sql.Rows that reach scanList in
// production. It writes the row into the destinations scanList passes — first
// checking that their count, order, and pointee types still match listSelect —
// and returns err, optionally only after that partial write.
type scanListStub struct {
	t            *testing.T
	row          listRow
	err          error
	writeThenErr bool

	gotDest []any
}

func (s *scanListStub) Scan(dest ...any) error {
	s.t.Helper()

	s.gotDest = dest

	if s.err != nil && !s.writeThenErr {
		return s.err
	}

	values := s.row.values()
	if len(dest) != len(values) {
		s.t.Fatalf("Scan got %d destinations, want %d", len(dest), len(values))
	}

	for idx, target := range dest {
		ptr := reflect.ValueOf(target)
		if ptr.Kind() != reflect.Pointer || ptr.IsNil() {
			s.t.Fatalf("Scan destination %d is %T, want a non-nil pointer", idx, target)
		}

		if want := reflect.TypeOf(values[idx]); ptr.Type().Elem() != want {
			s.t.Fatalf("Scan destination %d is %T, want *%s", idx, target, want)
		}

		ptr.Elem().Set(reflect.ValueOf(values[idx]))
	}

	return s.err
}

// creatorName is the joined members.name for the attributed fixtures.
const creatorName = "alex"

// emptyListRow is an all-zero row, for the cases that only care about how Scan
// is called or how its error is wrapped.
func emptyListRow() listRow {
	return listRow{
		id:            uuid.UUID{},
		name:          "",
		createdAt:     time.Time{},
		updatedAt:     time.Time{},
		open:          0,
		checked:       0,
		createdBy:     uuid.NullUUID{UUID: uuid.UUID{}, Valid: false},
		createdByName: sql.NullString{String: "", Valid: false},
	}
}

func mustParseTime(t *testing.T, value string) time.Time {
	t.Helper()

	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatalf("time.Parse(%q) = %v", value, err)
	}

	return parsed
}

func assertScannedList(t *testing.T, got, want lists.List) {
	t.Helper()

	if got.ID != want.ID {
		t.Errorf("ID = %v, want %v", got.ID, want.ID)
	}

	if got.Name != want.Name {
		t.Errorf("Name = %q, want %q", got.Name, want.Name)
	}

	if got.OpenItemCount != want.OpenItemCount {
		t.Errorf("OpenItemCount = %d, want %d", got.OpenItemCount, want.OpenItemCount)
	}

	if got.CheckedItemCount != want.CheckedItemCount {
		t.Errorf("CheckedItemCount = %d, want %d", got.CheckedItemCount, want.CheckedItemCount)
	}

	if !got.CreatedAt.Equal(want.CreatedAt) {
		t.Errorf("CreatedAt = %v, want %v", got.CreatedAt, want.CreatedAt)
	}

	if !got.UpdatedAt.Equal(want.UpdatedAt) {
		t.Errorf("UpdatedAt = %v, want %v", got.UpdatedAt, want.UpdatedAt)
	}

	assertCreatedBy(t, got.CreatedBy, want.CreatedBy)
}

// assertCreatedBy keeps the nil / empty-name distinction explicit: a missing
// member row must still yield an Actor, not a nil pointer.
func assertCreatedBy(t *testing.T, got, want *lists.Actor) {
	t.Helper()

	switch {
	case want == nil && got != nil:
		t.Errorf("CreatedBy = &%+v, want nil", *got)
	case want != nil && got == nil:
		t.Errorf("CreatedBy = nil, want &%+v", *want)
	case want != nil && (got.ID != want.ID || got.Name != want.Name):
		t.Errorf("CreatedBy = &%+v, want &%+v", *got, *want)
	}
}

func assertZeroList(t *testing.T, got lists.List) {
	t.Helper()

	empty := lists.List{
		ID:               uuid.UUID{},
		Name:             "",
		OpenItemCount:    0,
		CheckedItemCount: 0,
		CreatedBy:        nil,
		CreatedAt:        time.Time{},
		UpdatedAt:        time.Time{},
	}

	if got != empty {
		t.Errorf("list = %+v, want the zero lists.List", got)
	}
}

func TestScanList(t *testing.T) {
	t.Parallel()

	listID := uuid.MustParse("aaaaaaaa-0000-4000-8000-000000000001")
	creatorID := uuid.MustParse("bbbbbbbb-0000-4000-8000-000000000002")
	created := mustParseTime(t, "2024-03-01T10:00:00Z")
	updated := mustParseTime(t, "2024-03-02T11:30:00Z")

	tests := []struct {
		name          string
		open          int
		checked       int
		createdBy     uuid.NullUUID
		createdByName sql.NullString
		wantCreatedBy *lists.Actor
	}{
		{
			name:          "attributed list names its creator",
			open:          3,
			checked:       2,
			createdBy:     uuid.NullUUID{UUID: creatorID, Valid: true},
			createdByName: sql.NullString{String: creatorName, Valid: true},
			wantCreatedBy: &lists.Actor{ID: creatorID, Name: creatorName},
		},
		{
			// Creator has no member row to join: an Actor with an empty name,
			// deliberately not a nil pointer.
			name:          "creator without a member row keeps an empty name",
			open:          1,
			checked:       0,
			createdBy:     uuid.NullUUID{UUID: creatorID, Valid: true},
			createdByName: sql.NullString{String: "", Valid: false},
			wantCreatedBy: &lists.Actor{ID: creatorID, Name: ""},
		},
		{
			name:          "pre-attribution list has no creator",
			open:          7,
			checked:       4,
			createdBy:     uuid.NullUUID{UUID: uuid.UUID{}, Valid: false},
			createdByName: sql.NullString{String: "", Valid: false},
			wantCreatedBy: nil,
		},
		{
			// A NULL id wins even if the join somehow produced a name.
			name:          "null creator id outranks a joined name",
			open:          0,
			checked:       0,
			createdBy:     uuid.NullUUID{UUID: uuid.UUID{}, Valid: false},
			createdByName: sql.NullString{String: "ghost", Valid: true},
			wantCreatedBy: nil,
		},
		{
			// COALESCE over the LEFT JOIN: an item-less list scans as 0/0.
			name:          "empty list keeps both counts at zero",
			open:          0,
			checked:       0,
			createdBy:     uuid.NullUUID{UUID: creatorID, Valid: true},
			createdByName: sql.NullString{String: creatorName, Valid: true},
			wantCreatedBy: &lists.Actor{ID: creatorID, Name: creatorName},
		},
	}

	for _, tst := range tests {
		t.Run(tst.name, func(t *testing.T) {
			t.Parallel()

			stub := &scanListStub{
				t: t,
				row: listRow{
					id:            listID,
					name:          "Groceries",
					createdAt:     created,
					updatedAt:     updated,
					open:          tst.open,
					checked:       tst.checked,
					createdBy:     tst.createdBy,
					createdByName: tst.createdByName,
				},
				err:          nil,
				writeThenErr: false,
				gotDest:      nil,
			}

			got, err := scanList(stub)
			if err != nil {
				t.Fatalf("scanList() error = %v, want nil", err)
			}

			assertScannedList(t, got, lists.List{
				ID:               listID,
				Name:             "Groceries",
				OpenItemCount:    tst.open,
				CheckedItemCount: tst.checked,
				CreatedBy:        tst.wantCreatedBy,
				CreatedAt:        created,
				UpdatedAt:        updated,
			})
		})
	}
}

func TestScanListError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		scanErr      error
		writeThenErr bool
	}{
		{
			// Load-bearing: List() maps this to lists.ErrNotFound with
			// errors.Is, so the %w wrap must keep it recognisable.
			name:         "no rows survives the wrap",
			scanErr:      sql.ErrNoRows,
			writeThenErr: false,
		},
		{
			name:         "driver error is wrapped",
			scanErr:      sql.ErrConnDone,
			writeThenErr: false,
		},
		{
			// Scan may fill some destinations before it fails; none of that
			// may leak into the returned list.
			name:         "partially written row is discarded",
			scanErr:      sql.ErrConnDone,
			writeThenErr: true,
		},
	}

	for _, tst := range tests {
		t.Run(tst.name, func(t *testing.T) {
			t.Parallel()

			row := emptyListRow()
			row.id = uuid.MustParse("cccccccc-0000-4000-8000-000000000003")
			row.name = "written before the failure"
			row.open = 9

			stub := &scanListStub{
				t:            t,
				row:          row,
				err:          tst.scanErr,
				writeThenErr: tst.writeThenErr,
				gotDest:      nil,
			}

			got, err := scanList(stub)
			if !errors.Is(err, tst.scanErr) {
				t.Fatalf("scanList() error = %v, want one wrapping %v", err, tst.scanErr)
			}

			if !strings.HasPrefix(err.Error(), "scan list: ") {
				t.Errorf("scanList() error = %q, want the %q prefix", err.Error(), "scan list: ")
			}

			assertZeroList(t, got)
		})
	}
}

// TestScanListProjection pins the destinations scanList hands to Scan against
// listSelect's column list, so the projection cannot be reordered or retyped
// without a test noticing.
func TestScanListProjection(t *testing.T) {
	t.Parallel()

	stub := &scanListStub{
		t:            t,
		row:          emptyListRow(),
		err:          nil,
		writeThenErr: false,
		gotDest:      nil,
	}

	if _, err := scanList(stub); err != nil {
		t.Fatalf("scanList() error = %v, want nil", err)
	}

	want := []string{
		"*uuid.UUID",      // id
		"*string",         // name
		"*time.Time",      // created_at
		"*time.Time",      // updated_at
		"*int",            // open_count
		"*int",            // checked_count
		"*uuid.NullUUID",  // created_by
		"*sql.NullString", // created_by_name
	}

	if len(stub.gotDest) != len(want) {
		t.Fatalf("scanList passed %d destinations, want %d", len(stub.gotDest), len(want))
	}

	for idx, target := range stub.gotDest {
		if got := reflect.TypeOf(target).String(); got != want[idx] {
			t.Errorf("destination %d is %s, want %s", idx, got, want[idx])
		}
	}
}

// scanListRejectingStub hands scanList a Scan that always fails, the way the
// driver does when a column will not convert or the projection and the
// destination list have drifted apart. It records how many destinations it was
// given so the arity case can name the real number in its error.
type scanListRejectingStub struct {
	err func(destCount int) error

	gotDestCount int
}

func (s *scanListRejectingStub) Scan(dest ...any) error {
	s.gotDestCount = len(dest)

	return s.err(len(dest))
}

// TestScanListRejectsUnscannableRow covers the failures that come from Scan
// itself rather than from a missing row: they must surface as the wrapped
// "scan list" error with a zero list, never as a panic, and never already
// translated to lists.ErrNotFound — List() owns that translation, and mapping
// a type-conversion failure to "not found" would hide a real bug.
func TestScanListRejectsUnscannableRow(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		err     func(destCount int) error
		wantErr error
	}{
		{
			// database/sql reports a column whose driver value will not fit the
			// destination like this.
			name: "type conversion failure",
			err: func(int) error {
				return errSimulatedTypeConversion
			},
			wantErr: nil,
		},
		{
			// A projection that grew or lost a column fails here first; the
			// destination count scanList passed goes into the message.
			name: "destination count mismatch",
			err: func(destCount int) error {
				return fmt.Errorf("%w: expected %d, got %d", errScanDestMismatch, destCount+1, destCount)
			},
			wantErr: nil,
		},
		{
			// scanList only wraps ErrNoRows; turning it into ErrNotFound is the
			// caller's job.
			name: "no rows is wrapped, not translated",
			err: func(int) error {
				return sql.ErrNoRows
			},
			wantErr: sql.ErrNoRows,
		},
	}

	for _, tst := range tests {
		t.Run(tst.name, func(t *testing.T) {
			t.Parallel()

			stub := &scanListRejectingStub{err: tst.err, gotDestCount: 0}

			got, err := scanList(stub)
			if err == nil {
				t.Fatalf("scanList() error = nil, want a wrapped Scan failure")
			}

			if !strings.HasPrefix(err.Error(), "scan list: ") {
				t.Errorf("scanList() error = %q, want the %q prefix", err.Error(), "scan list: ")
			}

			if tst.wantErr != nil && !errors.Is(err, tst.wantErr) {
				t.Errorf("scanList() error = %v, want one wrapping %v", err, tst.wantErr)
			}

			if errors.Is(err, lists.ErrNotFound) {
				t.Errorf("scanList() error = %v, want it left untranslated (not ErrNotFound)", err)
			}

			if got != (lists.List{}) {
				t.Errorf("list = %+v, want the zero lists.List", got)
			}

			if stub.gotDestCount == 0 {
				t.Error("scanList passed no destinations to Scan, want the listSelect columns")
			}
		})
	}
}
