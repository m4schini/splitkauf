// SPDX-License-Identifier: CC0-1.0

package db_test

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/m4schini/splitkauf/lists"
)

// TestCopyList pins the copy's contract: every non-deleted item comes across
// (open and checked alike), each reset to unchecked, with name/quantity/unit/
// note and the source's display order preserved — and the source untouched.
//
//nolint:paralleltest // integration tests share one database and truncate tables between tests
func TestCopyList(t *testing.T) {
	repo, ctx := newTestRepo(t)

	source := mustCreateList(t, ctx, repo, "Groceries", testActor)

	note := noteWhole

	open := mustAddItem(t, ctx, repo, source.ID, "milk", 2, "l", &note, false, testActor)
	checked := mustAddItem(t, ctx, repo, source.ID, "eggs", 12, "amount", nil, true, testActor)
	gone := mustAddItem(t, ctx, repo, source.ID, "beer", 6, "bottle", nil, false, testActor)

	if err := repo.DeleteItem(ctx, source.ID, gone.ID); err != nil {
		t.Fatalf("DeleteItem: %v", err)
	}

	copied := mustCopyList(t, ctx, repo, source.ID, "Groceries (copy)", testActor)
	if copied.ID == source.ID {
		t.Fatal("copy reused the source's id")
	}

	if copied.Name != "Groceries (copy)" {
		t.Errorf("copy name = %q, want %q", copied.Name, "Groceries (copy)")
	}
	// The returned summary counts every copied item as open.
	assertReturnedCounts(t, copied, 2, 0)
	assertCounts(t, repo, copied.ID, 2, 0)

	items := mustListItems(t, ctx, repo, copied.ID)
	if len(items) != 2 {
		t.Fatalf("copy has %d items, want 2 (the soft-deleted one must not travel): %+v", len(items), items)
	}
	// ListItems reads in display order (created_at, id): the staggered insert
	// timestamps must reproduce the source's order.
	if items[0].Name != "milk" || items[1].Name != "eggs" {
		t.Errorf("copy order = %q, %q, want milk, eggs", items[0].Name, items[1].Name)
	}

	assertItemFields(t, items[0], 2, "l", &note)
	assertItemFields(t, items[1], 12, "amount", nil)

	for _, item := range items {
		assertCopiedItemFresh(t, item, copied, []uuid.UUID{open.ID, checked.ID})
	}

	// The source is untouched: same items, eggs still checked.
	assertCounts(t, repo, source.ID, 1, 1)

	if src, err := repo.Item(ctx, source.ID, checked.ID); err != nil || !src.Checked {
		t.Errorf("source item after copy = %+v, %v; want still checked", src, err)
	}
}

// assertReturnedCounts compares the counts carried on an already-read list
// summary, without re-reading it.
func assertReturnedCounts(t *testing.T, list lists.List, open, checked int) {
	t.Helper()

	if list.OpenItemCount != open || list.CheckedItemCount != checked {
		t.Errorf("returned counts = %d/%d, want %d/%d", list.OpenItemCount, list.CheckedItemCount, open, checked)
	}
}

// assertItemFields compares an item's quantity, unit, and note.
func assertItemFields(t *testing.T, item lists.Item, quantity int, unit string, note *string) {
	t.Helper()

	if item.Quantity != quantity || item.Unit != unit {
		t.Errorf("item %q = qty %d unit %q, want qty %d unit %q", item.Name, item.Quantity, item.Unit, quantity, unit)
	}

	if !equalNote(item.Note, note) {
		t.Errorf("item %q note = %v, want %v", item.Name, item.Note, note)
	}
}

// equalNote compares two optional notes: both nil, or both set and equal.
func equalNote(got, want *string) bool {
	if got == nil || want == nil {
		return got == want
	}

	return *got == *want
}

// assertCopiedItemFresh verifies one copied item starts unchecked on the copy
// with a fresh id and a created_at no older than the list holding it.
func assertCopiedItemFresh(t *testing.T, item lists.Item, copied lists.List, sourceItemIDs []uuid.UUID) {
	t.Helper()

	if item.Checked || item.CheckedAt != nil {
		t.Errorf("copied item %q = checked %v / %v, want unchecked with nil CheckedAt",
			item.Name, item.Checked, item.CheckedAt)
	}

	if item.ListID != copied.ID {
		t.Errorf("copied item %q belongs to %v, want the copy %v", item.Name, item.ListID, copied.ID)
	}

	for _, sourceItemID := range sourceItemIDs {
		if item.ID == sourceItemID {
			t.Errorf("copied item %q reused the source item's id", item.Name)
		}
	}

	if item.CreatedAt.Before(copied.CreatedAt) {
		t.Errorf("copied item %q created_at %v predates its list %v", item.Name, item.CreatedAt, copied.CreatedAt)
	}
}

// TestCopyEmptyListAndMissingSource covers the copy's edges: an empty list
// copies to an empty list, and a source that does not exist is ErrNotFound
// (with no stray list left behind).
//
//nolint:paralleltest // integration tests share one database and truncate tables between tests
func TestCopyEmptyListAndMissingSource(t *testing.T) {
	repo, ctx := newTestRepo(t)

	empty := mustCreateList(t, ctx, repo, "Empty", testActor)

	copied := mustCopyList(t, ctx, repo, empty.ID, "Empty (copy)", testActor)
	assertReturnedCounts(t, copied, 0, 0)

	items := mustListItems(t, ctx, repo, copied.ID)
	if len(items) != 0 {
		t.Errorf("copy of an empty list has %d items, want 0", len(items))
	}

	if _, err := repo.CopyList(ctx, uuid.New(), "Ghost (copy)", testActor); !errors.Is(err, lists.ErrNotFound) {
		t.Fatalf("CopyList(missing) err = %v, want ErrNotFound", err)
	}
	// The failed copy rolled back: only the two lists above exist.
	all := mustLists(t, ctx, repo)
	if len(all) != 2 {
		t.Errorf("Lists = %d, want 2 (a failed copy must not leave a list behind): %+v", len(all), all)
	}
}

// TestCopyListItemAttribution: the copier adds every item on the copy, and none
// of them is bought — the source's buyers do not travel with the copy.
//
//nolint:paralleltest // integration tests share one database and truncate tables between tests
func TestCopyListItemAttribution(t *testing.T) {
	repo, ctx := newTestRepo(t)

	source := mustCreateList(t, ctx, repo, "Groceries", testActor)
	item := mustAddItem(t, ctx, repo, source.ID, "Milk", 1, "l", nil, false, testActor)

	now := time.Now()
	mustSetItemChecked(t, ctx, repo, source.ID, item.ID, true, &now, &testActor)

	copier := uuid.MustParse("22222222-2222-2222-2222-222222222222")

	copied := mustCopyList(t, ctx, repo, source.ID, "Groceries (copy)", copier)

	items := mustListItems(t, ctx, repo, copied.ID)
	if len(items) != 1 {
		t.Fatalf("copy has %d items, want 1", len(items))
	}

	assertActor(t, "AddedBy", items[0].AddedBy, copier, "")

	if items[0].BoughtBy != nil {
		t.Errorf("BoughtBy = %+v on a copied item, want nil", items[0].BoughtBy)
	}
}
