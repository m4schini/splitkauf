// SPDX-License-Identifier: TODO

package db_test

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/m4schini/splitkauf/adapters/db"
	"github.com/m4schini/splitkauf/lists"
)

// newTestRepo opens a repository against the DSN in SPLITKAUF_TEST_DATABASE_DSN,
// skipping the test when running with -short or when the DSN is unset. It
// TRUNCATEs the lists table (items cascade) so each test starts clean without
// dropping or recreating the schema.
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

	created, err := repo.CreateList(ctx, "Groceries")
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
	if _, err := repo.List(ctx, created.ID); err != lists.ErrNotFound {
		t.Errorf("List after delete err = %v, want ErrNotFound", err)
	}
}

func TestListNotFound(t *testing.T) {
	repo, ctx := newTestRepo(t)

	if _, err := repo.List(ctx, uuid.New()); err != lists.ErrNotFound {
		t.Errorf("List err = %v, want ErrNotFound", err)
	}
	if _, err := repo.RenameList(ctx, uuid.New(), "x"); err != lists.ErrNotFound {
		t.Errorf("RenameList err = %v, want ErrNotFound", err)
	}
	if err := repo.DeleteList(ctx, uuid.New()); err != lists.ErrNotFound {
		t.Errorf("DeleteList err = %v, want ErrNotFound", err)
	}
	if _, err := repo.AddItem(ctx, uuid.New(), "milk", 1, nil, false); err != lists.ErrNotFound {
		t.Errorf("AddItem err = %v, want ErrNotFound", err)
	}
}

func TestDeleteListCascadesItems(t *testing.T) {
	repo, ctx := newTestRepo(t)

	list, err := repo.CreateList(ctx, "Cascade")
	if err != nil {
		t.Fatalf("CreateList: %v", err)
	}
	if _, err := repo.AddItem(ctx, list.ID, "milk", 1, nil, false); err != nil {
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

	list, err := repo.CreateList(ctx, "Shopping")
	if err != nil {
		t.Fatalf("CreateList: %v", err)
	}

	note := "whole"
	item, err := repo.AddItem(ctx, list.ID, "milk", 2, &note, false)
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
	checked, err := repo.SetItemChecked(ctx, list.ID, item.ID, true, &now)
	if err != nil {
		t.Fatalf("SetItemChecked(true): %v", err)
	}
	if !checked.Checked || checked.CheckedAt == nil {
		t.Errorf("checked = %+v, want Checked with CheckedAt", checked)
	}
	assertCounts(t, repo, list.ID, 0, 1)

	// Uncheck it: back to open, checkedAt cleared.
	unchecked, err := repo.SetItemChecked(ctx, list.ID, item.ID, false, nil)
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
	if _, err := repo.Item(ctx, list.ID, item.ID); err != lists.ErrNotFound {
		t.Errorf("Item after delete err = %v, want ErrNotFound", err)
	}
	assertCounts(t, repo, list.ID, 0, 0)
}

func TestItemNotFound(t *testing.T) {
	repo, ctx := newTestRepo(t)

	list, err := repo.CreateList(ctx, "L")
	if err != nil {
		t.Fatalf("CreateList: %v", err)
	}
	if _, err := repo.Item(ctx, list.ID, uuid.New()); err != lists.ErrNotFound {
		t.Errorf("Item err = %v, want ErrNotFound", err)
	}
	name := "x"
	if _, err := repo.UpdateItem(ctx, list.ID, uuid.New(), lists.ItemUpdate{Name: &name}); err != lists.ErrNotFound {
		t.Errorf("UpdateItem err = %v, want ErrNotFound", err)
	}
	if err := repo.DeleteItem(ctx, list.ID, uuid.New()); err != lists.ErrNotFound {
		t.Errorf("DeleteItem err = %v, want ErrNotFound", err)
	}
	if _, err := repo.SetItemChecked(ctx, list.ID, uuid.New(), true, nil); err != lists.ErrNotFound {
		t.Errorf("SetItemChecked err = %v, want ErrNotFound", err)
	}
}

// TestSoftDeleteAndRestore is the round trip: a delete hides the item from reads
// and counts but keeps the row, and a restore brings it back with its state.
func TestSoftDeleteAndRestore(t *testing.T) {
	repo, ctx := newTestRepo(t)

	list, err := repo.CreateList(ctx, "Shopping")
	if err != nil {
		t.Fatalf("CreateList: %v", err)
	}
	item, err := repo.AddItem(ctx, list.ID, "milk", 3, nil, false)
	if err != nil {
		t.Fatalf("AddItem: %v", err)
	}
	assertCounts(t, repo, list.ID, 1, 0)

	// Soft delete: hidden from reads and counts.
	if err := repo.DeleteItem(ctx, list.ID, item.ID); err != nil {
		t.Fatalf("DeleteItem: %v", err)
	}
	if _, err := repo.Item(ctx, list.ID, item.ID); err != lists.ErrNotFound {
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
	if err := repo.DeleteItem(ctx, list.ID, item.ID); err != lists.ErrNotFound {
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
	deletedList, err := repo.CreateList(ctx, "All deleted")
	if err != nil {
		t.Fatalf("CreateList: %v", err)
	}
	item, err := repo.AddItem(ctx, deletedList.ID, "milk", 1, nil, false)
	if err != nil {
		t.Fatalf("AddItem: %v", err)
	}
	if err := repo.DeleteItem(ctx, deletedList.ID, item.ID); err != nil {
		t.Fatalf("DeleteItem: %v", err)
	}

	// An empty list (no items at all).
	emptyList, err := repo.CreateList(ctx, "Empty")
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

	list, err := repo.CreateList(ctx, "Shopping")
	if err != nil {
		t.Fatalf("CreateList: %v", err)
	}
	item, err := repo.AddItem(ctx, list.ID, "milk", 1, nil, false)
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

	if _, err := repo.RestoreItem(ctx, list.ID, uuid.New()); err != lists.ErrNotFound {
		t.Errorf("RestoreItem(missing) err = %v, want ErrNotFound", err)
	}
}

// TestAddItemChecked verifies creating an item with checked=true stamps
// checked_at and reflects the checked count.
func TestAddItemChecked(t *testing.T) {
	repo, ctx := newTestRepo(t)

	list, err := repo.CreateList(ctx, "Shopping")
	if err != nil {
		t.Fatalf("CreateList: %v", err)
	}
	item, err := repo.AddItem(ctx, list.ID, "milk", 1, nil, true)
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

	if _, err := repo.AddItem(ctx, uuid.New(), "milk", 1, nil, false); err != lists.ErrNotFound {
		t.Errorf("AddItem to nonexistent list err = %v, want ErrNotFound", err)
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
