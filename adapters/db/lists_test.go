// SPDX-License-Identifier: CC0-1.0

package db_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/m4schini/splitkauf/adapters/db"
	"github.com/m4schini/splitkauf/lists"
	"github.com/m4schini/splitkauf/members"
)

// newTestRepo opens a repository against the DSN in SPLITKAUF_TEST_DATABASE_DSN,
// skipping the test when running with -short or when the DSN is unset. It
// TRUNCATEs the lists table (items cascade) so each test starts clean without
// dropping or recreating the schema.
// testActor stands in for the authenticated user whose id the REST layer
// passes down to every attributing write.
var testActor = uuid.MustParse("11111111-1111-1111-1111-111111111111")

func newTestRepo(t *testing.T) (*db.ListsRepository, context.Context) {
	t.Helper()

	if testing.Short() {
		t.Skip("skipping integration test in -short mode")
	}

	dsn := os.Getenv("SPLITKAUF_TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("SPLITKAUF_TEST_DATABASE_DSN not set; skipping integration test")
	}

	conn, err := db.NewSQL(dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}

	t.Cleanup(func() { _ = conn.Close() })

	if _, err := conn.ExecContext(context.Background(), `TRUNCATE TABLE lists CASCADE`); err != nil {
		t.Fatalf("truncate: %v", err)
	}

	return db.NewListsRepository(conn), context.Background()
}

func TestListLifecycle(t *testing.T) {
	repo, ctx := newTestRepo(t)

	created, err := repo.CreateList(ctx, "Groceries", testActor)
	if err != nil {
		t.Fatalf("CreateList: %v", err)
	}

	if created.Name != "Groceries" {
		t.Errorf("name = %q, want %q", created.Name, "Groceries")
	}

	if created.OpenItemCount != 0 || created.CheckedItemCount != 0 {
		t.Errorf("counts = %d/%d, want 0/0", created.OpenItemCount, created.CheckedItemCount)
	}

	all, err := repo.Lists(ctx)
	if err != nil {
		t.Fatalf("Lists: %v", err)
	}

	if len(all) != 1 || all[0].ID != created.ID {
		t.Fatalf("Lists = %+v, want the one created", all)
	}

	got, err := repo.List(ctx, created.ID)
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	if got.ID != created.ID {
		t.Errorf("List id = %v, want %v", got.ID, created.ID)
	}

	renamed, err := repo.RenameList(ctx, created.ID, "Weekly Groceries")
	if err != nil {
		t.Fatalf("RenameList: %v", err)
	}

	if renamed.Name != "Weekly Groceries" {
		t.Errorf("renamed name = %q, want %q", renamed.Name, "Weekly Groceries")
	}

	if err := repo.DeleteList(ctx, created.ID); err != nil {
		t.Fatalf("DeleteList: %v", err)
	}

	if _, err := repo.List(ctx, created.ID); !errors.Is(err, lists.ErrNotFound) {
		t.Errorf("List after delete err = %v, want ErrNotFound", err)
	}
}

func TestListNotFound(t *testing.T) {
	repo, ctx := newTestRepo(t)

	if _, err := repo.List(ctx, uuid.New()); !errors.Is(err, lists.ErrNotFound) {
		t.Errorf("List err = %v, want ErrNotFound", err)
	}

	if _, err := repo.RenameList(ctx, uuid.New(), "x"); !errors.Is(err, lists.ErrNotFound) {
		t.Errorf("RenameList err = %v, want ErrNotFound", err)
	}

	if err := repo.DeleteList(ctx, uuid.New()); !errors.Is(err, lists.ErrNotFound) {
		t.Errorf("DeleteList err = %v, want ErrNotFound", err)
	}

	if _, err := repo.AddItem(ctx, uuid.New(), "milk", 1, "amount", nil, false, testActor); !errors.Is(err, lists.ErrNotFound) {
		t.Errorf("AddItem err = %v, want ErrNotFound", err)
	}
}

func TestDeleteListCascadesItems(t *testing.T) {
	repo, ctx := newTestRepo(t)

	list, err := repo.CreateList(ctx, "Cascade", testActor)
	if err != nil {
		t.Fatalf("CreateList: %v", err)
	}

	if _, err := repo.AddItem(ctx, list.ID, "milk", 1, "amount", nil, false, testActor); err != nil {
		t.Fatalf("AddItem: %v", err)
	}

	if err := repo.DeleteList(ctx, list.ID); err != nil {
		t.Fatalf("DeleteList: %v", err)
	}

	items, err := repo.ListItems(ctx, list.ID)
	if err != nil {
		t.Fatalf("ListItems: %v", err)
	}

	if len(items) != 0 {
		t.Errorf("items after cascade = %d, want 0", len(items))
	}
}

func TestItemLifecycleAndCounts(t *testing.T) {
	repo, ctx := newTestRepo(t)

	list, err := repo.CreateList(ctx, "Shopping", testActor)
	if err != nil {
		t.Fatalf("CreateList: %v", err)
	}

	note := "whole"

	item, err := repo.AddItem(ctx, list.ID, "milk", 2, "amount", &note, false, testActor)
	if err != nil {
		t.Fatalf("AddItem: %v", err)
	}

	if item.Quantity != 2 {
		t.Errorf("quantity = %d, want 2", item.Quantity)
	}

	if item.Note == nil || *item.Note != "whole" {
		t.Errorf("note = %v, want %q", item.Note, "whole")
	}

	if item.Checked {
		t.Error("new item should be unchecked")
	}

	// One open item.
	assertCounts(t, repo, list.ID, 1, 0)

	// Update name, quantity, and clear the note.
	newName := "oat milk"
	newQty := 5

	updated, err := repo.UpdateItem(ctx, list.ID, item.ID, lists.ItemUpdate{
		Name:     &newName,
		Quantity: &newQty,
		NoteSet:  true,
		Note:     nil,
	})
	if err != nil {
		t.Fatalf("UpdateItem: %v", err)
	}

	if updated.Name != "oat milk" || updated.Quantity != 5 || updated.Note != nil {
		t.Errorf("updated = %+v, want name=oat milk qty=5 note=nil", updated)
	}

	// Check it: counts flip to one checked.
	now := item.CreatedAt

	checked, err := repo.SetItemChecked(ctx, list.ID, item.ID, true, &now, &testActor)
	if err != nil {
		t.Fatalf("SetItemChecked(true): %v", err)
	}

	if !checked.Checked || checked.CheckedAt == nil {
		t.Errorf("checked = %+v, want Checked with CheckedAt", checked)
	}

	assertCounts(t, repo, list.ID, 0, 1)

	// Uncheck it: back to open, checkedAt cleared.
	unchecked, err := repo.SetItemChecked(ctx, list.ID, item.ID, false, nil, nil)
	if err != nil {
		t.Fatalf("SetItemChecked(false): %v", err)
	}

	if unchecked.Checked || unchecked.CheckedAt != nil {
		t.Errorf("unchecked = %+v, want open with nil CheckedAt", unchecked)
	}

	assertCounts(t, repo, list.ID, 1, 0)

	// Delete the item.
	if err := repo.DeleteItem(ctx, list.ID, item.ID); err != nil {
		t.Fatalf("DeleteItem: %v", err)
	}

	if _, err := repo.Item(ctx, list.ID, item.ID); !errors.Is(err, lists.ErrNotFound) {
		t.Errorf("Item after delete err = %v, want ErrNotFound", err)
	}

	assertCounts(t, repo, list.ID, 0, 0)
}

func TestItemNotFound(t *testing.T) {
	repo, ctx := newTestRepo(t)

	list, err := repo.CreateList(ctx, "L", testActor)
	if err != nil {
		t.Fatalf("CreateList: %v", err)
	}

	if _, err := repo.Item(ctx, list.ID, uuid.New()); !errors.Is(err, lists.ErrNotFound) {
		t.Errorf("Item err = %v, want ErrNotFound", err)
	}

	name := "x"
	if _, err := repo.UpdateItem(ctx, list.ID, uuid.New(), lists.ItemUpdate{Name: &name}); !errors.Is(err, lists.ErrNotFound) {
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
func TestSoftDeleteAndRestore(t *testing.T) {
	repo, ctx := newTestRepo(t)

	list, err := repo.CreateList(ctx, "Shopping", testActor)
	if err != nil {
		t.Fatalf("CreateList: %v", err)
	}

	item, err := repo.AddItem(ctx, list.ID, "milk", 3, "amount", nil, false, testActor)
	if err != nil {
		t.Fatalf("AddItem: %v", err)
	}

	assertCounts(t, repo, list.ID, 1, 0)

	// Soft delete: hidden from reads and counts.
	if err := repo.DeleteItem(ctx, list.ID, item.ID); err != nil {
		t.Fatalf("DeleteItem: %v", err)
	}

	if _, err := repo.Item(ctx, list.ID, item.ID); !errors.Is(err, lists.ErrNotFound) {
		t.Errorf("Item after soft delete err = %v, want ErrNotFound", err)
	}

	items, err := repo.ListItems(ctx, list.ID)
	if err != nil {
		t.Fatalf("ListItems: %v", err)
	}

	if len(items) != 0 {
		t.Errorf("ListItems after soft delete = %d, want 0", len(items))
	}

	assertCounts(t, repo, list.ID, 0, 0)

	// Deleting again is a no-op ErrNotFound (already deleted).
	if err := repo.DeleteItem(ctx, list.ID, item.ID); !errors.Is(err, lists.ErrNotFound) {
		t.Errorf("second DeleteItem err = %v, want ErrNotFound", err)
	}

	// Restore: back in reads and counts, state preserved.
	restored, err := repo.RestoreItem(ctx, list.ID, item.ID)
	if err != nil {
		t.Fatalf("RestoreItem: %v", err)
	}

	if restored.ID != item.ID || restored.Name != "milk" || restored.Quantity != 3 {
		t.Errorf("restored = %+v, want the original milk/3", restored)
	}

	if _, err := repo.Item(ctx, list.ID, item.ID); err != nil {
		t.Errorf("Item after restore err = %v, want nil", err)
	}

	assertCounts(t, repo, list.ID, 1, 0)
}

// TestListWithAllItemsDeletedStillAppears pins the LEFT JOIN ON fix: a list whose
// items are all soft-deleted (and an empty list) must still appear in Lists() and
// List() with 0/0 counts — a WHERE deleted_at filter would drop them.
func TestListWithAllItemsDeletedStillAppears(t *testing.T) {
	repo, ctx := newTestRepo(t)

	// A list whose only item is soft-deleted.
	deletedList, err := repo.CreateList(ctx, "All deleted", testActor)
	if err != nil {
		t.Fatalf("CreateList: %v", err)
	}

	item, err := repo.AddItem(ctx, deletedList.ID, "milk", 1, "amount", nil, false, testActor)
	if err != nil {
		t.Fatalf("AddItem: %v", err)
	}

	if err := repo.DeleteItem(ctx, deletedList.ID, item.ID); err != nil {
		t.Fatalf("DeleteItem: %v", err)
	}

	// An empty list (no items at all).
	emptyList, err := repo.CreateList(ctx, "Empty", testActor)
	if err != nil {
		t.Fatalf("CreateList: %v", err)
	}

	// Both still resolve via List() with 0/0 counts.
	for _, id := range []uuid.UUID{deletedList.ID, emptyList.ID} {
		got, err := repo.List(ctx, id)
		if err != nil {
			t.Fatalf("List(%v) err = %v, want it to still appear", id, err)
		}

		if got.OpenItemCount != 0 || got.CheckedItemCount != 0 {
			t.Errorf("List(%v) counts = %d/%d, want 0/0", id, got.OpenItemCount, got.CheckedItemCount)
		}
	}

	// And both appear in Lists().
	all, err := repo.Lists(ctx)
	if err != nil {
		t.Fatalf("Lists: %v", err)
	}

	seen := map[uuid.UUID]bool{}
	for _, l := range all {
		seen[l.ID] = true
	}

	if !seen[deletedList.ID] {
		t.Error("Lists() dropped the list whose items are all soft-deleted")
	}

	if !seen[emptyList.ID] {
		t.Error("Lists() dropped the empty list")
	}
}

// TestRestoreNeverDeletedIsIdempotent verifies restoring an item that was never
// deleted succeeds and returns it unchanged, while a missing row is ErrNotFound.
func TestRestoreNeverDeletedIsIdempotent(t *testing.T) {
	repo, ctx := newTestRepo(t)

	list, err := repo.CreateList(ctx, "Shopping", testActor)
	if err != nil {
		t.Fatalf("CreateList: %v", err)
	}

	item, err := repo.AddItem(ctx, list.ID, "milk", 1, "amount", nil, false, testActor)
	if err != nil {
		t.Fatalf("AddItem: %v", err)
	}

	restored, err := repo.RestoreItem(ctx, list.ID, item.ID)
	if err != nil {
		t.Fatalf("RestoreItem (never deleted): %v", err)
	}

	if restored.ID != item.ID {
		t.Errorf("restored id = %v, want %v", restored.ID, item.ID)
	}

	if _, err := repo.RestoreItem(ctx, list.ID, uuid.New()); !errors.Is(err, lists.ErrNotFound) {
		t.Errorf("RestoreItem(missing) err = %v, want ErrNotFound", err)
	}
}

// TestAddItemChecked verifies creating an item with checked=true stamps
// checked_at and reflects the checked count.
func TestAddItemChecked(t *testing.T) {
	repo, ctx := newTestRepo(t)

	list, err := repo.CreateList(ctx, "Shopping", testActor)
	if err != nil {
		t.Fatalf("CreateList: %v", err)
	}

	item, err := repo.AddItem(ctx, list.ID, "milk", 1, "amount", nil, true, testActor)
	if err != nil {
		t.Fatalf("AddItem(checked): %v", err)
	}

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
func TestAddItemNonexistentListReturnsNotFound(t *testing.T) {
	repo, ctx := newTestRepo(t)

	if _, err := repo.AddItem(ctx, uuid.New(), "milk", 1, "amount", nil, false, testActor); !errors.Is(err, lists.ErrNotFound) {
		t.Errorf("AddItem to nonexistent list err = %v, want ErrNotFound", err)
	}
}

// TestItemUnitRoundTrip pins the items.unit column end to end: a non-default
// unit survives insert and read, the domain default ("amount") round-trips, an
// UpdateItem changes the unit, and the state transitions (check/uncheck/delete
// -> restore) preserve it rather than clobbering it.
func TestItemUnitRoundTrip(t *testing.T) {
	repo, ctx := newTestRepo(t)

	list, err := repo.CreateList(ctx, "Shopping", testActor)
	if err != nil {
		t.Fatalf("CreateList: %v", err)
	}

	// Add-with-unit round trip.
	item, err := repo.AddItem(ctx, list.ID, "milk", 2, "l", nil, false, testActor)
	if err != nil {
		t.Fatalf("AddItem: %v", err)
	}

	if item.Unit != "l" {
		t.Errorf("unit = %q, want l", item.Unit)
	}

	got, err := repo.Item(ctx, list.ID, item.ID)
	if err != nil {
		t.Fatalf("Item: %v", err)
	}

	if got.Unit != "l" {
		t.Errorf("read unit = %q, want l", got.Unit)
	}

	// Default "amount" round-trips (the value the service supplies when omitted).
	plain, err := repo.AddItem(ctx, list.ID, "eggs", 1, "amount", nil, false, testActor)
	if err != nil {
		t.Fatalf("AddItem(amount): %v", err)
	}

	if plain.Unit != "amount" {
		t.Errorf("unit = %q, want amount", plain.Unit)
	}

	// UpdateItem changes the unit.
	newUnit := "kg"

	updated, err := repo.UpdateItem(ctx, list.ID, item.ID, lists.ItemUpdate{Unit: &newUnit})
	if err != nil {
		t.Fatalf("UpdateItem: %v", err)
	}

	if updated.Unit != "kg" {
		t.Errorf("updated unit = %q, want kg", updated.Unit)
	}

	// check / uncheck preserve the unit.
	now := item.CreatedAt

	checked, err := repo.SetItemChecked(ctx, list.ID, item.ID, true, &now, &testActor)
	if err != nil {
		t.Fatalf("SetItemChecked(true): %v", err)
	}

	if checked.Unit != "kg" {
		t.Errorf("checked unit = %q, want kg (preserved)", checked.Unit)
	}

	unchecked, err := repo.SetItemChecked(ctx, list.ID, item.ID, false, nil, nil)
	if err != nil {
		t.Fatalf("SetItemChecked(false): %v", err)
	}

	if unchecked.Unit != "kg" {
		t.Errorf("unchecked unit = %q, want kg (preserved)", unchecked.Unit)
	}

	// delete -> restore preserves the unit.
	if err := repo.DeleteItem(ctx, list.ID, item.ID); err != nil {
		t.Fatalf("DeleteItem: %v", err)
	}

	restored, err := repo.RestoreItem(ctx, list.ID, item.ID)
	if err != nil {
		t.Fatalf("RestoreItem: %v", err)
	}

	if restored.Unit != "kg" {
		t.Errorf("restored unit = %q, want kg (preserved)", restored.Unit)
	}
}

// TestCopyList pins the copy's contract: every non-deleted item comes across
// (open and checked alike), each reset to unchecked, with name/quantity/unit/
// note and the source's display order preserved — and the source untouched.
func TestCopyList(t *testing.T) {
	repo, ctx := newTestRepo(t)

	source, err := repo.CreateList(ctx, "Groceries", testActor)
	if err != nil {
		t.Fatalf("CreateList: %v", err)
	}

	note := "whole"

	open, err := repo.AddItem(ctx, source.ID, "milk", 2, "l", &note, false, testActor)
	if err != nil {
		t.Fatalf("AddItem(milk): %v", err)
	}

	checked, err := repo.AddItem(ctx, source.ID, "eggs", 12, "amount", nil, true, testActor)
	if err != nil {
		t.Fatalf("AddItem(eggs): %v", err)
	}

	gone, err := repo.AddItem(ctx, source.ID, "beer", 6, "bottle", nil, false, testActor)
	if err != nil {
		t.Fatalf("AddItem(beer): %v", err)
	}

	if err := repo.DeleteItem(ctx, source.ID, gone.ID); err != nil {
		t.Fatalf("DeleteItem: %v", err)
	}

	copied, err := repo.CopyList(ctx, source.ID, "Groceries (copy)", testActor)
	if err != nil {
		t.Fatalf("CopyList: %v", err)
	}

	if copied.ID == source.ID {
		t.Fatal("copy reused the source's id")
	}

	if copied.Name != "Groceries (copy)" {
		t.Errorf("copy name = %q, want %q", copied.Name, "Groceries (copy)")
	}
	// The returned summary counts every copied item as open.
	if copied.OpenItemCount != 2 || copied.CheckedItemCount != 0 {
		t.Errorf("returned counts = %d/%d, want 2/0", copied.OpenItemCount, copied.CheckedItemCount)
	}

	assertCounts(t, repo, copied.ID, 2, 0)

	items, err := repo.ListItems(ctx, copied.ID)
	if err != nil {
		t.Fatalf("ListItems(copy): %v", err)
	}

	if len(items) != 2 {
		t.Fatalf("copy has %d items, want 2 (the soft-deleted one must not travel): %+v", len(items), items)
	}
	// ListItems reads in display order (created_at, id): the staggered insert
	// timestamps must reproduce the source's order.
	if items[0].Name != "milk" || items[1].Name != "eggs" {
		t.Errorf("copy order = %q, %q, want milk, eggs", items[0].Name, items[1].Name)
	}

	if items[0].Quantity != 2 || items[0].Unit != "l" || items[0].Note == nil || *items[0].Note != "whole" {
		t.Errorf("copied milk = %+v, want qty 2 unit l note whole", items[0])
	}

	if items[1].Quantity != 12 || items[1].Unit != "amount" || items[1].Note != nil {
		t.Errorf("copied eggs = %+v, want qty 12 unit amount no note", items[1])
	}

	for _, it := range items {
		if it.Checked || it.CheckedAt != nil {
			t.Errorf("copied item %q = checked %v / %v, want unchecked with nil CheckedAt", it.Name, it.Checked, it.CheckedAt)
		}

		if it.ListID != copied.ID {
			t.Errorf("copied item %q belongs to %v, want the copy %v", it.Name, it.ListID, copied.ID)
		}

		if it.ID == open.ID || it.ID == checked.ID {
			t.Errorf("copied item %q reused the source item's id", it.Name)
		}

		if it.CreatedAt.Before(copied.CreatedAt) {
			t.Errorf("copied item %q created_at %v predates its list %v", it.Name, it.CreatedAt, copied.CreatedAt)
		}
	}

	// The source is untouched: same items, eggs still checked.
	assertCounts(t, repo, source.ID, 1, 1)

	if src, err := repo.Item(ctx, source.ID, checked.ID); err != nil || !src.Checked {
		t.Errorf("source item after copy = %+v, %v; want still checked", src, err)
	}
}

// TestCopyEmptyListAndMissingSource covers the copy's edges: an empty list
// copies to an empty list, and a source that does not exist is ErrNotFound
// (with no stray list left behind).
func TestCopyEmptyListAndMissingSource(t *testing.T) {
	repo, ctx := newTestRepo(t)

	empty, err := repo.CreateList(ctx, "Empty", testActor)
	if err != nil {
		t.Fatalf("CreateList: %v", err)
	}

	copied, err := repo.CopyList(ctx, empty.ID, "Empty (copy)", testActor)
	if err != nil {
		t.Fatalf("CopyList(empty): %v", err)
	}

	if copied.OpenItemCount != 0 || copied.CheckedItemCount != 0 {
		t.Errorf("counts = %d/%d, want 0/0", copied.OpenItemCount, copied.CheckedItemCount)
	}

	items, err := repo.ListItems(ctx, copied.ID)
	if err != nil {
		t.Fatalf("ListItems: %v", err)
	}

	if len(items) != 0 {
		t.Errorf("copy of an empty list has %d items, want 0", len(items))
	}

	if _, err := repo.CopyList(ctx, uuid.New(), "Ghost (copy)", testActor); !errors.Is(err, lists.ErrNotFound) {
		t.Fatalf("CopyList(missing) err = %v, want ErrNotFound", err)
	}
	// The failed copy rolled back: only the two lists above exist.
	all, err := repo.Lists(ctx)
	if err != nil {
		t.Fatalf("Lists: %v", err)
	}

	if len(all) != 2 {
		t.Errorf("Lists = %d, want 2 (a failed copy must not leave a list behind): %+v", len(all), all)
	}
}

func assertCounts(t *testing.T, repo *db.ListsRepository, id uuid.UUID, open, checked int) {
	t.Helper()

	l, err := repo.List(context.Background(), id)
	if err != nil {
		t.Fatalf("List for counts: %v", err)
	}

	if l.OpenItemCount != open || l.CheckedItemCount != checked {
		t.Errorf("counts = %d/%d, want %d/%d", l.OpenItemCount, l.CheckedItemCount, open, checked)
	}
}

// TestListAttribution pins the read-time name resolution (US-L.11): the
// creator's id is stored on the list, the display name comes from the members
// join, and a rename of that member changes what past lists report — which is
// the whole reason the name is not snapshotted at write time.
func TestListAttribution(t *testing.T) {
	memberRepo, ctx := newTestMemberRepo(t)
	repo, _ := newTestRepo(t)

	if err := memberRepo.Upsert(ctx, members.Member{
		Subject: "subject-for-" + testActor.String(),
		UserID:  testActor,
		Name:    "Alice",
	}); err != nil {
		t.Fatalf("Upsert member: %v", err)
	}

	created, err := repo.CreateList(ctx, "Groceries", testActor)
	if err != nil {
		t.Fatalf("CreateList: %v", err)
	}

	if created.CreatedBy == nil {
		t.Fatal("CreatedBy is nil, want the acting user")
	}

	if created.CreatedBy.ID != testActor || created.CreatedBy.Name != "Alice" {
		t.Errorf("CreatedBy = %+v, want {%v Alice}", created.CreatedBy, testActor)
	}

	// A copy is credited to the copier, and the overview read resolves the name
	// through the same join.
	copied, err := repo.CopyList(ctx, created.ID, "Groceries (copy)", testActor)
	if err != nil {
		t.Fatalf("CopyList: %v", err)
	}

	if copied.CreatedBy == nil || copied.CreatedBy.Name != "Alice" {
		t.Errorf("copy CreatedBy = %+v, want Alice", copied.CreatedBy)
	}

	// Rename the member: every past attribution must follow.
	if err := memberRepo.Upsert(ctx, members.Member{
		Subject: "subject-for-" + testActor.String(),
		UserID:  testActor,
		Name:    "Alice Cooper",
	}); err != nil {
		t.Fatalf("Upsert member (rename): %v", err)
	}

	all, err := repo.Lists(ctx)
	if err != nil {
		t.Fatalf("Lists: %v", err)
	}

	if len(all) != 2 {
		t.Fatalf("Lists = %d, want 2", len(all))
	}

	for _, l := range all {
		if l.CreatedBy == nil || l.CreatedBy.Name != "Alice Cooper" {
			t.Errorf("list %q CreatedBy = %+v, want the renamed Alice Cooper", l.Name, l.CreatedBy)
		}
	}

	// An actor with no member row still yields an attribution — the id is known,
	// only the name is not. The UI hides the line unless the id is the viewer's.
	stranger := uuid.MustParse("33333333-3333-3333-3333-333333333333")

	orphan, err := repo.CreateList(ctx, "Orphan", stranger)
	if err != nil {
		t.Fatalf("CreateList (unknown member): %v", err)
	}

	if orphan.CreatedBy == nil || orphan.CreatedBy.ID != stranger || orphan.CreatedBy.Name != "" {
		t.Errorf("CreatedBy = %+v, want id %v with an empty name", orphan.CreatedBy, stranger)
	}
}

// TestItemAttribution pins the item attributions against real SQL: the adder
// survives every subsequent write, the buyer is written on check and actually
// cleared (not merely hidden) on uncheck, and both names resolve through the
// members join.
func TestItemAttribution(t *testing.T) {
	memberRepo, ctx := newTestMemberRepo(t)
	repo, _ := newTestRepo(t)

	buyer := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	for _, m := range []members.Member{
		{Subject: "adder", UserID: testActor, Name: "Alice"},
		{Subject: "buyer", UserID: buyer, Name: "Maria"},
	} {
		if err := memberRepo.Upsert(ctx, m); err != nil {
			t.Fatalf("Upsert member %s: %v", m.Name, err)
		}
	}

	list, err := repo.CreateList(ctx, "Groceries", testActor)
	if err != nil {
		t.Fatalf("CreateList: %v", err)
	}

	item, err := repo.AddItem(ctx, list.ID, "Milk", 1, "l", nil, false, testActor)
	if err != nil {
		t.Fatalf("AddItem: %v", err)
	}

	if item.AddedBy == nil || item.AddedBy.Name != "Alice" {
		t.Fatalf("AddedBy = %+v, want Alice", item.AddedBy)
	}

	if item.BoughtBy != nil {
		t.Errorf("BoughtBy = %+v on a new open item, want nil", item.BoughtBy)
	}

	now := time.Now()

	checked, err := repo.SetItemChecked(ctx, list.ID, item.ID, true, &now, &buyer)
	if err != nil {
		t.Fatalf("SetItemChecked(true): %v", err)
	}

	if checked.BoughtBy == nil || checked.BoughtBy.Name != "Maria" {
		t.Fatalf("BoughtBy = %+v, want Maria", checked.BoughtBy)
	}

	if checked.AddedBy == nil || checked.AddedBy.Name != "Alice" {
		t.Errorf("AddedBy = %+v, want Alice preserved through the check", checked.AddedBy)
	}

	// An edit must not disturb either attribution — the write path re-reads the
	// row rather than returning a RETURNING projection that cannot join.
	updated, err := repo.UpdateItem(ctx, list.ID, item.ID, lists.ItemUpdate{Name: new("Oat milk")})
	if err != nil {
		t.Fatalf("UpdateItem: %v", err)
	}

	if updated.AddedBy == nil || updated.AddedBy.Name != "Alice" ||
		updated.BoughtBy == nil || updated.BoughtBy.Name != "Maria" {
		t.Errorf("attributions lost on update: added=%+v bought=%+v", updated.AddedBy, updated.BoughtBy)
	}

	unchecked, err := repo.SetItemChecked(ctx, list.ID, item.ID, false, nil, nil)
	if err != nil {
		t.Fatalf("SetItemChecked(false): %v", err)
	}

	if unchecked.BoughtBy != nil {
		t.Errorf("BoughtBy = %+v after uncheck, want nil", unchecked.BoughtBy)
	}
	// Re-read independently: the buyer must be gone from the row itself, not
	// just absent from the response the write happened to build.
	reread, err := repo.Item(ctx, list.ID, item.ID)
	if err != nil {
		t.Fatalf("Item: %v", err)
	}

	if reread.BoughtBy != nil {
		t.Errorf("bought_by still set in the database: %+v", reread.BoughtBy)
	}

	if reread.AddedBy == nil || reread.AddedBy.Name != "Alice" {
		t.Errorf("AddedBy = %+v, want Alice preserved across the uncheck", reread.AddedBy)
	}
}

// TestAddItemCheckedStampsBuyer covers the offline fold: a create that arrives
// already checked credits its adder as the buyer too, because no separate check
// call will ever follow to record one.
func TestAddItemCheckedStampsBuyer(t *testing.T) {
	repo, ctx := newTestRepo(t)

	list, err := repo.CreateList(ctx, "Groceries", testActor)
	if err != nil {
		t.Fatalf("CreateList: %v", err)
	}

	item, err := repo.AddItem(ctx, list.ID, "Milk", 1, "l", nil, true, testActor)
	if err != nil {
		t.Fatalf("AddItem(checked): %v", err)
	}

	if item.AddedBy == nil || item.AddedBy.ID != testActor {
		t.Errorf("AddedBy = %+v, want %v", item.AddedBy, testActor)
	}

	if item.BoughtBy == nil || item.BoughtBy.ID != testActor {
		t.Errorf("BoughtBy = %+v, want the adder %v", item.BoughtBy, testActor)
	}
}

// TestCopyListItemAttribution: the copier adds every item on the copy, and none
// of them is bought — the source's buyers do not travel with the copy.
func TestCopyListItemAttribution(t *testing.T) {
	repo, ctx := newTestRepo(t)

	source, err := repo.CreateList(ctx, "Groceries", testActor)
	if err != nil {
		t.Fatalf("CreateList: %v", err)
	}

	item, err := repo.AddItem(ctx, source.ID, "Milk", 1, "l", nil, false, testActor)
	if err != nil {
		t.Fatalf("AddItem: %v", err)
	}

	now := time.Now()
	if _, err := repo.SetItemChecked(ctx, source.ID, item.ID, true, &now, &testActor); err != nil {
		t.Fatalf("SetItemChecked: %v", err)
	}

	copier := uuid.MustParse("22222222-2222-2222-2222-222222222222")

	copied, err := repo.CopyList(ctx, source.ID, "Groceries (copy)", copier)
	if err != nil {
		t.Fatalf("CopyList: %v", err)
	}

	items, err := repo.ListItems(ctx, copied.ID)
	if err != nil {
		t.Fatalf("ListItems: %v", err)
	}

	if len(items) != 1 {
		t.Fatalf("copy has %d items, want 1", len(items))
	}

	if items[0].AddedBy == nil || items[0].AddedBy.ID != copier {
		t.Errorf("AddedBy = %+v, want the copier %v", items[0].AddedBy, copier)
	}

	if items[0].BoughtBy != nil {
		t.Errorf("BoughtBy = %+v on a copied item, want nil", items[0].BoughtBy)
	}
}

func ptrString(s string) *string { return new(s) }
