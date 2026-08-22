// SPDX-License-Identifier: CC0-1.0

package lists

import (
	"context"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/google/uuid"
)

func TestCreateList(t *testing.T) {
	t.Parallel()

	svc, _ := newService()
	ctx := context.Background()

	if _, err := svc.CreateList(ctx, "  ", testActor()); err == nil {
		t.Fatal("expected empty-name validation error")
	} else {
		assertValidationError(t, err, fieldName)
	}

	list, err := svc.CreateList(ctx, "  Groceries  ", testActor())
	if err != nil {
		t.Fatalf("CreateList: %v", err)
	}

	if list.Name != "Groceries" {
		t.Fatalf("name not trimmed: %q", list.Name)
	}
	// The acting user must reach the repository, or the list is created
	// unattributed and nothing downstream can recover who made it (US-L.11).
	if list.CreatedBy == nil || list.CreatedBy.ID != testActor() {
		t.Fatalf("CreatedBy = %+v, want actor %v", list.CreatedBy, testActor())
	}
}

func TestListsAndGetList(t *testing.T) {
	t.Parallel()

	svc, _ := newService()
	ctx := context.Background()
	list := mustCreateList(t, svc, "Groceries")

	mustAddItem(t, svc, list.ID, "Milk")

	all, err := svc.Lists(ctx)
	if err != nil || len(all) != 1 {
		t.Fatalf("Lists: %v len=%d", err, len(all))
	}

	if all[0].OpenItemCount != 1 || all[0].CheckedItemCount != 0 {
		t.Fatalf("bad counts: %+v", all[0])
	}

	got, items, err := svc.GetList(ctx, list.ID)
	if err != nil {
		t.Fatalf("GetList: %v", err)
	}

	if got.ID != list.ID || len(items) != 1 {
		t.Fatalf("GetList mismatch: %+v items=%d", got, len(items))
	}

	if _, _, err := svc.GetList(ctx, uuid.New()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestRenameList(t *testing.T) {
	t.Parallel()

	svc, _ := newService()
	ctx := context.Background()
	list := mustCreateList(t, svc, "Old")

	if _, err := svc.RenameList(ctx, list.ID, ""); err == nil {
		t.Fatal("expected validation error")
	} else {
		assertValidationError(t, err, fieldName)
	}

	got, err := svc.RenameList(ctx, list.ID, "New")
	if err != nil || got.Name != "New" {
		t.Fatalf("RenameList: %v name=%q", err, got.Name)
	}

	if _, err := svc.RenameList(ctx, uuid.New(), "X"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestDeleteList(t *testing.T) {
	t.Parallel()

	svc, repo := newService()
	ctx := context.Background()
	list := mustCreateList(t, svc, "Groceries")
	item := mustAddItem(t, svc, list.ID, "Milk")

	if err := svc.DeleteList(ctx, list.ID); err != nil {
		t.Fatalf("DeleteList: %v", err)
	}

	if _, ok := repo.items[item.ID]; ok {
		t.Fatal("expected item cascade-deleted")
	}

	if err := svc.DeleteList(ctx, uuid.New()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

// TestCopyListDerivedName covers the copy's default naming and its crediting:
// with no name supplied the copy is named after the source, every item comes
// back open, and the copy is credited to the copier. The item reset itself is
// the repository's job (see the db package's integration test); here the fake
// mirrors it so the counts can be asserted.
func TestCopyListDerivedName(t *testing.T) {
	t.Parallel()

	svc, _ := newService()
	ctx := context.Background()
	list := mustCreateList(t, svc, "Groceries")

	mustAddItem(t, svc, list.ID, "Milk")

	eggs := mustAddItem(t, svc, list.ID, "Eggs")
	mustCheckItem(t, svc, list.ID, eggs.ID, testActor())

	// A second actor copies the list, so the assertion below distinguishes
	// "credited to the copier" from "inherited from the source's creator".
	copier := uuid.MustParse("22222222-2222-2222-2222-222222222222")

	copied, err := svc.CopyList(ctx, list.ID, "", copier)
	if err != nil {
		t.Fatalf("CopyList: %v", err)
	}

	if copied.Name != "Groceries (copy)" {
		t.Errorf("copy name = %q, want %q", copied.Name, "Groceries (copy)")
	}

	if copied.OpenItemCount != 2 || copied.CheckedItemCount != 0 {
		t.Errorf("copy counts = %d/%d, want 2/0", copied.OpenItemCount, copied.CheckedItemCount)
	}

	if copied.CreatedBy == nil || copied.CreatedBy.ID != copier {
		t.Errorf("copy CreatedBy = %+v, want the copier %v", copied.CreatedBy, copier)
	}
}

// TestCopyListSuppliedName covers the explicit-name path: a supplied name wins
// and is validated like any other list name, and a missing source passes
// through as ErrNotFound.
func TestCopyListSuppliedName(t *testing.T) {
	t.Parallel()

	svc, _ := newService()
	ctx := context.Background()
	list := mustCreateList(t, svc, "Groceries")

	// A supplied name wins and is trimmed like any other list name.
	named, err := svc.CopyList(ctx, list.ID, "  Party  ", testActor())
	if err != nil {
		t.Fatalf("CopyList (named): %v", err)
	}

	if named.Name != "Party" {
		t.Errorf("copy name = %q, want %q", named.Name, "Party")
	}

	// A whitespace-only name reaches the domain and is rejected. (A JSON-level
	// empty string never gets here: the OpenAPI validator's minLength rejects
	// it first.)
	if _, err := svc.CopyList(ctx, list.ID, "   ", testActor()); err == nil {
		t.Fatal("expected name validation error")
	} else {
		assertValidationError(t, err, fieldName)
	}

	if _, err := svc.CopyList(ctx, uuid.New(), "", testActor()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

// TestCopyListNameTrimming pins the derived name's length handling: the suffix
// always survives, the result fits maxNameLength, and the trim cuts whole runes
// so a multi-byte name is never corrupted.
func TestCopyListNameTrimming(t *testing.T) {
	t.Parallel()

	if got := copyListName("  Groceries  "); got != "Groceries (copy)" {
		t.Errorf("copyListName = %q, want %q", got, "Groceries (copy)")
	}

	// A name already at the limit must be shortened to make room for " (copy)".
	long := strings.Repeat("a", maxNameLength)

	got := copyListName(long)
	if len(got) != maxNameLength {
		t.Errorf("len = %d, want %d: %q", len(got), maxNameLength, got)
	}

	if !strings.HasSuffix(got, copySuffix) {
		t.Errorf("copyListName(long) = %q, want it to end in %q", got, copySuffix)
	}

	// Multi-byte runes: 100 × "ä" is 200 bytes, so the trim must drop whole
	// runes (96 fit alongside the 7-byte suffix) rather than half a character.
	multi := copyListName(strings.Repeat("ä", 100))
	if len(multi) > maxNameLength {
		t.Errorf("len = %d, want <= %d", len(multi), maxNameLength)
	}

	if !utf8.ValidString(multi) {
		t.Errorf("copyListName cut a rune in half: %q", multi)
	}

	if want := strings.Repeat("ä", 96) + copySuffix; multi != want {
		t.Errorf("copyListName = %q, want %q", multi, want)
	}

	// A cut that lands on a space must not leave it dangling before the suffix.
	if got := copyListName(strings.Repeat("a", maxNameLength-8) + " bc"); !strings.HasSuffix(got, "a"+copySuffix) {
		t.Errorf("copyListName = %q, want no whitespace before the suffix", got)
	}
}
