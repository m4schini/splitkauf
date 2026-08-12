// SPDX-License-Identifier: TODO

package lists

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

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

func (r *fakeRepo) CreateList(_ context.Context, name string, createdBy uuid.UUID) (List, error) {
	l := &List{ID: uuid.New(), Name: name, CreatedBy: &Actor{ID: createdBy}, CreatedAt: r.clock, UpdatedAt: r.clock}
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

func (r *fakeRepo) CopyList(_ context.Context, sourceID uuid.UUID, name string, actor uuid.UUID) (List, error) {
	if _, ok := r.lists[sourceID]; !ok {
		return List{}, ErrNotFound
	}
	cp := &List{ID: uuid.New(), Name: name, CreatedBy: &Actor{ID: actor}, CreatedAt: r.clock, UpdatedAt: r.clock}
	r.lists[cp.ID] = cp
	for _, it := range r.items {
		if it.ListID != sourceID || r.deleted[it.ID] {
			continue
		}
		copied := *it
		copied.ID = uuid.New()
		copied.ListID = cp.ID
		copied.Checked = false
		copied.CheckedAt = nil
		copied.AddedBy = &Actor{ID: actor}
		copied.BoughtBy = nil
		copied.CreatedAt = r.clock
		copied.UpdatedAt = r.clock
		r.items[copied.ID] = &copied
	}
	r.recount(cp.ID)
	return *cp, nil
}

func (r *fakeRepo) AddItem(_ context.Context, listID uuid.UUID, name string, quantity int, unit string, note *string, checked bool, addedBy uuid.UUID) (Item, error) {
	if _, ok := r.lists[listID]; !ok {
		return Item{}, ErrNotFound
	}
	it := &Item{
		ID: uuid.New(), ListID: listID, Name: name, Quantity: quantity, Unit: unit, Note: note,
		Checked: checked, AddedBy: &Actor{ID: addedBy}, CreatedAt: r.clock, UpdatedAt: r.clock,
	}
	if checked {
		t := r.clock
		it.CheckedAt = &t
		// A create that arrives already checked was checked by its adder
		// offline; mirror the adapter and credit them as the buyer too.
		it.BoughtBy = &Actor{ID: addedBy}
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
	if update.Unit != nil {
		it.Unit = *update.Unit
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

func (r *fakeRepo) SetItemChecked(_ context.Context, listID, itemID uuid.UUID, checked bool, checkedAt *time.Time, checkedBy *uuid.UUID) (Item, error) {
	it, ok := r.items[itemID]
	if !ok || it.ListID != listID || r.deleted[itemID] {
		return Item{}, ErrNotFound
	}
	it.Checked = checked
	it.CheckedAt = checkedAt
	// nil clears the buyer, exactly as the adapter's NULL write does.
	it.BoughtBy = nil
	if checkedBy != nil {
		it.BoughtBy = &Actor{ID: *checkedBy}
	}
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

// testActor stands in for the authenticated user the REST layer passes down.
var testActor = uuid.MustParse("11111111-1111-1111-1111-111111111111")

func TestCreateList(t *testing.T) {
	svc, _ := newService()
	ctx := context.Background()

	if _, err := svc.CreateList(ctx, "  ", testActor); err == nil {
		t.Fatal("expected empty-name validation error")
	} else {
		assertValidationError(t, err, "name")
	}

	l, err := svc.CreateList(ctx, "  Groceries  ", testActor)
	if err != nil {
		t.Fatalf("CreateList: %v", err)
	}
	if l.Name != "Groceries" {
		t.Fatalf("name not trimmed: %q", l.Name)
	}
	// The acting user must reach the repository, or the list is created
	// unattributed and nothing downstream can recover who made it (US-L.11).
	if l.CreatedBy == nil || l.CreatedBy.ID != testActor {
		t.Fatalf("CreatedBy = %+v, want actor %v", l.CreatedBy, testActor)
	}
}

func TestListsAndGetList(t *testing.T) {
	svc, _ := newService()
	ctx := context.Background()

	l, _ := svc.CreateList(ctx, "Groceries", testActor)
	if _, err := svc.AddItem(ctx, l.ID, "Milk", 0, "", nil, false, testActor); err != nil {
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
	l, _ := svc.CreateList(ctx, "Old", testActor)

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
	l, _ := svc.CreateList(ctx, "Groceries", testActor)
	it, _ := svc.AddItem(ctx, l.ID, "Milk", 2, "", nil, false, testActor)

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

// TestCopyList covers the copy's naming rules and its pass-through of a missing
// source. The item reset itself is the repository's job (see the db package's
// integration test); here the fake mirrors it so the counts can be asserted.
func TestCopyList(t *testing.T) {
	svc, _ := newService()
	ctx := context.Background()
	l, _ := svc.CreateList(ctx, "Groceries", testActor)
	if _, err := svc.AddItem(ctx, l.ID, "Milk", 1, "", nil, false, testActor); err != nil {
		t.Fatalf("AddItem: %v", err)
	}
	checked, _ := svc.AddItem(ctx, l.ID, "Eggs", 1, "", nil, false, testActor)
	if _, err := svc.CheckItem(ctx, l.ID, checked.ID, testActor); err != nil {
		t.Fatalf("CheckItem: %v", err)
	}

	// No name supplied: derived from the source, and every item comes back open.
	// A second actor copies it, so the assertion below distinguishes "credited
	// to the copier" from "inherited from the source's creator".
	copier := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	cp, err := svc.CopyList(ctx, l.ID, "", copier)
	if err != nil {
		t.Fatalf("CopyList: %v", err)
	}
	if cp.Name != "Groceries (copy)" {
		t.Errorf("copy name = %q, want %q", cp.Name, "Groceries (copy)")
	}
	if cp.OpenItemCount != 2 || cp.CheckedItemCount != 0 {
		t.Errorf("copy counts = %d/%d, want 2/0", cp.OpenItemCount, cp.CheckedItemCount)
	}
	if cp.CreatedBy == nil || cp.CreatedBy.ID != copier {
		t.Errorf("copy CreatedBy = %+v, want the copier %v", cp.CreatedBy, copier)
	}

	// A supplied name wins and is trimmed like any other list name.
	named, err := svc.CopyList(ctx, l.ID, "  Party  ", testActor)
	if err != nil {
		t.Fatalf("CopyList (named): %v", err)
	}
	if named.Name != "Party" {
		t.Errorf("copy name = %q, want %q", named.Name, "Party")
	}

	// A whitespace-only name reaches the domain and is rejected. (A JSON-level
	// empty string never gets here: the OpenAPI validator's minLength rejects
	// it first.)
	if _, err := svc.CopyList(ctx, l.ID, "   ", testActor); err == nil {
		t.Fatal("expected name validation error")
	} else {
		assertValidationError(t, err, "name")
	}

	if _, err := svc.CopyList(ctx, uuid.New(), "", testActor); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

// TestCopyListNameTrimming pins the derived name's length handling: the suffix
// always survives, the result fits maxNameLength, and the trim cuts whole runes
// so a multi-byte name is never corrupted.
func TestCopyListNameTrimming(t *testing.T) {
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

func TestAddItem(t *testing.T) {
	svc, _ := newService()
	ctx := context.Background()
	l, _ := svc.CreateList(ctx, "Groceries", testActor)

	// name validation
	if _, err := svc.AddItem(ctx, l.ID, " ", 1, "", nil, false, testActor); err == nil {
		t.Fatal("expected name validation error")
	} else {
		assertValidationError(t, err, "name")
	}

	// quantity validation
	if _, err := svc.AddItem(ctx, l.ID, "Milk", -1, "", nil, false, testActor); err == nil {
		t.Fatal("expected quantity validation error")
	} else {
		assertValidationError(t, err, "quantity")
	}

	// default quantity + note normalisation (blank note -> nil)
	it, err := svc.AddItem(ctx, l.ID, "Milk", 0, "", ptr("   "), false, testActor)
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
	it2, err := svc.AddItem(ctx, l.ID, "Eggs", 12, "", ptr("free range"), false, testActor)
	if err != nil {
		t.Fatalf("AddItem: %v", err)
	}
	if it2.Quantity != 12 || it2.Note == nil || *it2.Note != "free range" {
		t.Fatalf("unexpected item: %+v", it2)
	}

	// unknown list
	if _, err := svc.AddItem(ctx, uuid.New(), "X", 1, "", nil, false, testActor); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestUpdateItem(t *testing.T) {
	svc, _ := newService()
	ctx := context.Background()
	l, _ := svc.CreateList(ctx, "Groceries", testActor)
	it, _ := svc.AddItem(ctx, l.ID, "Milk", 1, "", ptr("2%"), false, testActor)

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
	l, _ := svc.CreateList(ctx, "Groceries", testActor)
	it, _ := svc.AddItem(ctx, l.ID, "Milk", 1, "", nil, false, testActor)

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
	l, _ := svc.CreateList(ctx, "Groceries", testActor)
	it, _ := svc.AddItem(ctx, l.ID, "Milk", 1, "", nil, false, testActor)

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
	if _, err := svc.CheckItem(ctx, l.ID, it.ID, testActor); err != nil {
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
	l, _ := svc.CreateList(ctx, "Groceries", testActor)

	it, err := svc.AddItem(ctx, l.ID, "Milk", 1, "", nil, true, testActor)
	if err != nil {
		t.Fatalf("AddItem: %v", err)
	}
	if !it.Checked || it.CheckedAt == nil {
		t.Fatalf("expected checked item with CheckedAt, got %+v", it)
	}
	// A create that arrives already checked is an offline check folded into the
	// queued create: no CheckItem will ever follow, so the buyer has to be
	// recorded here or "Bought by you" is lost on replay (US-L.11).
	if it.AddedBy == nil || it.AddedBy.ID != testActor {
		t.Errorf("AddedBy = %+v, want actor %v", it.AddedBy, testActor)
	}
	if it.BoughtBy == nil || it.BoughtBy.ID != testActor {
		t.Errorf("BoughtBy = %+v, want the adder %v", it.BoughtBy, testActor)
	}
}

// TestItemAttribution covers the buyer's lifecycle: an open item has an adder
// but no buyer, checking credits the checker, unchecking clears them, and a
// re-check by someone else does not rewrite an already-checked item's buyer.
func TestItemAttribution(t *testing.T) {
	svc, _ := newService()
	ctx := context.Background()
	l, _ := svc.CreateList(ctx, "Groceries", testActor)

	it, err := svc.AddItem(ctx, l.ID, "Milk", 1, "", nil, false, testActor)
	if err != nil {
		t.Fatalf("AddItem: %v", err)
	}
	if it.AddedBy == nil || it.AddedBy.ID != testActor {
		t.Fatalf("AddedBy = %+v, want actor %v", it.AddedBy, testActor)
	}
	if it.BoughtBy != nil {
		t.Errorf("BoughtBy = %+v on an open item, want nil", it.BoughtBy)
	}

	// Someone else does the shopping: the buyer is the checker, not the adder.
	buyer := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	checked, err := svc.CheckItem(ctx, l.ID, it.ID, buyer)
	if err != nil {
		t.Fatalf("CheckItem: %v", err)
	}
	if checked.BoughtBy == nil || checked.BoughtBy.ID != buyer {
		t.Fatalf("BoughtBy = %+v, want the checker %v", checked.BoughtBy, buyer)
	}
	if checked.AddedBy == nil || checked.AddedBy.ID != testActor {
		t.Errorf("AddedBy = %+v, want it preserved as %v", checked.AddedBy, testActor)
	}

	// Checking an already-checked item must not reassign it: the early return
	// in setChecked is what protects the original buyer's credit.
	again, err := svc.CheckItem(ctx, l.ID, it.ID, testActor)
	if err != nil {
		t.Fatalf("CheckItem (repeat): %v", err)
	}
	if again.BoughtBy == nil || again.BoughtBy.ID != buyer {
		t.Errorf("BoughtBy = %+v after a re-check, want the original buyer %v", again.BoughtBy, buyer)
	}

	// Back on the open list, nobody has bought it — a stale buyer would claim
	// the item is handled when it is not.
	unchecked, err := svc.UncheckItem(ctx, l.ID, it.ID, testActor)
	if err != nil {
		t.Fatalf("UncheckItem: %v", err)
	}
	if unchecked.BoughtBy != nil {
		t.Errorf("BoughtBy = %+v after uncheck, want nil", unchecked.BoughtBy)
	}
	if unchecked.AddedBy == nil || unchecked.AddedBy.ID != testActor {
		t.Errorf("AddedBy = %+v, want it preserved across the uncheck", unchecked.AddedBy)
	}

	// Re-checking after an uncheck credits whoever checked it this time.
	recheck, err := svc.CheckItem(ctx, l.ID, it.ID, testActor)
	if err != nil {
		t.Fatalf("CheckItem (after uncheck): %v", err)
	}
	if recheck.BoughtBy == nil || recheck.BoughtBy.ID != testActor {
		t.Errorf("BoughtBy = %+v, want the new checker %v", recheck.BoughtBy, testActor)
	}
}

func TestCheckUncheckItem(t *testing.T) {
	svc, _ := newService()
	ctx := context.Background()
	l, _ := svc.CreateList(ctx, "Groceries", testActor)
	it, _ := svc.AddItem(ctx, l.ID, "Milk", 1, "", nil, false, testActor)

	checked, err := svc.CheckItem(ctx, l.ID, it.ID, testActor)
	if err != nil {
		t.Fatalf("CheckItem: %v", err)
	}
	if !checked.Checked || checked.CheckedAt == nil {
		t.Fatalf("expected checked with timestamp: %+v", checked)
	}

	// idempotent: checking again returns the same checkedAt (no rewrite)
	again, err := svc.CheckItem(ctx, l.ID, it.ID, testActor)
	if err != nil {
		t.Fatalf("CheckItem (idempotent): %v", err)
	}
	if !again.CheckedAt.Equal(*checked.CheckedAt) {
		t.Fatalf("checkedAt changed on idempotent re-check")
	}

	unchecked, err := svc.UncheckItem(ctx, l.ID, it.ID, testActor)
	if err != nil {
		t.Fatalf("UncheckItem: %v", err)
	}
	if unchecked.Checked || unchecked.CheckedAt != nil {
		t.Fatalf("expected unchecked with no timestamp: %+v", unchecked)
	}

	// idempotent uncheck
	if _, err := svc.UncheckItem(ctx, l.ID, it.ID, testActor); err != nil {
		t.Fatalf("UncheckItem (idempotent): %v", err)
	}

	// unknown item
	if _, err := svc.CheckItem(ctx, l.ID, uuid.New(), testActor); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestValidationErrorMessage(t *testing.T) {
	err := &ValidationError{Field: "name", Message: "boom"}
	if err.Error() != "boom" {
		t.Fatalf("unexpected message: %q", err.Error())
	}
}

// TestUnitsIsSourceOfTruth pins the canonical token list and that Units returns
// a copy callers cannot mutate.
func TestUnitsIsSourceOfTruth(t *testing.T) {
	want := []string{"amount", "g", "kg", "ml", "l", "pack", "bottle", "can", "jar", "cup", "bunch", "bag"}
	got := Units()
	if len(got) != len(want) {
		t.Fatalf("Units() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Units()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
	// Mutating the returned slice must not affect the source of truth.
	got[0] = "mutated"
	if Units()[0] != "amount" {
		t.Fatal("Units() returned a slice aliasing the internal source of truth")
	}
}

// TestAddItemUnit covers the unit path on add: empty defaults to "amount", a
// valid token is kept, and an unrecognised token is a validation error on the
// "unit" field.
func TestAddItemUnit(t *testing.T) {
	svc, _ := newService()
	ctx := context.Background()
	l, _ := svc.CreateList(ctx, "Groceries", testActor)

	// empty -> default amount
	it, err := svc.AddItem(ctx, l.ID, "Milk", 1, "", nil, false, testActor)
	if err != nil {
		t.Fatalf("AddItem: %v", err)
	}
	if it.Unit != "amount" {
		t.Fatalf("default unit = %q, want amount", it.Unit)
	}

	// valid token kept
	it2, err := svc.AddItem(ctx, l.ID, "Milk", 2, "l", nil, false, testActor)
	if err != nil {
		t.Fatalf("AddItem: %v", err)
	}
	if it2.Unit != "l" {
		t.Fatalf("unit = %q, want l", it2.Unit)
	}

	// invalid token -> validation error
	if _, err := svc.AddItem(ctx, l.ID, "Milk", 1, "furlong", nil, false, testActor); err == nil {
		t.Fatal("expected unit validation error")
	} else {
		assertValidationError(t, err, "unit")
	}
}

// TestUpdateItemUnit covers the unit path on update: a valid token is written,
// an unrecognised token is rejected on the "unit" field, and an absent unit
// leaves the item's unit unchanged.
func TestUpdateItemUnit(t *testing.T) {
	svc, _ := newService()
	ctx := context.Background()
	l, _ := svc.CreateList(ctx, "Groceries", testActor)
	it, _ := svc.AddItem(ctx, l.ID, "Milk", 1, "l", nil, false, testActor)

	// valid change
	got, err := svc.UpdateItem(ctx, l.ID, it.ID, ItemUpdate{Unit: ptr("kg")})
	if err != nil {
		t.Fatalf("UpdateItem: %v", err)
	}
	if got.Unit != "kg" {
		t.Fatalf("unit = %q, want kg", got.Unit)
	}

	// invalid token
	if _, err := svc.UpdateItem(ctx, l.ID, it.ID, ItemUpdate{Unit: ptr("furlong")}); err == nil {
		t.Fatal("expected unit validation error")
	} else {
		assertValidationError(t, err, "unit")
	}

	// absent unit leaves it unchanged
	got2, err := svc.UpdateItem(ctx, l.ID, it.ID, ItemUpdate{Name: ptr("Whole Milk")})
	if err != nil {
		t.Fatalf("UpdateItem: %v", err)
	}
	if got2.Unit != "kg" {
		t.Fatalf("unit = %q, want kg (unchanged)", got2.Unit)
	}
}

// TestUnitPreservedThroughStateChanges verifies check/uncheck/restore never
// clobber an item's unit.
func TestUnitPreservedThroughStateChanges(t *testing.T) {
	svc, _ := newService()
	ctx := context.Background()
	l, _ := svc.CreateList(ctx, "Groceries", testActor)
	it, _ := svc.AddItem(ctx, l.ID, "Milk", 1, "l", nil, false, testActor)

	checked, err := svc.CheckItem(ctx, l.ID, it.ID, testActor)
	if err != nil {
		t.Fatalf("CheckItem: %v", err)
	}
	if checked.Unit != "l" {
		t.Fatalf("checked unit = %q, want l", checked.Unit)
	}

	unchecked, err := svc.UncheckItem(ctx, l.ID, it.ID, testActor)
	if err != nil {
		t.Fatalf("UncheckItem: %v", err)
	}
	if unchecked.Unit != "l" {
		t.Fatalf("unchecked unit = %q, want l", unchecked.Unit)
	}

	if err := svc.DeleteItem(ctx, l.ID, it.ID); err != nil {
		t.Fatalf("DeleteItem: %v", err)
	}
	restored, err := svc.RestoreItem(ctx, l.ID, it.ID)
	if err != nil {
		t.Fatalf("RestoreItem: %v", err)
	}
	if restored.Unit != "l" {
		t.Fatalf("restored unit = %q, want l", restored.Unit)
	}
}
