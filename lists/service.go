// SPDX-License-Identifier: TODO

package lists

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// Service holds the domain logic for the M1 list and item operations. It
// validates and normalises input, then delegates persistence to a Repository.
// It is transport-agnostic: the REST layer maps its errors to problems.
type Service struct {
	repo Repository
	now  func() time.Time
}

// NewService constructs a Service over the given Repository. It uses the wall
// clock for check/uncheck timestamps.
func NewService(repo Repository) *Service {
	return &Service{repo: repo, now: time.Now}
}

// CreateList validates the name and persists a new, empty list.
func (s *Service) CreateList(ctx context.Context, name string) (List, error) {
	clean, err := validateListName(name)
	if err != nil {
		return List{}, err
	}
	return s.repo.CreateList(ctx, clean)
}

// Lists returns every list with its item-count summary.
func (s *Service) Lists(ctx context.Context) ([]List, error) {
	return s.repo.Lists(ctx)
}

// GetList returns a single list together with all of its items. It returns
// ErrNotFound when the list does not exist.
func (s *Service) GetList(ctx context.Context, id uuid.UUID) (List, []Item, error) {
	list, err := s.repo.List(ctx, id)
	if err != nil {
		return List{}, nil, err
	}
	items, err := s.repo.ListItems(ctx, id)
	if err != nil {
		return List{}, nil, err
	}
	return list, items, nil
}

// RenameList validates the new name and renames the list. It returns
// ErrNotFound when the list does not exist.
func (s *Service) RenameList(ctx context.Context, id uuid.UUID, name string) (List, error) {
	clean, err := validateListName(name)
	if err != nil {
		return List{}, err
	}
	return s.repo.RenameList(ctx, id, clean)
}

// DeleteList deletes a list and (via the repository's cascade) all its items.
// It returns ErrNotFound when the list does not exist.
func (s *Service) DeleteList(ctx context.Context, id uuid.UUID) error {
	return s.repo.DeleteList(ctx, id)
}

// AddItem validates the name, applies the quantity default, normalises the
// note, and adds the item to the list. When checked is true the item is created
// already checked off (used when an offline check folds into a queued create);
// the repository sets checkedAt at insert. It returns ErrNotFound when the list
// does not exist.
func (s *Service) AddItem(ctx context.Context, listID uuid.UUID, name string, quantity int, note *string, checked bool) (Item, error) {
	clean, err := validateItemName(name)
	if err != nil {
		return Item{}, err
	}
	qty, err := normalizeQuantity(quantity)
	if err != nil {
		return Item{}, err
	}
	return s.repo.AddItem(ctx, listID, clean, qty, normalizeNote(note), checked)
}

// UpdateItem applies a partial update to an item: only the fields present in
// update are changed (last-write-wins). It validates any supplied name and
// quantity. It returns ErrNotFound when the item does not exist on the list.
func (s *Service) UpdateItem(ctx context.Context, listID, itemID uuid.UUID, update ItemUpdate) (Item, error) {
	if update.Name != nil {
		clean, err := validateItemName(*update.Name)
		if err != nil {
			return Item{}, err
		}
		update.Name = &clean
	}
	if update.Quantity != nil {
		if *update.Quantity < 1 {
			return Item{}, &ValidationError{Field: "quantity", Message: "quantity must be at least 1"}
		}
	}
	if update.NoteSet {
		update.Note = normalizeNote(update.Note)
	}
	return s.repo.UpdateItem(ctx, listID, itemID, update)
}

// DeleteItem removes an item from a list. The removal is a soft delete: the row
// is kept with deleted_at set so the action replays offline and can be undone.
// It returns ErrNotFound when the item does not exist (or is already deleted).
func (s *Service) DeleteItem(ctx context.Context, listID, itemID uuid.UUID) error {
	return s.repo.DeleteItem(ctx, listID, itemID)
}

// RestoreItem clears a soft delete, returning the restored item. It is
// idempotent: restoring an item that is not deleted returns it unchanged. It
// returns ErrNotFound when the item does not exist on the list.
func (s *Service) RestoreItem(ctx context.Context, listID, itemID uuid.UUID) (Item, error) {
	return s.repo.RestoreItem(ctx, listID, itemID)
}

// CheckItem marks an item as checked. It is an idempotent state transition:
// an already-checked item is returned unchanged (its checkedAt is preserved).
func (s *Service) CheckItem(ctx context.Context, listID, itemID uuid.UUID) (Item, error) {
	return s.setChecked(ctx, listID, itemID, true)
}

// UncheckItem returns a checked item to the open list. It is idempotent: an
// already-open item is returned unchanged.
func (s *Service) UncheckItem(ctx context.Context, listID, itemID uuid.UUID) (Item, error) {
	return s.setChecked(ctx, listID, itemID, false)
}

// setChecked performs the shared check/uncheck logic: it loads the item, and
// only writes when the checked state actually changes, keeping the transition
// idempotent. When checking, checkedAt is set to now; when unchecking, it is
// cleared.
func (s *Service) setChecked(ctx context.Context, listID, itemID uuid.UUID, checked bool) (Item, error) {
	item, err := s.repo.Item(ctx, listID, itemID)
	if err != nil {
		return Item{}, err
	}
	if item.Checked == checked {
		return item, nil
	}
	var checkedAt *time.Time
	if checked {
		t := s.now()
		checkedAt = &t
	}
	return s.repo.SetItemChecked(ctx, listID, itemID, checked, checkedAt)
}
