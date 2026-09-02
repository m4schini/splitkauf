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

// The item every UpdateItem case below starts from: "Milk", 2 l, note "fresh".
const (
	baseItemName = "Milk"
	baseItemUnit = "l"
	baseItemNote = "fresh"
	baseItemQty  = 2
)

// errUpdateItemBoom stands in for a repository failing for reasons of its own,
// so the wrapping can be checked without one of the domain's sentinels.
var errUpdateItemBoom = errors.New("repository exploded")

// updateItemCtxKey tags the context handed to UpdateItem, so the repository can
// prove the caller's context - and not a fresh one - reached persistence.
type updateItemCtxKey struct{}

// updateSpyRepo records what UpdateItem reaches the repository with, and can
// make it fail. The service's normalisation is only observable through those
// arguments, and a rejected update must never get this far.
type updateSpyRepo struct {
	*fakeRepo

	calls       int
	sawCtxValue string
	lastListID  uuid.UUID
	lastItemID  uuid.UUID
	lastUpdate  ItemUpdate
	err         error
}

func (r *updateSpyRepo) UpdateItem(ctx context.Context, listID, itemID uuid.UUID, update ItemUpdate) (Item, error) {
	r.calls++
	r.sawCtxValue, _ = ctx.Value(updateItemCtxKey{}).(string)
	r.lastListID = listID
	r.lastItemID = itemID
	r.lastUpdate = update

	if r.err != nil {
		return Item{}, r.err
	}

	return r.fakeRepo.UpdateItem(ctx, listID, itemID, update)
}

// newUpdateItemService builds a Service over a spying repository holding one
// list with the single item the cases patch.
func newUpdateItemService(t *testing.T) (*Service, *updateSpyRepo, List, Item) {
	t.Helper()

	fake := newFakeRepo()
	repo := &updateSpyRepo{
		fakeRepo:    fake,
		calls:       0,
		sawCtxValue: "",
		lastListID:  uuid.Nil,
		lastItemID:  uuid.Nil,
		lastUpdate:  itemUpdate(nil, nil, nil, false, nil),
		err:         nil,
	}
	svc := NewService(repo)
	svc.now = func() time.Time { return fake.clock }

	list := mustCreateList(t, svc, "Groceries")

	item, err := svc.AddItem(
		context.Background(), list.ID,
		baseItemName, baseItemQty, baseItemUnit, new(baseItemNote), false, testActor(),
	)
	if err != nil {
		t.Fatalf("AddItem: %v", err)
	}

	return svc, repo, list, item
}

// itemUpdate spells out every field of a partial update, so no case leaves one
// to chance and the tables stay readable.
func itemUpdate(name *string, quantity *int, unit *string, noteSet bool, note *string) ItemUpdate {
	return ItemUpdate{Name: name, Quantity: quantity, Unit: unit, NoteSet: noteSet, Note: note}
}

// assertNote compares a nullable note by value, so a cleared note is
// distinguishable from an unchanged one.
func assertNote(t *testing.T, got, want *string) {
	t.Helper()

	switch {
	case want == nil && got != nil:
		t.Errorf("note = %q, want nil", *got)
	case want != nil && got == nil:
		t.Errorf("note = nil, want %q", *want)
	case want != nil && got != nil && *got != *want:
		t.Errorf("note = %q, want %q", *got, *want)
	}
}

// TestUpdateItemNormalisesFields covers the success path: only the supplied
// fields change, and they are trimmed and canonicalised on the way to the
// repository. The fake applies exactly what it was handed, so the returned item
// shows what was persisted.
func TestUpdateItemNormalisesFields(t *testing.T) {
	t.Parallel()

	atLimit := strings.Repeat("a", maxNameLength)

	tests := []struct {
		name         string
		update       ItemUpdate
		wantName     string
		wantQuantity int
		wantUnit     string
		wantNote     *string
	}{
		// The last-write-wins no-op: nothing is validated, and the repository is
		// still asked to apply an update that touches no field.
		{
			name:         "empty update changes nothing",
			update:       itemUpdate(nil, nil, nil, false, nil),
			wantName:     baseItemName,
			wantQuantity: baseItemQty,
			wantUnit:     baseItemUnit,
			wantNote:     new(baseItemNote),
		},
		{
			name:         "name is trimmed",
			update:       itemUpdate(new("  Oat milk  "), nil, nil, false, nil),
			wantName:     "Oat milk",
			wantQuantity: baseItemQty,
			wantUnit:     baseItemUnit,
			wantNote:     new(baseItemNote),
		},
		// The limit applies after trimming, so padding must not push a name that
		// is exactly at the limit over it.
		{
			name:         "name of exactly the maximum length is accepted",
			update:       itemUpdate(new("  "+atLimit+"  "), nil, nil, false, nil),
			wantName:     atLimit,
			wantQuantity: baseItemQty,
			wantUnit:     baseItemUnit,
			wantNote:     new(baseItemNote),
		},
		// One is the floor, not a value the service rewrites, and a canonical
		// unit passes through untouched.
		{
			name:         "quantity of one and a known unit are accepted",
			update:       itemUpdate(nil, new(1), new("kg"), false, nil),
			wantName:     baseItemName,
			wantQuantity: 1,
			wantUnit:     "kg",
			wantNote:     new(baseItemNote),
		},
		// An explicitly empty unit means "no unit", which is stored as the
		// default rather than as a blank.
		{
			name:         "empty unit becomes the default",
			update:       itemUpdate(nil, nil, new(""), false, nil),
			wantName:     baseItemName,
			wantQuantity: baseItemQty,
			wantUnit:     defaultUnit,
			wantNote:     new(baseItemNote),
		},
		// Without NoteSet the note is not part of the update at all.
		{
			name:         "note without NoteSet is left alone",
			update:       itemUpdate(nil, nil, nil, false, new("  ignored  ")),
			wantName:     baseItemName,
			wantQuantity: baseItemQty,
			wantUnit:     baseItemUnit,
			wantNote:     new(baseItemNote),
		},
		{
			name:         "NoteSet with a nil note clears it",
			update:       itemUpdate(nil, nil, nil, true, nil),
			wantName:     baseItemName,
			wantQuantity: baseItemQty,
			wantUnit:     baseItemUnit,
			wantNote:     nil,
		},
		// A note the user blanked out arrives as whitespace: it clears the note
		// rather than storing an empty string.
		{
			name:         "NoteSet with a blank note clears it",
			update:       itemUpdate(nil, nil, nil, true, new("  \t ")),
			wantName:     baseItemName,
			wantQuantity: baseItemQty,
			wantUnit:     baseItemUnit,
			wantNote:     nil,
		},
		{
			name:         "NoteSet with a padded note trims it",
			update:       itemUpdate(nil, nil, nil, true, new("  buy two  ")),
			wantName:     baseItemName,
			wantQuantity: baseItemQty,
			wantUnit:     baseItemUnit,
			wantNote:     new("buy two"),
		},
		{
			name:         "every field at once",
			update:       itemUpdate(new(" Oat milk "), new(3), new("pack"), true, new(" barista ")),
			wantName:     "Oat milk",
			wantQuantity: 3,
			wantUnit:     "pack",
			wantNote:     new("barista"),
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			svc, repo, list, item := newUpdateItemService(t)

			got, err := svc.UpdateItem(context.Background(), list.ID, item.ID, testCase.update)
			if err != nil {
				t.Fatalf("UpdateItem: %v", err)
			}

			if repo.calls != 1 {
				t.Fatalf("repository UpdateItem calls = %d, want 1", repo.calls)
			}

			if got.ID != item.ID || got.ListID != list.ID {
				t.Errorf("item identity = %v on %v, want %v on %v", got.ID, got.ListID, item.ID, list.ID)
			}

			if got.Name != testCase.wantName {
				t.Errorf("name = %q, want %q", got.Name, testCase.wantName)
			}

			if got.Quantity != testCase.wantQuantity {
				t.Errorf("quantity = %d, want %d", got.Quantity, testCase.wantQuantity)
			}

			if got.Unit != testCase.wantUnit {
				t.Errorf("unit = %q, want %q", got.Unit, testCase.wantUnit)
			}

			assertNote(t, got.Note, testCase.wantNote)
		})
	}
}

// TestUpdateItemValidation pins the rejected updates, the field each is blamed
// on, and the order the fields are checked in. Every case must fail before the
// repository is touched, and must arrive unwrapped so the REST layer maps it to
// a validation problem rather than a server error.
func TestUpdateItemValidation(t *testing.T) {
	t.Parallel()

	overLimit := strings.Repeat("a", maxNameLength+1)
	// 201 x "ä" is 201 runes (402 bytes): the limit counts runes.
	multiByte := strings.Repeat("ä", maxNameLength+1)
	longNote := strings.Repeat("ü", maxNoteLength+1)

	tests := []struct {
		name      string
		update    ItemUpdate
		wantField string
	}{
		{name: "empty name", update: itemUpdate(new(""), nil, nil, false, nil), wantField: fieldName},
		{name: "blank name", update: itemUpdate(new("   "), nil, nil, false, nil), wantField: fieldName},
		{name: "name one rune too long", update: itemUpdate(&overLimit, nil, nil, false, nil), wantField: fieldName},
		{name: "multi-byte name too long", update: itemUpdate(&multiByte, nil, nil, false, nil), wantField: fieldName},
		{name: "note one rune too long", update: itemUpdate(nil, nil, nil, true, &longNote), wantField: fieldNote},
		// Unlike creation, an explicit zero here is a mistake rather than a
		// request for the default of one.
		{name: "quantity of zero", update: itemUpdate(nil, new(0), nil, false, nil), wantField: fieldQuantity},
		{name: "negative quantity", update: itemUpdate(nil, new(-1), nil, false, nil), wantField: fieldQuantity},
		{name: "unknown unit", update: itemUpdate(nil, nil, new("stone"), false, nil), wantField: fieldUnit},
		{name: "bad name outranks bad quantity", update: itemUpdate(new(" "), new(0), nil, false, nil), wantField: fieldName},
		{
			name:      "bad quantity outranks bad unit",
			update:    itemUpdate(nil, new(0), new("stone"), false, nil),
			wantField: fieldQuantity,
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

			assertValidationError(t, err, testCase.wantField)

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

// TestUpdateItemForwardsToRepository checks that the caller's context and both
// IDs arrive unchanged, and that an update with nothing to normalise is passed
// on exactly as it came in.
func TestUpdateItemForwardsToRepository(t *testing.T) {
	t.Parallel()

	svc, repo, list, item := newUpdateItemService(t)
	ctx := context.WithValue(context.Background(), updateItemCtxKey{}, "caller")
	// A note supplied without NoteSet is not part of the update, so there is
	// nothing here for the service to validate or rewrite.
	update := itemUpdate(nil, nil, nil, false, new("  ignored  "))

	if _, err := svc.UpdateItem(ctx, list.ID, item.ID, update); err != nil {
		t.Fatalf("UpdateItem: %v", err)
	}

	if repo.sawCtxValue != "caller" {
		t.Errorf("repository saw context value %q, want %q", repo.sawCtxValue, "caller")
	}

	if repo.lastListID != list.ID || repo.lastItemID != item.ID {
		t.Errorf("repository saw %v/%v, want list %v item %v", repo.lastListID, repo.lastItemID, list.ID, item.ID)
	}

	if repo.lastUpdate != update {
		t.Errorf("repository saw update %+v, want it forwarded unchanged", repo.lastUpdate)
	}
}

// TestUpdateItemRepositoryErrors covers the persistence failures: they come
// back wrapped, so callers have to unwrap, and no item comes with them.
func TestUpdateItemRepositoryErrors(t *testing.T) {
	t.Parallel()

	const wantPrefix = "updating item: "

	t.Run("missing item", func(t *testing.T) {
		t.Parallel()

		var zero Item

		svc, _, list, _ := newUpdateItemService(t)

		got, err := svc.UpdateItem(context.Background(), list.ID, uuid.New(), itemUpdate(new("Bread"), nil, nil, false, nil))
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("expected ErrNotFound, got %v", err)
		}

		if !strings.HasPrefix(err.Error(), wantPrefix) {
			t.Errorf("error = %q, want it prefixed with %q", err, wantPrefix)
		}

		if got != zero {
			t.Errorf("item = %+v, want the zero value", got)
		}
	})

	// List-scoping is the repository's job: the service forwards both IDs and
	// lets the mismatch come back as ErrNotFound.
	t.Run("item on another list", func(t *testing.T) {
		t.Parallel()

		svc, _, _, item := newUpdateItemService(t)
		other := mustCreateList(t, svc, "Hardware")

		_, err := svc.UpdateItem(context.Background(), other.ID, item.ID, itemUpdate(nil, nil, nil, false, nil))
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("expected ErrNotFound, got %v", err)
		}
	})

	t.Run("arbitrary repository failure", func(t *testing.T) {
		t.Parallel()

		var zero Item

		svc, repo, list, item := newUpdateItemService(t)
		repo.err = errUpdateItemBoom

		got, err := svc.UpdateItem(context.Background(), list.ID, item.ID, itemUpdate(nil, new(4), nil, false, nil))
		if !errors.Is(err, errUpdateItemBoom) {
			t.Fatalf("expected the repository error, got %v", err)
		}

		if !strings.HasPrefix(err.Error(), wantPrefix) {
			t.Errorf("error = %q, want it prefixed with %q", err, wantPrefix)
		}

		if got != zero {
			t.Errorf("item = %+v, want the zero value", got)
		}
	})
}
