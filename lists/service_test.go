// SPDX-License-Identifier: TODO

package lists

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

// fakeRepo is an in-memory Repository used to unit-test the Service without a
// database. It mirrors the Postgres adapter's contract: item lookups are scoped
// to their list, missing rows yield ErrNotFound, and counts are derived.
type fakeRepo struct {
	lists   map[uuid.UUID]*List
	items   map[uuid.UUID]*Item
	deleted map[uuid.UUID]bool
	clock   time.Time
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{
		lists:   make(map[uuid.UUID]*List),
		items:   make(map[uuid.UUID]*Item),
		deleted: make(map[uuid.UUID]bool),
		clock:   time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC),
	}
}

func (r *fakeRepo) recount(listID uuid.UUID) {
	l, ok := r.lists[listID]
	if !ok {
		return
	}
	open, checked := 0, 0
	for _, it := range r.items {
		if it.ListID != listID || r.deleted[it.ID] {
			continue
		}
		if it.Checked {
			checked++
		} else {
			open++
		}
	}
	l.OpenItemCount = open
	l.CheckedItemCount = checked
}

func (r *fakeRepo) CreateList(_ context.Context, name string) (List, error) {
	l := &List{ID: uuid.New(), Name: name, CreatedAt: r.clock, UpdatedAt: r.clock}
	r.lists[l.ID] = l
	return *l, nil
}

func (r *fakeRepo) Lists(_ context.Context) ([]List, error) {
	out := make([]List, 0, len(r.lists))
	for id := range r.lists {
		r.recount(id)
		out = append(out, *r.lists[id])
	}
	return out, nil
}

func (r *fakeRepo) List(_ context.Context, id uuid.UUID) (List, error) {
	l, ok := r.lists[id]
	if !ok {
		return List{}, ErrNotFound
	}
	r.recount(id)
	return *l, nil
}

func (r *fakeRepo) ListItems(_ context.Context, listID uuid.UUID) ([]Item, error) {
	if _, ok := r.lists[listID]; !ok {
		return nil, ErrNotFound
	}
	out := make([]Item, 0)
	for _, it := range r.items {
		if it.ListID == listID && !r.deleted[it.ID] {
			out = append(out, *it)
		}
	}
	return out, nil
}

func (r *fakeRepo) RenameList(_ context.Context, id uuid.UUID, name string) (List, error) {
	l, ok := r.lists[id]
	if !ok {
		return List{}, ErrNotFound
	}
	l.Name = name
	l.UpdatedAt = r.clock
	r.recount(id)
	return *l, nil
}

func (r *fakeRepo) DeleteList(_ context.Context, id uuid.UUID) error {
	if _, ok := r.lists[id]; !ok {
		return ErrNotFound
	}
	delete(r.lists, id)
	for iid, it := range r.items {
		if it.ListID == id {
			delete(r.items, iid) // cascade
		}
	}
	return nil
}

func (r *fakeRepo) AddItem(_ context.Context, listID uuid.UUID, name string, quantity int, note *string, checked bool) (Item, error) {
	if _, ok := r.lists[listID]; !ok {
		return Item{}, ErrNotFound
	}
	it := &Item{
		ID: uuid.New(), ListID: listID, Name: name, Quantity: quantity, Note: note,
		Checked: checked, CreatedAt: r.clock, UpdatedAt: r.clock,
	}
	if checked {
		t := r.clock
		it.CheckedAt = &t
	}
	r.items[it.ID] = it
	return *it, nil
}

func (r *fakeRepo) Item(_ context.Context, listID, itemID uuid.UUID) (Item, error) {
	it, ok := r.items[itemID]
	if !ok || it.ListID != listID || r.deleted[itemID] {
		return Item{}, ErrNotFound
	}
	return *it, nil
}

func (r *fakeRepo) UpdateItem(_ context.Context, listID, itemID uuid.UUID, update ItemUpdate) (Item, error) {
	it, ok := r.items[itemID]
	if !ok || it.ListID != listID || r.deleted[itemID] {
		return Item{}, ErrNotFound
	}
	if update.Name != nil {
		it.Name = *update.Name
	}
	if update.Quantity != nil {
		it.Quantity = *update.Quantity
	}
	if update.NoteSet {
		it.Note = update.Note
	}
	it.UpdatedAt = r.clock
	return *it, nil
}

func (r *fakeRepo) DeleteItem(_ context.Context, listID, itemID uuid.UUID) error {
	it, ok := r.items[itemID]
	if !ok || it.ListID != listID || r.deleted[itemID] {
		return ErrNotFound
	}
	r.deleted[itemID] = true
	return nil
}

func (r *fakeRepo) RestoreItem(_ context.Context, listID, itemID uuid.UUID) (Item, error) {
	it, ok := r.items[itemID]
	if !ok || it.ListID != listID {
		return Item{}, ErrNotFound
	}
	delete(r.deleted, itemID)
	it.UpdatedAt = r.clock
	return *it, nil
}

func (r *fakeRepo) SetItemChecked(_ context.Context, listID, itemID uuid.UUID, checked bool, checkedAt *time.Time) (Item, error) {
	it, ok := r.items[itemID]
	if !ok || it.ListID != listID || r.deleted[itemID] {
		return Item{}, ErrNotFound
	}
	it.Checked = checked
	it.CheckedAt = checkedAt
	it.UpdatedAt = r.clock
	return *it, nil
}

// assertValidationError fails the test unless err is a *ValidationError on the
// expected field.
func assertValidationError(t *testing.T, err error, field string) {
	t.Helper()
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("expected *ValidationError, got %v", err)
	}
	if ve.Field != field {
		t.Fatalf("expected field %q, got %q", field, ve.Field)
	}
}

func newService() (*Service, *fakeRepo) {
	repo := newFakeRepo()
	svc := NewService(repo)
	svc.now = func() time.Time { return repo.clock }
	return svc, repo
}

func ptr[T any](v T) *T { return &v }

func TestCreateList(t *testing.T) {
	svc, _ := newService()
	ctx := context.Background()

	if _, err := svc.CreateList(ctx, "  "); err == nil {
		t.Fatal("expected empty-name validation error")
	} else {
		assertValidationError(t, err, "name")
	}

	l, err := svc.CreateList(ctx, "  Groceries  ")
	if err != nil {
		t.Fatalf("CreateList: %v", err)
	}
	if l.Name != "Groceries" {
		t.Fatalf("name not trimmed: %q", l.Name)
	}
}

func TestListsAndGetList(t *testing.T) {
	svc, _ := newService()
	ctx := context.Background()

	l, _ := svc.CreateList(ctx, "Groceries")
	if _, err := svc.AddItem(ctx, l.ID, "Milk", 0, nil, false); err != nil {
		t.Fatalf("AddItem: %v", err)
	}

	all, err := svc.Lists(ctx)
	if err != nil || len(all) != 1 {
		t.Fatalf("Lists: %v len=%d", err, len(all))
	}
	if all[0].OpenItemCount != 1 || all[0].CheckedItemCount != 0 {
		t.Fatalf("bad counts: %+v", all[0])
	}

	got, items, err := svc.GetList(ctx, l.ID)
	if err != nil {
		t.Fatalf("GetList: %v", err)
	}
	if got.ID != l.ID || len(items) != 1 {
		t.Fatalf("GetList mismatch: %+v items=%d", got, len(items))
	}

	if _, _, err := svc.GetList(ctx, uuid.New()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestRenameList(t *testing.T) {
	svc, _ := newService()
	ctx := context.Background()
	l, _ := svc.CreateList(ctx, "Old")

	if _, err := svc.RenameList(ctx, l.ID, ""); err == nil {
		t.Fatal("expected validation error")
	} else {
		assertValidationError(t, err, "name")
	}

	got, err := svc.RenameList(ctx, l.ID, "New")
	if err != nil || got.Name != "New" {
		t.Fatalf("RenameList: %v name=%q", err, got.Name)
	}

	if _, err := svc.RenameList(ctx, uuid.New(), "X"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestDeleteList(t *testing.T) {
	svc, repo := newService()
	ctx := context.Background()
	l, _ := svc.CreateList(ctx, "Groceries")
	it, _ := svc.AddItem(ctx, l.ID, "Milk", 2, nil, false)

	if err := svc.DeleteList(ctx, l.ID); err != nil {
		t.Fatalf("DeleteList: %v", err)
	}
	if _, ok := repo.items[it.ID]; ok {
		t.Fatal("expected item cascade-deleted")
	}
	if err := svc.DeleteList(ctx, uuid.New()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestAddItem(t *testing.T) {
	svc, _ := newService()
	ctx := context.Background()
	l, _ := svc.CreateList(ctx, "Groceries")

	// name validation
	if _, err := svc.AddItem(ctx, l.ID, " ", 1, nil, false); err == nil {
		t.Fatal("expected name validation error")
	} else {
		assertValidationError(t, err, "name")
	}

	// quantity validation
	if _, err := svc.AddItem(ctx, l.ID, "Milk", -1, nil, false); err == nil {
		t.Fatal("expected quantity validation error")
	} else {
		assertValidationError(t, err, "quantity")
	}

	// default quantity + note normalisation (blank note -> nil)
	it, err := svc.AddItem(ctx, l.ID, "Milk", 0, ptr("   "), false)
	if err != nil {
		t.Fatalf("AddItem: %v", err)
	}
	if it.Quantity != 1 {
		t.Fatalf("expected default quantity 1, got %d", it.Quantity)
	}
	if it.Note != nil {
		t.Fatalf("expected nil note, got %v", *it.Note)
	}

	// explicit quantity + real note
	it2, err := svc.AddItem(ctx, l.ID, "Eggs", 12, ptr("free range"), false)
	if err != nil {
		t.Fatalf("AddItem: %v", err)
	}
	if it2.Quantity != 12 || it2.Note == nil || *it2.Note != "free range" {
		t.Fatalf("unexpected item: %+v", it2)
	}

	// unknown list
	if _, err := svc.AddItem(ctx, uuid.New(), "X", 1, nil, false); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestUpdateItem(t *testing.T) {
	svc, _ := newService()
	ctx := context.Background()
	l, _ := svc.CreateList(ctx, "Groceries")
	it, _ := svc.AddItem(ctx, l.ID, "Milk", 1, ptr("2%"), false)

	// invalid name
	if _, err := svc.UpdateItem(ctx, l.ID, it.ID, ItemUpdate{Name: ptr("")}); err == nil {
		t.Fatal("expected name validation error")
	} else {
		assertValidationError(t, err, "name")
	}

	// invalid quantity
	if _, err := svc.UpdateItem(ctx, l.ID, it.ID, ItemUpdate{Quantity: ptr(0)}); err == nil {
		t.Fatal("expected quantity validation error")
	} else {
		assertValidationError(t, err, "quantity")
	}

	// happy path: rename, requantify, clear note
	got, err := svc.UpdateItem(ctx, l.ID, it.ID, ItemUpdate{
		Name:     ptr("  Whole Milk "),
		Quantity: ptr(3),
		NoteSet:  true,
		Note:     nil,
	})
	if err != nil {
		t.Fatalf("UpdateItem: %v", err)
	}
	if got.Name != "Whole Milk" || got.Quantity != 3 || got.Note != nil {
		t.Fatalf("unexpected item: %+v", got)
	}

	// no-op update (nothing set) leaves fields intact
	got2, err := svc.UpdateItem(ctx, l.ID, it.ID, ItemUpdate{})
	if err != nil || got2.Name != "Whole Milk" {
		t.Fatalf("no-op update failed: %v %+v", err, got2)
	}

	// wrong list scoping -> not found
	if _, err := svc.UpdateItem(ctx, uuid.New(), it.ID, ItemUpdate{}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestDeleteItem(t *testing.T) {
	svc, _ := newService()
	ctx := context.Background()
	l, _ := svc.CreateList(ctx, "Groceries")
	it, _ := svc.AddItem(ctx, l.ID, "Milk", 1, nil, false)

	if err := svc.DeleteItem(ctx, l.ID, it.ID); err != nil {
		t.Fatalf("DeleteItem: %v", err)
	}
	if err := svc.DeleteItem(ctx, l.ID, it.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

// TestRestoreItem checks the Service delegates to the repository's RestoreItem:
// a deleted item becomes visible again, and a missing item yields ErrNotFound.
func TestRestoreItem(t *testing.T) {
	svc, _ := newService()
	ctx := context.Background()
	l, _ := svc.CreateList(ctx, "Groceries")
	it, _ := svc.AddItem(ctx, l.ID, "Milk", 1, nil, false)

	if err := svc.DeleteItem(ctx, l.ID, it.ID); err != nil {
		t.Fatalf("DeleteItem: %v", err)
	}

	restored, err := svc.RestoreItem(ctx, l.ID, it.ID)
	if err != nil {
		t.Fatalf("RestoreItem: %v", err)
	}
	if restored.ID != it.ID {
		t.Fatalf("restored id = %v, want %v", restored.ID, it.ID)
	}
	// The item is visible again after restore.
	if _, err := svc.CheckItem(ctx, l.ID, it.ID); err != nil {
		t.Fatalf("item not visible after restore: %v", err)
	}

	if _, err := svc.RestoreItem(ctx, l.ID, uuid.New()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

// TestAddItemChecked verifies the checked flag is threaded through to the
// repository, which stamps CheckedAt when creating an already-checked item.
func TestAddItemChecked(t *testing.T) {
	svc, _ := newService()
	ctx := context.Background()
	l, _ := svc.CreateList(ctx, "Groceries")

	it, err := svc.AddItem(ctx, l.ID, "Milk", 1, nil, true)
	if err != nil {
		t.Fatalf("AddItem: %v", err)
	}
	if !it.Checked || it.CheckedAt == nil {
		t.Fatalf("expected checked item with CheckedAt, got %+v", it)
	}
}

func TestCheckUncheckItem(t *testing.T) {
	svc, _ := newService()
	ctx := context.Background()
	l, _ := svc.CreateList(ctx, "Groceries")
	it, _ := svc.AddItem(ctx, l.ID, "Milk", 1, nil, false)

	checked, err := svc.CheckItem(ctx, l.ID, it.ID)
	if err != nil {
		t.Fatalf("CheckItem: %v", err)
	}
	if !checked.Checked || checked.CheckedAt == nil {
		t.Fatalf("expected checked with timestamp: %+v", checked)
	}

	// idempotent: checking again returns the same checkedAt (no rewrite)
	again, err := svc.CheckItem(ctx, l.ID, it.ID)
	if err != nil {
		t.Fatalf("CheckItem (idempotent): %v", err)
	}
	if !again.CheckedAt.Equal(*checked.CheckedAt) {
		t.Fatalf("checkedAt changed on idempotent re-check")
	}

	unchecked, err := svc.UncheckItem(ctx, l.ID, it.ID)
	if err != nil {
		t.Fatalf("UncheckItem: %v", err)
	}
	if unchecked.Checked || unchecked.CheckedAt != nil {
		t.Fatalf("expected unchecked with no timestamp: %+v", unchecked)
	}

	// idempotent uncheck
	if _, err := svc.UncheckItem(ctx, l.ID, it.ID); err != nil {
		t.Fatalf("UncheckItem (idempotent): %v", err)
	}

	// unknown item
	if _, err := svc.CheckItem(ctx, l.ID, uuid.New()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestValidationErrorMessage(t *testing.T) {
	err := &ValidationError{Field: "name", Message: "boom"}
	if err.Error() != "boom" {
		t.Fatalf("unexpected message: %q", err.Error())
	}
}
