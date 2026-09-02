// SPDX-License-Identifier: CC0-1.0

package lists

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestUpdateItemUnitIsMatchedExactly pins that a unit is only accepted when it
// matches a canonical token byte for byte: unlike the name, it is neither
// trimmed nor case-folded, so a padded or capitalised token that a user would
// read as valid is still rejected. The last case also fixes where the unit sits
// in the order of work: it is checked before the note is normalised and before
// anything reaches the repository.
func TestUpdateItemUnitIsMatchedExactly(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		update ItemUpdate
	}{
		{name: "unit padded on both sides", update: itemUpdate(nil, nil, new(" kg "), false, nil)},
		{name: "unit with a trailing space", update: itemUpdate(nil, nil, new("kg "), false, nil)},
		{name: "unit with a leading tab", update: itemUpdate(nil, nil, new("\tl"), false, nil)},
		// A unit of only whitespace is not the same as the empty unit that means
		// "no unit": it never becomes the default.
		{name: "blank unit", update: itemUpdate(nil, nil, new("   "), false, nil)},
		{name: "unit in upper case", update: itemUpdate(nil, nil, new("KG"), false, nil)},
		{name: "unit in title case", update: itemUpdate(nil, nil, new("Kg"), false, nil)},
		{name: "default unit in upper case", update: itemUpdate(nil, nil, new(strings.ToUpper(defaultUnit)), false, nil)},
		// Everything else about this update is fine, and the note would have been
		// rewritten had the unit not been rejected first.
		{
			name:   "bad unit outranks the note and the repository",
			update: itemUpdate(new(" Oat milk "), new(3), new(" kg "), true, new("  buy two  ")),
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var zero Item

			svc, repo, list, item := newUpdateItemService(t)

			got, err := svc.UpdateItem(context.Background(), list.ID, item.ID, testCase.update)
			if err == nil {
				t.Fatalf("expected a validation error, got item %+v", got)
			}

			assertValidationError(t, err, fieldUnit)

			if strings.Contains(err.Error(), "updating item") {
				t.Errorf("validation error = %q, want it returned unwrapped", err)
			}

			if got != zero {
				t.Errorf("item = %+v, want the zero value", got)
			}

			if repo.calls != 0 {
				t.Errorf("repository UpdateItem calls = %d, want 0 - validation runs first", repo.calls)
			}
		})
	}
}

// TestUpdateItemAcceptsALongNote pins that a note at maxNoteLength runes -
// longer than any name may be, and multi-byte - is stored as given, trimmed but
// otherwise untouched. The limit is measured after trimming.
func TestUpdateItemAcceptsALongNote(t *testing.T) {
	t.Parallel()

	svc, repo, list, item := newUpdateItemService(t)
	longNote := strings.Repeat("ö", maxNoteLength)

	update := itemUpdate(nil, nil, nil, true, new("  "+longNote+"  "))

	got, err := svc.UpdateItem(context.Background(), list.ID, item.ID, update)
	if err != nil {
		t.Fatalf("UpdateItem: %v", err)
	}

	if repo.calls != 1 {
		t.Fatalf("repository UpdateItem calls = %d, want 1", repo.calls)
	}

	assertNote(t, got.Note, &longNote)
}

// TestAddItemRejectsATooLongNote pins that AddItem enforces maxNoteLength
// (in runes) before anything reaches the repository, and returns the
// ValidationError unwrapped so the REST layer maps it to a 400.
func TestAddItemRejectsATooLongNote(t *testing.T) {
	t.Parallel()

	svc := NewService(newFakeRepo())
	list := mustCreateList(t, svc, "Groceries")

	atLimit := strings.Repeat("ä", maxNoteLength)

	got, err := svc.AddItem(context.Background(), list.ID, "Milk", 1, "", &atLimit, false, testActor())
	if err != nil {
		t.Fatalf("AddItem at the limit: %v", err)
	}

	assertNote(t, got.Note, &atLimit)

	overLimit := atLimit + "ä"

	_, err = svc.AddItem(context.Background(), list.ID, "Eggs", 1, "", &overLimit, false, testActor())
	assertValidationError(t, err, fieldNote)

	if strings.Contains(err.Error(), "adding item") {
		t.Errorf("validation error = %q, want it returned unwrapped", err)
	}
}

// The prefix DeleteItem wraps every repository failure with; the REST layer
// unwraps through it, so it is part of the contract.
const deleteItemErrPrefix = "deleting item: "

// errDeleteItemBoom stands in for a repository failing for reasons of its own,
// so the wrapping can be checked without one of the domain's sentinels.
var errDeleteItemBoom = errors.New("repository exploded")

// deleteItemCtxKey tags the context handed to DeleteItem, so the repository can
// prove the caller's context - and not a fresh one - reached persistence.
type deleteItemCtxKey struct{}

// deleteSpyRepo records what DeleteItem reaches the repository with, and can
// make it fail. DeleteItem returns nothing but an error, so its arguments are
// the only place the service's forwarding is observable.
type deleteSpyRepo struct {
	*fakeRepo

	calls       int
	sawCtxValue string
	lastListID  uuid.UUID
	lastItemID  uuid.UUID
	err         error
}

func (r *deleteSpyRepo) DeleteItem(ctx context.Context, listID, itemID uuid.UUID) error {
	r.calls++
	r.sawCtxValue, _ = ctx.Value(deleteItemCtxKey{}).(string)
	r.lastListID = listID
	r.lastItemID = itemID

	if r.err != nil {
		return r.err
	}

	return r.fakeRepo.DeleteItem(ctx, listID, itemID)
}

// newDeleteItemService builds a Service over a spying repository holding one
// list with the single item the cases delete.
func newDeleteItemService(t *testing.T) (*Service, *deleteSpyRepo, List, Item) {
	t.Helper()

	fake := newFakeRepo()
	repo := &deleteSpyRepo{
		fakeRepo:    fake,
		calls:       0,
		sawCtxValue: "",
		lastListID:  uuid.Nil,
		lastItemID:  uuid.Nil,
		err:         nil,
	}
	svc := NewService(repo)
	svc.now = func() time.Time { return fake.clock }

	list := mustCreateList(t, svc, "Groceries")
	item := mustAddItem(t, svc, list.ID, "Milk")

	return svc, repo, list, item
}

// TestDeleteItemSoftDeletes covers the success path: the call reports nothing
// but a nil error, and the row survives with the deletion recorded so the
// action can replay offline and be undone.
func TestDeleteItemSoftDeletes(t *testing.T) {
	t.Parallel()

	svc, repo, list, item := newDeleteItemService(t)

	if err := svc.DeleteItem(context.Background(), list.ID, item.ID); err != nil {
		t.Fatalf("DeleteItem: %v", err)
	}

	if repo.calls != 1 {
		t.Fatalf("repository DeleteItem calls = %d, want 1", repo.calls)
	}

	// Soft delete: the row is kept, only marked.
	if _, ok := repo.items[item.ID]; !ok {
		t.Errorf("item row is gone, want it kept with the deletion recorded")
	}

	if !repo.deleted[item.ID] {
		t.Errorf("item is not marked deleted, want a soft delete")
	}

	if _, err := repo.Item(context.Background(), list.ID, item.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("reading the deleted item = %v, want ErrNotFound", err)
	}
}

// TestDeleteItemForwardsToRepository checks that the caller's context and both
// identifiers arrive unchanged and in the right order - a swapped list and item
// would otherwise fail silently in a signature that takes two UUIDs.
func TestDeleteItemForwardsToRepository(t *testing.T) {
	t.Parallel()

	svc, repo, list, item := newDeleteItemService(t)
	ctx := context.WithValue(context.Background(), deleteItemCtxKey{}, "caller")

	if err := svc.DeleteItem(ctx, list.ID, item.ID); err != nil {
		t.Fatalf("DeleteItem: %v", err)
	}

	if repo.sawCtxValue != "caller" {
		t.Errorf("repository saw context value %q, want %q", repo.sawCtxValue, "caller")
	}

	if repo.lastListID != list.ID {
		t.Errorf("repository saw list %v, want %v", repo.lastListID, list.ID)
	}

	if repo.lastItemID != item.ID {
		t.Errorf("repository saw item %v, want %v", repo.lastItemID, item.ID)
	}
}

// TestDeleteItemRepositoryErrors covers every failure: the service validates
// nothing, so each one is the repository's verdict, handed back wrapped with
// the sentinel still reachable through errors.Is.
func TestDeleteItemRepositoryErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// arrange sets the repository up and picks the identifiers to delete.
		arrange func(t *testing.T, svc *Service, repo *deleteSpyRepo, list List, item Item) (uuid.UUID, uuid.UUID)
		wantErr error
		// wantNotFound pins whether the caller may read the failure as a missing
		// item: a plain repository failure must not be converted into one.
		wantNotFound bool
	}{
		{
			name: "unknown item",
			arrange: func(_ *testing.T, _ *Service, _ *deleteSpyRepo, list List, _ Item) (uuid.UUID, uuid.UUID) {
				return list.ID, uuid.New()
			},
			wantErr:      ErrNotFound,
			wantNotFound: true,
		},
		// List-scoping is the repository's job: the service forwards both IDs and
		// lets the mismatch come back as ErrNotFound.
		{
			name: "item on another list",
			arrange: func(t *testing.T, svc *Service, _ *deleteSpyRepo, _ List, item Item) (uuid.UUID, uuid.UUID) {
				t.Helper()

				other := mustCreateList(t, svc, "Hardware")

				return other.ID, item.ID
			},
			wantErr:      ErrNotFound,
			wantNotFound: true,
		},
		// Deleting twice is the offline replay case: the second attempt finds the
		// row already soft-deleted and is refused rather than silently accepted.
		{
			name: "item already deleted",
			arrange: func(t *testing.T, svc *Service, _ *deleteSpyRepo, list List, item Item) (uuid.UUID, uuid.UUID) {
				t.Helper()

				if err := svc.DeleteItem(context.Background(), list.ID, item.ID); err != nil {
					t.Fatalf("first DeleteItem: %v", err)
				}

				return list.ID, item.ID
			},
			wantErr:      ErrNotFound,
			wantNotFound: true,
		},
		// Nothing is guarded before the call, so even nil identifiers reach the
		// repository and come back as its ordinary miss.
		{
			name: "zero-value identifiers",
			arrange: func(_ *testing.T, _ *Service, _ *deleteSpyRepo, _ List, _ Item) (uuid.UUID, uuid.UUID) {
				return uuid.Nil, uuid.Nil
			},
			wantErr:      ErrNotFound,
			wantNotFound: true,
		},
		{
			name: "repository failure",
			arrange: func(_ *testing.T, _ *Service, repo *deleteSpyRepo, list List, item Item) (uuid.UUID, uuid.UUID) {
				repo.err = errDeleteItemBoom

				return list.ID, item.ID
			},
			wantErr:      errDeleteItemBoom,
			wantNotFound: false,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			svc, repo, list, item := newDeleteItemService(t)
			listID, itemID := testCase.arrange(t, svc, repo, list, item)

			err := svc.DeleteItem(context.Background(), listID, itemID)
			if !errors.Is(err, testCase.wantErr) {
				t.Fatalf("DeleteItem = %v, want it to wrap %v", err, testCase.wantErr)
			}

			if !strings.HasPrefix(err.Error(), deleteItemErrPrefix) {
				t.Errorf("error = %q, want it prefixed with %q", err, deleteItemErrPrefix)
			}

			if errors.Is(err, ErrNotFound) != testCase.wantNotFound {
				t.Errorf("errors.Is(%v, ErrNotFound) = %t, want %t", err, !testCase.wantNotFound, testCase.wantNotFound)
			}

			if repo.calls == 0 {
				t.Errorf("repository DeleteItem calls = 0, want the call forwarded - the service validates nothing")
			}
		})
	}
}

// TestDeleteItemForwardsCancelledContext pins that the service does not check
// the context itself: the call still reaches the repository, and whatever the
// repository makes of the cancellation comes back wrapped like any other error.
func TestDeleteItemForwardsCancelledContext(t *testing.T) {
	t.Parallel()

	svc, repo, list, item := newDeleteItemService(t)
	repo.err = context.Canceled

	ctx, cancel := context.WithCancel(context.WithValue(context.Background(), deleteItemCtxKey{}, "caller"))
	cancel()

	err := svc.DeleteItem(ctx, list.ID, item.ID)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("DeleteItem = %v, want it to wrap context.Canceled", err)
	}

	if !strings.HasPrefix(err.Error(), deleteItemErrPrefix) {
		t.Errorf("error = %q, want it prefixed with %q", err, deleteItemErrPrefix)
	}

	if repo.calls != 1 {
		t.Errorf("repository DeleteItem calls = %d, want 1 - the service does not short-circuit", repo.calls)
	}
}

// errRestoreItemBoom stands in for a repository failing for reasons of its own,
// so the wrapping can be checked without one of the domain's sentinels.
var errRestoreItemBoom = errors.New("repository exploded")

// restoreItemCtxKey tags the context handed to RestoreItem, so the repository
// can prove the caller's context - and not a fresh one - reached persistence.
type restoreItemCtxKey struct{}

// restoreSpyRepo records what RestoreItem reaches the repository with, and can
// make it fail. RestoreItem is a pass-through, so its arguments and the row it
// hands back are the only things there are to observe.
type restoreSpyRepo struct {
	*fakeRepo

	calls       int
	sawCtxValue string
	lastListID  uuid.UUID
	lastItemID  uuid.UUID
	err         error
}

func (r *restoreSpyRepo) RestoreItem(ctx context.Context, listID, itemID uuid.UUID) (Item, error) {
	r.calls++
	r.sawCtxValue, _ = ctx.Value(restoreItemCtxKey{}).(string)
	r.lastListID = listID
	r.lastItemID = itemID

	if r.err != nil {
		return Item{}, r.err
	}

	return r.fakeRepo.RestoreItem(ctx, listID, itemID)
}

// newRestoreItemService builds a Service over a spying repository holding one
// empty list for the cases to put items on.
func newRestoreItemService(t *testing.T) (*Service, *restoreSpyRepo, List) {
	t.Helper()

	fake := newFakeRepo()
	repo := &restoreSpyRepo{
		fakeRepo:    fake,
		calls:       0,
		sawCtxValue: "",
		lastListID:  uuid.Nil,
		lastItemID:  uuid.Nil,
		err:         nil,
	}
	svc := NewService(repo)
	svc.now = func() time.Time { return fake.clock }

	return svc, repo, mustCreateList(t, svc, "Groceries")
}

// assertRestoredItemNote fails the test unless both notes are unset or both
// hold the same text.
func assertRestoredItemNote(t *testing.T, got, want *string) {
	t.Helper()

	if (got == nil) != (want == nil) {
		t.Fatalf("note = %v, want it preserved as %v", got, want)
	}

	if got != nil && *got != *want {
		t.Errorf("note = %q, want %q", *got, *want)
	}
}

// assertRestoredItemShape fails the test unless the optional fields survived
// the restore: the note as supplied, the checked state with its timestamp and
// buyer, and the original adder.
func assertRestoredItemShape(t *testing.T, got Item, note *string, checked bool) {
	t.Helper()

	assertRestoredItemNote(t, got.Note, note)

	if got.Checked != checked {
		t.Errorf("checked = %t, want %t - restore does not touch the checked state", got.Checked, checked)
	}

	if checked && (got.CheckedAt == nil || got.BoughtBy == nil) {
		t.Errorf("checkedAt = %v, boughtBy = %v, want both preserved", got.CheckedAt, got.BoughtBy)
	}

	if got.AddedBy == nil || got.AddedBy.ID != testActor() {
		t.Errorf("addedBy = %+v, want the original adder %v", got.AddedBy, testActor())
	}
}

// TestRestoreItem covers the happy path: the stored row comes back exactly as
// the repository holds it, and restoring an item that was never deleted is the
// same no-op returning the same row.
func TestRestoreItem(t *testing.T) {
	t.Parallel()

	note := "fresh"

	testCases := []struct {
		name    string
		note    *string
		checked bool
		deleted bool
	}{
		{name: "soft deleted open item", note: nil, checked: false, deleted: true},
		{name: "soft deleted item with note and buyer", note: &note, checked: true, deleted: true},
		{name: "item that was never deleted", note: &note, checked: false, deleted: false},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			svc, repo, list := newRestoreItemService(t)

			added, err := svc.AddItem(
				context.Background(), list.ID, "Milk", 2, "l", testCase.note, testCase.checked, testActor(),
			)
			if err != nil {
				t.Fatalf("AddItem: %v", err)
			}

			// The stored row, read before the soft delete hides it, is what a
			// pass-through restore has to hand back untouched.
			want, err := repo.Item(context.Background(), list.ID, added.ID)
			if err != nil {
				t.Fatalf("Item: %v", err)
			}

			if testCase.deleted {
				repo.deleted[added.ID] = true
			}

			got, err := svc.RestoreItem(context.Background(), list.ID, added.ID)
			if err != nil {
				t.Fatalf("RestoreItem: %v", err)
			}

			if got != want {
				t.Errorf("item = %+v, want the stored row unchanged: %+v", got, want)
			}

			if repo.deleted[added.ID] {
				t.Error("item is still soft deleted, want the delete cleared")
			}

			assertRestoredItemShape(t, got, testCase.note, testCase.checked)
		})
	}
}

// TestRestoreItemForwardsToRepository checks that the caller's context and both
// IDs arrive at persistence unchanged - the service has nothing of its own to
// add on the way.
func TestRestoreItemForwardsToRepository(t *testing.T) {
	t.Parallel()

	svc, repo, list := newRestoreItemService(t)
	item := mustAddItem(t, svc, list.ID, "Milk")
	repo.deleted[item.ID] = true

	ctx := context.WithValue(context.Background(), restoreItemCtxKey{}, "restore caller")

	if _, err := svc.RestoreItem(ctx, list.ID, item.ID); err != nil {
		t.Fatalf("RestoreItem: %v", err)
	}

	if repo.calls != 1 {
		t.Errorf("repository RestoreItem calls = %d, want 1", repo.calls)
	}

	if repo.sawCtxValue != "restore caller" {
		t.Errorf("repository saw context value %q, want %q", repo.sawCtxValue, "restore caller")
	}

	if repo.lastListID != list.ID || repo.lastItemID != item.ID {
		t.Errorf("repository saw %v/%v, want list %v item %v", repo.lastListID, repo.lastItemID, list.ID, item.ID)
	}
}

// TestRestoreItemDoesNotValidateIDs pins that the service validates nothing:
// even uuid.Nil reaches the repository, which is what turns it into ErrNotFound.
func TestRestoreItemDoesNotValidateIDs(t *testing.T) {
	t.Parallel()

	svc, repo, _ := newRestoreItemService(t)

	if _, err := svc.RestoreItem(context.Background(), uuid.Nil, uuid.Nil); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}

	if repo.calls != 1 {
		t.Errorf("repository RestoreItem calls = %d, want 1 - the service rejects nothing itself", repo.calls)
	}

	if repo.lastListID != uuid.Nil || repo.lastItemID != uuid.Nil {
		t.Errorf("repository saw %v/%v, want both uuid.Nil", repo.lastListID, repo.lastItemID)
	}
}

// TestRestoreItemDoesNotUseTheClock pins the pass-through: restore has no
// timestamp of its own, so the service clock must stay untouched.
func TestRestoreItemDoesNotUseTheClock(t *testing.T) {
	t.Parallel()

	svc, repo, list := newRestoreItemService(t)
	item := mustAddItem(t, svc, list.ID, "Milk")
	repo.deleted[item.ID] = true

	svc.now = func() time.Time {
		t.Error("RestoreItem read the service clock, want persistence to own the timestamp")

		return repo.clock
	}

	if _, err := svc.RestoreItem(context.Background(), list.ID, item.ID); err != nil {
		t.Fatalf("RestoreItem: %v", err)
	}
}

// TestRestoreItemRepositoryErrors covers the persistence failures: they come
// back wrapped, so callers have to unwrap, and no item comes with them.
func TestRestoreItemRepositoryErrors(t *testing.T) {
	t.Parallel()

	const wantPrefix = "restoring item: "

	testCases := []struct {
		name    string
		arrange func(t *testing.T, svc *Service, repo *restoreSpyRepo, list List, item Item) (uuid.UUID, uuid.UUID)
		wantErr error
	}{
		{
			name: "unknown item",
			arrange: func(_ *testing.T, _ *Service, _ *restoreSpyRepo, list List, _ Item) (uuid.UUID, uuid.UUID) {
				return list.ID, uuid.New()
			},
			wantErr: ErrNotFound,
		},
		{
			name: "unknown list",
			arrange: func(_ *testing.T, _ *Service, _ *restoreSpyRepo, _ List, item Item) (uuid.UUID, uuid.UUID) {
				return uuid.New(), item.ID
			},
			wantErr: ErrNotFound,
		},
		// List-scoping is the repository's job: the service forwards both IDs
		// and lets the mismatch come back as ErrNotFound.
		{
			name: "item on another list",
			arrange: func(t *testing.T, svc *Service, _ *restoreSpyRepo, _ List, item Item) (uuid.UUID, uuid.UUID) {
				t.Helper()

				return mustCreateList(t, svc, "Hardware").ID, item.ID
			},
			wantErr: ErrNotFound,
		},
		{
			name: "arbitrary repository failure",
			arrange: func(_ *testing.T, _ *Service, repo *restoreSpyRepo, list List, item Item) (uuid.UUID, uuid.UUID) {
				repo.err = errRestoreItemBoom

				return list.ID, item.ID
			},
			wantErr: errRestoreItemBoom,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var zero Item

			svc, repo, list := newRestoreItemService(t)
			item := mustAddItem(t, svc, list.ID, "Milk")
			repo.deleted[item.ID] = true

			listID, itemID := testCase.arrange(t, svc, repo, list, item)

			got, err := svc.RestoreItem(context.Background(), listID, itemID)
			if !errors.Is(err, testCase.wantErr) {
				t.Fatalf("error = %v, want it to match %v", err, testCase.wantErr)
			}

			if !strings.HasPrefix(err.Error(), wantPrefix) {
				t.Errorf("error = %q, want it prefixed with %q", err, wantPrefix)
			}

			if got != zero {
				t.Errorf("item = %+v, want the zero value", got)
			}
		})
	}
}

// TestRestoreItemCancelledContext pins that the service does not inspect the
// context itself: a cancelled one still reaches the repository, and the error
// that comes back stays matchable through the wrap.
func TestRestoreItemCancelledContext(t *testing.T) {
	t.Parallel()

	var zero Item

	svc, repo, list := newRestoreItemService(t)
	item := mustAddItem(t, svc, list.ID, "Milk")
	repo.deleted[item.ID] = true
	repo.err = context.Canceled

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	got, err := svc.RestoreItem(ctx, list.ID, item.ID)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}

	if !strings.HasPrefix(err.Error(), "restoring item: ") {
		t.Errorf("error = %q, want it prefixed with %q", err, "restoring item: ")
	}

	if got != zero {
		t.Errorf("item = %+v, want the zero value", got)
	}

	if repo.calls != 1 {
		t.Errorf("repository RestoreItem calls = %d, want 1 - the service does not check the context", repo.calls)
	}
}

// The item every UncheckItem case below starts from: "Milk", 2 l, note "fresh".
const (
	uncheckItemName = "Milk"
	uncheckItemUnit = "l"
	uncheckItemNote = "fresh"
	uncheckItemQty  = 2
	// The context value proves propagation; the prefixes tell read from write.
	uncheckCtxValue    = "caller"
	uncheckReadPrefix  = "getting item: "
	uncheckWritePrefix = "setting item checked: "
)

// errUncheckItemBoom stands in for a repository failing for reasons of its own,
// so the wrapping can be checked without one of the domain's sentinels.
var errUncheckItemBoom = errors.New("repository exploded")

// uncheckItemCtxKey tags the context handed to UncheckItem, so the repository
// can prove the caller's context - and not a fresh one - reached persistence.
type uncheckItemCtxKey struct{}

// uncheckSpyRepo records the read and the write UncheckItem makes, and can make
// either fail. Whether the write happens at all is the contract here.
type uncheckSpyRepo struct {
	*fakeRepo

	setCalls      int
	sawItemCtx    string
	sawSetCtx     string
	lastListID    uuid.UUID
	lastItemID    uuid.UUID
	lastChecked   bool
	lastCheckedAt *time.Time
	lastCheckedBy *uuid.UUID
	itemErr       error
	setErr        error
}

func (r *uncheckSpyRepo) Item(ctx context.Context, listID, itemID uuid.UUID) (Item, error) {
	r.sawItemCtx, _ = ctx.Value(uncheckItemCtxKey{}).(string)

	if r.itemErr != nil {
		return Item{}, r.itemErr
	}

	return r.fakeRepo.Item(ctx, listID, itemID)
}

func (r *uncheckSpyRepo) SetItemChecked(
	ctx context.Context,
	listID, itemID uuid.UUID,
	checked bool,
	checkedAt *time.Time,
	checkedBy *uuid.UUID,
) (Item, error) {
	r.setCalls++
	r.sawSetCtx, _ = ctx.Value(uncheckItemCtxKey{}).(string)
	r.lastListID, r.lastItemID = listID, itemID
	r.lastChecked, r.lastCheckedAt, r.lastCheckedBy = checked, checkedAt, checkedBy

	if r.setErr != nil {
		return Item{}, r.setErr
	}

	return r.fakeRepo.SetItemChecked(ctx, listID, itemID, checked, checkedAt, checkedBy)
}

// uncheckFixture is what a case needs, including a count of the clock reads the
// service made - only checking consults it, so here it must stay at zero.
type uncheckFixture struct {
	svc      *Service
	repo     *uncheckSpyRepo
	list     List
	item     Item
	nowCalls *int
}

// newUncheckItemFixture builds a Service over a spying repository holding one
// list with a single item, checked off by buyer when checked is true. The
// recorded calls are reset, so a case only sees what UncheckItem itself did.
func newUncheckItemFixture(t *testing.T, checked bool, buyer uuid.UUID) uncheckFixture {
	t.Helper()

	fake := newFakeRepo()
	repo := &uncheckSpyRepo{
		fakeRepo:      fake,
		setCalls:      0,
		sawItemCtx:    "",
		sawSetCtx:     "",
		lastListID:    uuid.Nil,
		lastItemID:    uuid.Nil,
		lastChecked:   false,
		lastCheckedAt: nil,
		lastCheckedBy: nil,
		itemErr:       nil,
		setErr:        nil,
	}

	nowCalls := 0
	svc := NewService(repo)
	svc.now = func() time.Time {
		nowCalls++

		return fake.clock
	}

	list := mustCreateList(t, svc, "Groceries")

	item, err := svc.AddItem(
		context.Background(), list.ID,
		uncheckItemName, uncheckItemQty, uncheckItemUnit, new(uncheckItemNote), false, testActor(),
	)
	if err != nil {
		t.Fatalf("AddItem: %v", err)
	}

	if checked {
		item = mustCheckItem(t, svc, list.ID, item.ID, buyer)
	}

	repo.setCalls, nowCalls = 0, 0

	return uncheckFixture{svc: svc, repo: repo, list: list, item: item, nowCalls: &nowCalls}
}

// assertUncheckedItemIsOpen fails unless the item is back on the open list, with
// no purchase moment and no buyer.
func assertUncheckedItemIsOpen(t *testing.T, got Item) {
	t.Helper()

	if got.Checked {
		t.Errorf("checked = true, want false")
	}

	if got.CheckedAt != nil {
		t.Errorf("checkedAt = %v, want nil", *got.CheckedAt)
	}

	if got.BoughtBy != nil {
		t.Errorf("boughtBy = %+v, want nil - an open item has no buyer", *got.BoughtBy)
	}
}

// assertUncheckedFieldsPreserved fails unless everything uncheck has no business
// touching came through as it was.
func assertUncheckedFieldsPreserved(t *testing.T, got, before Item) {
	t.Helper()

	if got.ID != before.ID || got.ListID != before.ListID {
		t.Errorf("item identity = %v on %v, want %v on %v", got.ID, got.ListID, before.ID, before.ListID)
	}

	if got.Name != before.Name || got.Quantity != before.Quantity || got.Unit != before.Unit {
		t.Errorf(
			"item = %q %d %q, want %q %d %q",
			got.Name, got.Quantity, got.Unit, before.Name, before.Quantity, before.Unit,
		)
	}

	assertNote(t, got.Note, before.Note)

	if got.AddedBy == nil || got.AddedBy.ID != testActor() {
		t.Errorf("addedBy = %+v, want the adder %v", got.AddedBy, testActor())
	}

	if !got.CreatedAt.Equal(before.CreatedAt) {
		t.Errorf("createdAt = %v, want %v", got.CreatedAt, before.CreatedAt)
	}
}

// assertUncheckWrite fails unless the repository was asked exactly once to clear
// the item, with both IDs and the caller's context forwarded to both calls.
func assertUncheckWrite(t *testing.T, repo *uncheckSpyRepo, listID, itemID uuid.UUID) {
	t.Helper()

	if repo.setCalls != 1 {
		t.Fatalf("repository SetItemChecked calls = %d, want 1", repo.setCalls)
	}

	if repo.lastListID != listID || repo.lastItemID != itemID {
		t.Errorf("repository saw %v/%v, want list %v item %v", repo.lastListID, repo.lastItemID, listID, itemID)
	}

	if repo.lastChecked {
		t.Errorf("repository saw checked = true, want false")
	}

	if repo.lastCheckedAt != nil {
		t.Errorf("repository saw checkedAt = %v, want nil", *repo.lastCheckedAt)
	}

	if repo.lastCheckedBy != nil {
		t.Errorf("repository saw checkedBy = %v, want nil", *repo.lastCheckedBy)
	}

	if repo.sawItemCtx != uncheckCtxValue || repo.sawSetCtx != uncheckCtxValue {
		t.Errorf(
			"repository saw context values %q/%q, want the caller's %q on both calls",
			repo.sawItemCtx, repo.sawSetCtx, uncheckCtxValue,
		)
	}
}

// TestUncheckItemReturnsItemToTheOpenList covers the success path: the item comes
// back open with no buyer and nothing else disturbed, the repository is asked for
// exactly that, and the clock is left alone. The actor varies per case because
// uncheck must not attribute anything to whoever performed it.
func TestUncheckItemReturnsItemToTheOpenList(t *testing.T) {
	t.Parallel()

	otherActor := uuid.MustParse("22222222-2222-2222-2222-222222222222")

	tests := []struct {
		name  string
		buyer uuid.UUID
		actor uuid.UUID
	}{
		{name: "the buyer unchecks their own purchase", buyer: testActor(), actor: testActor()},
		{name: "someone else unchecks it", buyer: testActor(), actor: otherActor},
		// Nothing validates the actor and nothing on this path reads it, so the
		// zero UUID has to behave exactly like a real one.
		{name: "the nil actor is accepted", buyer: testActor(), actor: uuid.Nil},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			fixture := newUncheckItemFixture(t, true, testCase.buyer)
			ctx := context.WithValue(context.Background(), uncheckItemCtxKey{}, uncheckCtxValue)

			got, err := fixture.svc.UncheckItem(ctx, fixture.list.ID, fixture.item.ID, testCase.actor)
			if err != nil {
				t.Fatalf("UncheckItem: %v", err)
			}

			assertUncheckedItemIsOpen(t, got)
			assertUncheckedFieldsPreserved(t, got, fixture.item)
			assertUncheckWrite(t, fixture.repo, fixture.list.ID, fixture.item.ID)

			// The clock stamps a purchase; an uncheck has no moment to record.
			if *fixture.nowCalls != 0 {
				t.Errorf("service clock reads = %d, want 0", *fixture.nowCalls)
			}
		})
	}
}

// TestUncheckItemIsIdempotent covers the documented early return: an already-open
// item comes back as it was read, and the repository is never asked to write.
func TestUncheckItemIsIdempotent(t *testing.T) {
	t.Parallel()

	t.Run("an item that was never checked", func(t *testing.T) {
		t.Parallel()

		fixture := newUncheckItemFixture(t, false, uuid.Nil)

		got, err := fixture.svc.UncheckItem(context.Background(), fixture.list.ID, fixture.item.ID, testActor())
		if err != nil {
			t.Fatalf("UncheckItem: %v", err)
		}

		if got != fixture.item {
			t.Errorf("item = %+v, want it returned unchanged as %+v", got, fixture.item)
		}

		if fixture.repo.setCalls != 0 {
			t.Errorf("repository SetItemChecked calls = %d, want 0 - the item is already open", fixture.repo.setCalls)
		}
	})

	t.Run("a second uncheck in a row", func(t *testing.T) {
		t.Parallel()

		fixture := newUncheckItemFixture(t, true, testActor())

		first, err := fixture.svc.UncheckItem(context.Background(), fixture.list.ID, fixture.item.ID, testActor())
		if err != nil {
			t.Fatalf("first UncheckItem: %v", err)
		}

		second, err := fixture.svc.UncheckItem(context.Background(), fixture.list.ID, fixture.item.ID, testActor())
		if err != nil {
			t.Fatalf("second UncheckItem: %v", err)
		}

		if second != first {
			t.Errorf("second uncheck = %+v, want the first result %+v", second, first)
		}

		if fixture.repo.setCalls != 1 {
			t.Errorf("repository SetItemChecked calls = %d, want 1 - only the first writes", fixture.repo.setCalls)
		}
	})
}

// TestUncheckItemErrors covers every way UncheckItem fails: the list-scoped lookup
// finding nothing (an unknown id and a real id under the wrong list are alike),
// and either repository call failing outright. Each failure point has its own
// wrapping, the sentinel survives it, and none of them returns an item.
func TestUncheckItemErrors(t *testing.T) {
	t.Parallel()

	// Cases override one of these, never both.
	noFailure := func(_ *uncheckSpyRepo) {}
	sameItem := func(_ *testing.T, f uncheckFixture) (uuid.UUID, uuid.UUID) { return f.list.ID, f.item.ID }

	tests := []struct {
		name string
		// ids picks the pair to ask for, given the fixture's list and item.
		ids func(t *testing.T, fixture uncheckFixture) (uuid.UUID, uuid.UUID)
		// fail arms the fixture's repository for this case.
		fail       func(repo *uncheckSpyRepo)
		wantErr    error
		wantPrefix string
		wantWrites int
	}{
		{
			name:       "unknown item",
			ids:        func(_ *testing.T, f uncheckFixture) (uuid.UUID, uuid.UUID) { return f.list.ID, uuid.New() },
			fail:       noFailure,
			wantErr:    ErrNotFound,
			wantPrefix: uncheckReadPrefix,
			wantWrites: 0,
		},
		{
			name: "real item under the wrong list",
			ids: func(t *testing.T, f uncheckFixture) (uuid.UUID, uuid.UUID) {
				t.Helper()

				return mustCreateList(t, f.svc, "Hardware").ID, f.item.ID
			},
			fail:       noFailure,
			wantErr:    ErrNotFound,
			wantPrefix: uncheckReadPrefix,
			wantWrites: 0,
		},
		{
			name:       "the read fails",
			ids:        sameItem,
			fail:       func(repo *uncheckSpyRepo) { repo.itemErr = errUncheckItemBoom },
			wantErr:    errUncheckItemBoom,
			wantPrefix: uncheckReadPrefix,
			wantWrites: 0,
		},
		// The item was there a moment ago and is gone by the time the write
		// lands - deleted in between.
		{
			name:       "the write fails after a good read",
			ids:        sameItem,
			fail:       func(repo *uncheckSpyRepo) { repo.setErr = errUncheckItemBoom },
			wantErr:    errUncheckItemBoom,
			wantPrefix: uncheckWritePrefix,
			wantWrites: 1,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var zero Item

			fixture := newUncheckItemFixture(t, true, testActor())
			listID, itemID := testCase.ids(t, fixture)
			testCase.fail(fixture.repo)

			got, err := fixture.svc.UncheckItem(context.Background(), listID, itemID, testActor())
			if !errors.Is(err, testCase.wantErr) {
				t.Fatalf("expected %v, got %v", testCase.wantErr, err)
			}

			if !strings.HasPrefix(err.Error(), testCase.wantPrefix) {
				t.Errorf("error = %q, want it prefixed with %q", err, testCase.wantPrefix)
			}

			if got != zero {
				t.Errorf("item = %+v, want the zero value", got)
			}

			if fixture.repo.setCalls != testCase.wantWrites {
				t.Errorf("repository SetItemChecked calls = %d, want %d", fixture.repo.setCalls, testCase.wantWrites)
			}
		})
	}
}
