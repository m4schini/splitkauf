// SPDX-License-Identifier: CC0-1.0

package lists

import (
	"context"
	"fmt"
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

// CreateList validates the name and persists a new, empty list, credited to
// actor as its creator.
func (s *Service) CreateList(ctx context.Context, name string, actor uuid.UUID) (List, error) {
	clean, err := validateListName(name)
	if err != nil {
		return List{}, err
	}

	list, err := s.repo.CreateList(ctx, clean, actor)
	if err != nil {
		return List{}, fmt.Errorf("creating list: %w", err)
	}

	return list, nil
}

// Lists returns every list with its item-count summary.
func (s *Service) Lists(ctx context.Context) ([]List, error) {
	lists, err := s.repo.Lists(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting lists: %w", err)
	}

	return lists, nil
}

// GetList returns a single list together with all of its items. It returns
// ErrNotFound when the list does not exist.
func (s *Service) GetList(ctx context.Context, listID uuid.UUID) (List, []Item, error) {
	list, err := s.repo.List(ctx, listID)
	if err != nil {
		return List{}, nil, fmt.Errorf("getting list: %w", err)
	}

	items, err := s.repo.ListItems(ctx, listID)
	if err != nil {
		return List{}, nil, fmt.Errorf("getting list items: %w", err)
	}

	return list, items, nil
}

// RenameList validates the new name and renames the list. It returns
// ErrNotFound when the list does not exist.
func (s *Service) RenameList(ctx context.Context, listID uuid.UUID, name string) (List, error) {
	clean, err := validateListName(name)
	if err != nil {
		return List{}, err
	}

	list, err := s.repo.RenameList(ctx, listID, clean)
	if err != nil {
		return List{}, fmt.Errorf("renaming list: %w", err)
	}

	return list, nil
}

// CopyList creates a new list holding a copy of every non-deleted item of the
// source list, each reset to unchecked. An empty name means "derive one": the
// copy is named "«source name» (copy)". A supplied name is validated like any
// other list name. The copy (and every item on it) is attributed to actor, not
// to whoever created the source. It returns ErrNotFound when the source does
// not exist.
func (s *Service) CopyList(ctx context.Context, listID uuid.UUID, name string, actor uuid.UUID) (List, error) {
	source, err := s.repo.List(ctx, listID)
	if err != nil {
		return List{}, fmt.Errorf("getting source list: %w", err)
	}

	clean := copyListName(source.Name)
	if name != "" {
		clean, err = validateListName(name)
		if err != nil {
			return List{}, err
		}
	}

	list, err := s.repo.CopyList(ctx, listID, clean, actor)
	if err != nil {
		return List{}, fmt.Errorf("copying list: %w", err)
	}

	return list, nil
}

// DeleteList deletes a list and (via the repository's cascade) all its items.
// It returns ErrNotFound when the list does not exist.
func (s *Service) DeleteList(ctx context.Context, id uuid.UUID) error {
	if err := s.repo.DeleteList(ctx, id); err != nil {
		return fmt.Errorf("deleting list: %w", err)
	}

	return nil
}

// AddItem validates the name, applies the quantity default, validates the unit
// (empty defaults to "amount"), normalises the note, and adds the item to the
// list. When checked is true the item is created
// already checked off (used when an offline check folds into a queued create);
// the repository sets checkedAt at insert. It returns ErrNotFound when the list
// does not exist.
func (s *Service) AddItem(
	ctx context.Context,
	listID uuid.UUID,
	name string,
	quantity int,
	unit string,
	note *string,
	checked bool,
	actor uuid.UUID,
) (Item, error) {
	clean, err := validateItemName(name)
	if err != nil {
		return Item{}, err
	}

	qty, err := normalizeQuantity(quantity)
	if err != nil {
		return Item{}, err
	}

	cleanUnit, err := validateUnit(unit)
	if err != nil {
		return Item{}, err
	}

	item, err := s.repo.AddItem(ctx, listID, clean, qty, cleanUnit, normalizeNote(note), checked, actor)
	if err != nil {
		return Item{}, fmt.Errorf("adding item: %w", err)
	}

	return item, nil
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
			return Item{}, &ValidationError{Field: fieldQuantity, Message: "quantity must be at least 1"}
		}
	}

	if update.Unit != nil {
		cleanUnit, err := validateUnit(*update.Unit)
		if err != nil {
			return Item{}, err
		}

		update.Unit = &cleanUnit
	}

	if update.NoteSet {
		update.Note = normalizeNote(update.Note)
	}

	item, err := s.repo.UpdateItem(ctx, listID, itemID, update)
	if err != nil {
		return Item{}, fmt.Errorf("updating item: %w", err)
	}

	return item, nil
}

// DeleteItem removes an item from a list. The removal is a soft delete: the row
// is kept with deleted_at set so the action replays offline and can be undone.
// It returns ErrNotFound when the item does not exist (or is already deleted).
func (s *Service) DeleteItem(ctx context.Context, listID, itemID uuid.UUID) error {
	if err := s.repo.DeleteItem(ctx, listID, itemID); err != nil {
		return fmt.Errorf("deleting item: %w", err)
	}

	return nil
}

// RestoreItem clears a soft delete, returning the restored item. It is
// idempotent: restoring an item that is not deleted returns it unchanged. It
// returns ErrNotFound when the item does not exist on the list.
func (s *Service) RestoreItem(ctx context.Context, listID, itemID uuid.UUID) (Item, error) {
	item, err := s.repo.RestoreItem(ctx, listID, itemID)
	if err != nil {
		return Item{}, fmt.Errorf("restoring item: %w", err)
	}

	return item, nil
}

// CheckItem marks an item as checked, crediting actor as its buyer. It is an
// idempotent state transition: an already-checked item is returned unchanged
// (its checkedAt and original buyer are preserved).
func (s *Service) CheckItem(ctx context.Context, listID, itemID, actor uuid.UUID) (Item, error) {
	return s.setChecked(ctx, listID, itemID, true, actor)
}

// UncheckItem returns a checked item to the open list, clearing its buyer. It
// is idempotent: an already-open item is returned unchanged.
func (s *Service) UncheckItem(ctx context.Context, listID, itemID, actor uuid.UUID) (Item, error) {
	return s.setChecked(ctx, listID, itemID, false, actor)
}

// setChecked performs the shared check/uncheck logic: it loads the item, and
// only writes when the checked state actually changes, keeping the transition
// idempotent. When checking, checkedAt is set to now and actor is credited as
// the buyer; when unchecking, both are cleared — an item back on the open list
// has not been bought by anyone.
//
// The early return on an unchanged state is what keeps a re-check from
// reassigning an item someone else already bought.
func (s *Service) setChecked(
	ctx context.Context,
	listID, itemID uuid.UUID,
	checked bool,
	actor uuid.UUID,
) (Item, error) {
	item, err := s.repo.Item(ctx, listID, itemID)
	if err != nil {
		return Item{}, fmt.Errorf("getting item: %w", err)
	}

	if item.Checked == checked {
		return item, nil
	}

	var (
		checkedAt *time.Time
		checkedBy *uuid.UUID
	)

	if checked {
		t := s.now()
		checkedAt = &t
		checkedBy = &actor
	}

	result, err := s.repo.SetItemChecked(ctx, listID, itemID, checked, checkedAt, checkedBy)
	if err != nil {
		return Item{}, fmt.Errorf("setting item checked: %w", err)
	}

	return result, nil
}
