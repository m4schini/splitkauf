// SPDX-License-Identifier: CC0-1.0

package db_test

import (
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/m4schini/splitkauf/adapters/db"
	"github.com/m4schini/splitkauf/lists"
)

//nolint:paralleltest // integration tests share one database and truncate tables between tests
func TestItemLifecycleAndCounts(t *testing.T) {
	repo, ctx := newTestRepo(t)

	list := mustCreateList(t, ctx, repo, "Shopping", testActor)

	note := noteWhole

	item := mustAddItem(t, ctx, repo, list.ID, "milk", 2, "amount", &note, false, testActor)
	if item.Quantity != 2 {
		t.Errorf("quantity = %d, want 2", item.Quantity)
	}

	if item.Note == nil || *item.Note != noteWhole {
		t.Errorf("note = %v, want %q", item.Note, noteWhole)
	}

	if item.Checked {
		t.Error("new item should be unchecked")
	}

	// One open item.
	assertCounts(t, repo, list.ID, 1, 0)

	// Update name, quantity, and clear the note.
	newName := "oat milk"
	newQty := 5

	updated := mustUpdateItem(t, ctx, repo, list.ID, item.ID, lists.ItemUpdate{
		Name:     &newName,
		Quantity: &newQty,
		Unit:     nil,
		NoteSet:  true,
		Note:     nil,
	})
	if updated.Name != "oat milk" || updated.Quantity != 5 || updated.Note != nil {
		t.Errorf("updated = %+v, want name=oat milk qty=5 note=nil", updated)
	}

	// Check then uncheck: counts flip to one checked and back, checkedAt
	// stamped and cleared.
	assertCheckUncheckRoundTrip(t, repo, list, item)

	// Delete the item.
	if err := repo.DeleteItem(ctx, list.ID, item.ID); err != nil {
		t.Fatalf("DeleteItem: %v", err)
	}

	if _, err := repo.Item(ctx, list.ID, item.ID); !errors.Is(err, lists.ErrNotFound) {
		t.Errorf("Item after delete err = %v, want ErrNotFound", err)
	}

	assertCounts(t, repo, list.ID, 0, 0)
}

// assertCheckUncheckRoundTrip checks the item off and back on, verifying the
// checked state, the checkedAt stamp, and the list counts at each step.
func assertCheckUncheckRoundTrip(t *testing.T, repo *db.ListsRepository, list lists.List, item lists.Item) {
	t.Helper()

	ctx := t.Context()
	now := item.CreatedAt

	checked := mustSetItemChecked(t, ctx, repo, list.ID, item.ID, true, &now, &testActor)
	if !checked.Checked || checked.CheckedAt == nil {
		t.Errorf("checked = %+v, want Checked with CheckedAt", checked)
	}

	assertCounts(t, repo, list.ID, 0, 1)

	unchecked := mustSetItemChecked(t, ctx, repo, list.ID, item.ID, false, nil, nil)
	if unchecked.Checked || unchecked.CheckedAt != nil {
		t.Errorf("unchecked = %+v, want open with nil CheckedAt", unchecked)
	}

	assertCounts(t, repo, list.ID, 1, 0)
}

//nolint:paralleltest // integration tests share one database and truncate tables between tests
func TestItemNotFound(t *testing.T) {
	repo, ctx := newTestRepo(t)

	list := mustCreateList(t, ctx, repo, "L", testActor)

	if _, err := repo.Item(ctx, list.ID, uuid.New()); !errors.Is(err, lists.ErrNotFound) {
		t.Errorf("Item err = %v, want ErrNotFound", err)
	}

	name := "x"

	_, err := repo.UpdateItem(ctx, list.ID, uuid.New(), lists.ItemUpdate{
		Name:     &name,
		Quantity: nil,
		Unit:     nil,
		NoteSet:  false,
		Note:     nil,
	})
	if !errors.Is(err, lists.ErrNotFound) {
		t.Errorf("UpdateItem err = %v, want ErrNotFound", err)
	}

	if err := repo.DeleteItem(ctx, list.ID, uuid.New()); !errors.Is(err, lists.ErrNotFound) {
		t.Errorf("DeleteItem err = %v, want ErrNotFound", err)
	}

	if _, err := repo.SetItemChecked(ctx, list.ID, uuid.New(), true, nil, nil); !errors.Is(err, lists.ErrNotFound) {
		t.Errorf("SetItemChecked err = %v, want ErrNotFound", err)
	}
}

// TestSoftDeleteAndRestore is the round trip: a delete hides the item from reads
// and counts but keeps the row, and a restore brings it back with its state.
//
//nolint:paralleltest // integration tests share one database and truncate tables between tests
func TestSoftDeleteAndRestore(t *testing.T) {
	repo, ctx := newTestRepo(t)

	list := mustCreateList(t, ctx, repo, "Shopping", testActor)
	item := mustAddItem(t, ctx, repo, list.ID, "milk", 3, "amount", nil, false, testActor)

	assertCounts(t, repo, list.ID, 1, 0)

	// Soft delete: hidden from reads and counts.
	if err := repo.DeleteItem(ctx, list.ID, item.ID); err != nil {
		t.Fatalf("DeleteItem: %v", err)
	}

	if _, err := repo.Item(ctx, list.ID, item.ID); !errors.Is(err, lists.ErrNotFound) {
		t.Errorf("Item after soft delete err = %v, want ErrNotFound", err)
	}

	items := mustListItems(t, ctx, repo, list.ID)
	if len(items) != 0 {
		t.Errorf("ListItems after soft delete = %d, want 0", len(items))
	}

	assertCounts(t, repo, list.ID, 0, 0)

	// Deleting again is a no-op ErrNotFound (already deleted).
	if err := repo.DeleteItem(ctx, list.ID, item.ID); !errors.Is(err, lists.ErrNotFound) {
		t.Errorf("second DeleteItem err = %v, want ErrNotFound", err)
	}

	// Restore: back in reads and counts, state preserved.
	restored := mustRestoreItem(t, ctx, repo, list.ID, item.ID)
	if restored.ID != item.ID || restored.Name != "milk" || restored.Quantity != 3 {
		t.Errorf("restored = %+v, want the original milk/3", restored)
	}

	if _, err := repo.Item(ctx, list.ID, item.ID); err != nil {
		t.Errorf("Item after restore err = %v, want nil", err)
	}

	assertCounts(t, repo, list.ID, 1, 0)
}

// TestRestoreNeverDeletedIsIdempotent verifies restoring an item that was never
// deleted succeeds and returns it unchanged, while a missing row is ErrNotFound.
//
//nolint:paralleltest // integration tests share one database and truncate tables between tests
func TestRestoreNeverDeletedIsIdempotent(t *testing.T) {
	repo, ctx := newTestRepo(t)

	list := mustCreateList(t, ctx, repo, "Shopping", testActor)
	item := mustAddItem(t, ctx, repo, list.ID, "milk", 1, "amount", nil, false, testActor)

	restored := mustRestoreItem(t, ctx, repo, list.ID, item.ID)
	if restored.ID != item.ID {
		t.Errorf("restored id = %v, want %v", restored.ID, item.ID)
	}

	if _, err := repo.RestoreItem(ctx, list.ID, uuid.New()); !errors.Is(err, lists.ErrNotFound) {
		t.Errorf("RestoreItem(missing) err = %v, want ErrNotFound", err)
	}
}

// TestAddItemChecked verifies creating an item with checked=true stamps
// checked_at and reflects the checked count.
//
//nolint:paralleltest // integration tests share one database and truncate tables between tests
func TestAddItemChecked(t *testing.T) {
	repo, ctx := newTestRepo(t)

	list := mustCreateList(t, ctx, repo, "Shopping", testActor)

	item := mustAddItem(t, ctx, repo, list.ID, "milk", 1, "amount", nil, true, testActor)
	if !item.Checked || item.CheckedAt == nil {
		t.Errorf("checked item = %+v, want Checked with CheckedAt", item)
	}

	assertCounts(t, repo, list.ID, 0, 1)
}

// TestAddItemNonexistentListReturnsNotFound pins the FK-violation mapping in
// AddItem: adding an item to a well-formed but nonexistent list id must
// surface as lists.ErrNotFound (mapped from the insert's foreign-key
// violation), not a raw driver/500 error. AddItem no longer pre-checks list
// existence, so this exercises the FK path directly.
//
//nolint:paralleltest // integration tests share one database and truncate tables between tests
func TestAddItemNonexistentListReturnsNotFound(t *testing.T) {
	repo, ctx := newTestRepo(t)

	_, err := repo.AddItem(ctx, uuid.New(), "milk", 1, "amount", nil, false, testActor)
	if !errors.Is(err, lists.ErrNotFound) {
		t.Errorf("AddItem to nonexistent list err = %v, want ErrNotFound", err)
	}
}

// TestItemUnitRoundTrip pins the items.unit column end to end: a non-default
// unit survives insert and read, the domain default ("amount") round-trips, an
// UpdateItem changes the unit, and the state transitions (check/uncheck/delete
// -> restore) preserve it rather than clobbering it.
//
//nolint:paralleltest // integration tests share one database and truncate tables between tests
func TestItemUnitRoundTrip(t *testing.T) {
	repo, ctx := newTestRepo(t)

	list := mustCreateList(t, ctx, repo, "Shopping", testActor)

	// Add-with-unit round trip.
	item := mustAddItem(t, ctx, repo, list.ID, "milk", 2, "l", nil, false, testActor)
	if item.Unit != "l" {
		t.Errorf("unit = %q, want l", item.Unit)
	}

	got := mustGetItem(t, ctx, repo, list.ID, item.ID)
	if got.Unit != "l" {
		t.Errorf("read unit = %q, want l", got.Unit)
	}

	// Default "amount" round-trips (the value the service supplies when omitted).
	plain := mustAddItem(t, ctx, repo, list.ID, "eggs", 1, "amount", nil, false, testActor)
	if plain.Unit != "amount" {
		t.Errorf("unit = %q, want amount", plain.Unit)
	}

	// UpdateItem changes the unit.
	newUnit := "kg"

	updated := mustUpdateItem(t, ctx, repo, list.ID, item.ID, lists.ItemUpdate{
		Name:     nil,
		Quantity: nil,
		Unit:     &newUnit,
		NoteSet:  false,
		Note:     nil,
	})
	if updated.Unit != "kg" {
		t.Errorf("updated unit = %q, want kg", updated.Unit)
	}

	// check / uncheck preserve the unit.
	now := item.CreatedAt

	checked := mustSetItemChecked(t, ctx, repo, list.ID, item.ID, true, &now, &testActor)
	if checked.Unit != "kg" {
		t.Errorf("checked unit = %q, want kg (preserved)", checked.Unit)
	}

	unchecked := mustSetItemChecked(t, ctx, repo, list.ID, item.ID, false, nil, nil)
	if unchecked.Unit != "kg" {
		t.Errorf("unchecked unit = %q, want kg (preserved)", unchecked.Unit)
	}

	// delete -> restore preserves the unit.
	if err := repo.DeleteItem(ctx, list.ID, item.ID); err != nil {
		t.Fatalf("DeleteItem: %v", err)
	}

	restored := mustRestoreItem(t, ctx, repo, list.ID, item.ID)
	if restored.Unit != "kg" {
		t.Errorf("restored unit = %q, want kg (preserved)", restored.Unit)
	}
}
