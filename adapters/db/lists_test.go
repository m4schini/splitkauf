// SPDX-License-Identifier: CC0-1.0

package db_test

import (
	"context"
	"errors"
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

//nolint:paralleltest // integration tests share one database and truncate tables between tests
func TestListLifecycle(t *testing.T) {
	repo, ctx := newTestRepo(t)

	created := mustCreateList(t, ctx, repo, "Groceries", testActor)
	if created.Name != "Groceries" {
		t.Errorf("name = %q, want %q", created.Name, "Groceries")
	}

	assertCounts(t, repo, created.ID, 0, 0)

	all := mustLists(t, ctx, repo)
	if len(all) != 1 || all[0].ID != created.ID {
		t.Fatalf("Lists = %+v, want the one created", all)
	}

	got := mustGetList(t, ctx, repo, created.ID)
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

//nolint:paralleltest // integration tests share one database and truncate tables between tests
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

	_, err := repo.AddItem(ctx, uuid.New(), "milk", 1, "amount", nil, false, testActor)
	if !errors.Is(err, lists.ErrNotFound) {
		t.Errorf("AddItem err = %v, want ErrNotFound", err)
	}
}

//nolint:paralleltest // integration tests share one database and truncate tables between tests
func TestDeleteListCascadesItems(t *testing.T) {
	repo, ctx := newTestRepo(t)

	list := mustCreateList(t, ctx, repo, "Cascade", testActor)
	mustAddItem(t, ctx, repo, list.ID, "milk", 1, "amount", nil, false, testActor)

	if err := repo.DeleteList(ctx, list.ID); err != nil {
		t.Fatalf("DeleteList: %v", err)
	}

	items := mustListItems(t, ctx, repo, list.ID)
	if len(items) != 0 {
		t.Errorf("items after cascade = %d, want 0", len(items))
	}
}

// TestListWithAllItemsDeletedStillAppears pins the LEFT JOIN ON fix: a list whose
// items are all soft-deleted (and an empty list) must still appear in Lists() and
// List() with 0/0 counts — a WHERE deleted_at filter would drop them.
//
//nolint:paralleltest // integration tests share one database and truncate tables between tests
func TestListWithAllItemsDeletedStillAppears(t *testing.T) {
	repo, ctx := newTestRepo(t)

	// A list whose only item is soft-deleted.
	deletedList := mustCreateList(t, ctx, repo, "All deleted", testActor)
	item := mustAddItem(t, ctx, repo, deletedList.ID, "milk", 1, "amount", nil, false, testActor)

	if err := repo.DeleteItem(ctx, deletedList.ID, item.ID); err != nil {
		t.Fatalf("DeleteItem: %v", err)
	}

	// An empty list (no items at all).
	emptyList := mustCreateList(t, ctx, repo, "Empty", testActor)

	// Both still resolve via List() with 0/0 counts.
	for _, listID := range []uuid.UUID{deletedList.ID, emptyList.ID} {
		got := mustGetList(t, ctx, repo, listID)
		if got.OpenItemCount != 0 || got.CheckedItemCount != 0 {
			t.Errorf("List(%v) counts = %d/%d, want 0/0", listID, got.OpenItemCount, got.CheckedItemCount)
		}
	}

	// And both appear in Lists().
	all := mustLists(t, ctx, repo)

	seen := map[uuid.UUID]bool{}
	for _, list := range all {
		seen[list.ID] = true
	}

	if !seen[deletedList.ID] {
		t.Error("Lists() dropped the list whose items are all soft-deleted")
	}

	if !seen[emptyList.ID] {
		t.Error("Lists() dropped the empty list")
	}
}

// assertCounts re-reads the list and compares its open/checked item counts.
func assertCounts(t *testing.T, repo *db.ListsRepository, listID uuid.UUID, open, checked int) {
	t.Helper()

	list, err := repo.List(context.Background(), listID)
	if err != nil {
		t.Fatalf("List for counts: %v", err)
	}

	if list.OpenItemCount != open || list.CheckedItemCount != checked {
		t.Errorf("counts = %d/%d, want %d/%d", list.OpenItemCount, list.CheckedItemCount, open, checked)
	}
}
