// SPDX-License-Identifier: CC0-1.0

package lists

import (
	"context"
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

func (r *fakeRepo) CreateList(_ context.Context, name string, createdBy uuid.UUID) (List, error) {
	list := &List{
		ID:               uuid.New(),
		Name:             name,
		OpenItemCount:    0,
		CheckedItemCount: 0,
		CreatedBy:        &Actor{ID: createdBy, Name: ""},
		CreatedAt:        r.clock,
		UpdatedAt:        r.clock,
	}
	r.lists[list.ID] = list

	return *list, nil
}

func (r *fakeRepo) Lists(_ context.Context) ([]List, error) {
	out := make([]List, 0, len(r.lists))
	for id := range r.lists {
		r.recount(id)
		out = append(out, *r.lists[id])
	}

	return out, nil
}

func (r *fakeRepo) List(_ context.Context, listID uuid.UUID) (List, error) {
	list, ok := r.lists[listID]
	if !ok {
		return List{}, ErrNotFound
	}

	r.recount(listID)

	return *list, nil
}

func (r *fakeRepo) ListItems(_ context.Context, listID uuid.UUID) ([]Item, error) {
	if _, ok := r.lists[listID]; !ok {
		return nil, ErrNotFound
	}

	out := make([]Item, 0)

	for _, item := range r.items {
		if item.ListID == listID && !r.deleted[item.ID] {
			out = append(out, *item)
		}
	}

	return out, nil
}

func (r *fakeRepo) RenameList(_ context.Context, listID uuid.UUID, name string) (List, error) {
	list, ok := r.lists[listID]
	if !ok {
		return List{}, ErrNotFound
	}

	list.Name = name
	list.UpdatedAt = r.clock
	r.recount(listID)

	return *list, nil
}

func (r *fakeRepo) DeleteList(_ context.Context, listID uuid.UUID) error {
	if _, ok := r.lists[listID]; !ok {
		return ErrNotFound
	}

	delete(r.lists, listID)

	for itemID, item := range r.items {
		if item.ListID == listID {
			delete(r.items, itemID) // cascade
		}
	}

	return nil
}

func (r *fakeRepo) CopyList(_ context.Context, sourceID uuid.UUID, name string, actor uuid.UUID) (List, error) {
	if _, ok := r.lists[sourceID]; !ok {
		return List{}, ErrNotFound
	}

	copied := &List{
		ID:               uuid.New(),
		Name:             name,
		OpenItemCount:    0,
		CheckedItemCount: 0,
		CreatedBy:        &Actor{ID: actor, Name: ""},
		CreatedAt:        r.clock,
		UpdatedAt:        r.clock,
	}

	r.lists[copied.ID] = copied
	for _, item := range r.items {
		if item.ListID != sourceID || r.deleted[item.ID] {
			continue
		}

		itemCopy := *item
		itemCopy.ID = uuid.New()
		itemCopy.ListID = copied.ID
		itemCopy.Checked = false
		itemCopy.CheckedAt = nil
		itemCopy.AddedBy = &Actor{ID: actor, Name: ""}
		itemCopy.BoughtBy = nil
		itemCopy.CreatedAt = r.clock
		itemCopy.UpdatedAt = r.clock
		r.items[itemCopy.ID] = &itemCopy
	}

	r.recount(copied.ID)

	return *copied, nil
}

func (r *fakeRepo) AddItem(
	_ context.Context,
	listID uuid.UUID,
	name string,
	quantity int,
	unit string,
	note *string,
	checked bool,
	addedBy uuid.UUID,
) (Item, error) {
	if _, ok := r.lists[listID]; !ok {
		return Item{}, ErrNotFound
	}

	var (
		checkedAt *time.Time
		boughtBy  *Actor
	)

	if checked {
		at := r.clock
		checkedAt = &at
		// A create that arrives already checked was checked by its adder
		// offline; mirror the adapter and credit them as the buyer too.
		boughtBy = &Actor{ID: addedBy, Name: ""}
	}

	item := &Item{
		ID:        uuid.New(),
		ListID:    listID,
		Name:      name,
		Quantity:  quantity,
		Unit:      unit,
		Note:      note,
		Checked:   checked,
		CheckedAt: checkedAt,
		AddedBy:   &Actor{ID: addedBy, Name: ""},
		BoughtBy:  boughtBy,
		CreatedAt: r.clock,
		UpdatedAt: r.clock,
	}
	r.items[item.ID] = item

	return *item, nil
}

func (r *fakeRepo) Item(_ context.Context, listID, itemID uuid.UUID) (Item, error) {
	item, ok := r.items[itemID]
	if !ok || item.ListID != listID || r.deleted[itemID] {
		return Item{}, ErrNotFound
	}

	return *item, nil
}

func (r *fakeRepo) UpdateItem(_ context.Context, listID, itemID uuid.UUID, update ItemUpdate) (Item, error) {
	item, ok := r.items[itemID]
	if !ok || item.ListID != listID || r.deleted[itemID] {
		return Item{}, ErrNotFound
	}

	if update.Name != nil {
		item.Name = *update.Name
	}

	if update.Quantity != nil {
		item.Quantity = *update.Quantity
	}

	if update.Unit != nil {
		item.Unit = *update.Unit
	}

	if update.NoteSet {
		item.Note = update.Note
	}

	item.UpdatedAt = r.clock

	return *item, nil
}

func (r *fakeRepo) DeleteItem(_ context.Context, listID, itemID uuid.UUID) error {
	item, ok := r.items[itemID]
	if !ok || item.ListID != listID || r.deleted[itemID] {
		return ErrNotFound
	}

	r.deleted[itemID] = true

	return nil
}

func (r *fakeRepo) RestoreItem(_ context.Context, listID, itemID uuid.UUID) (Item, error) {
	item, ok := r.items[itemID]
	if !ok || item.ListID != listID {
		return Item{}, ErrNotFound
	}

	delete(r.deleted, itemID)
	item.UpdatedAt = r.clock

	return *item, nil
}

func (r *fakeRepo) SetItemChecked(
	_ context.Context,
	listID, itemID uuid.UUID,
	checked bool,
	checkedAt *time.Time,
	checkedBy *uuid.UUID,
) (Item, error) {
	item, ok := r.items[itemID]
	if !ok || item.ListID != listID || r.deleted[itemID] {
		return Item{}, ErrNotFound
	}

	item.Checked = checked
	item.CheckedAt = checkedAt
	// nil clears the buyer, exactly as the adapter's NULL write does.
	item.BoughtBy = nil
	if checkedBy != nil {
		item.BoughtBy = &Actor{ID: *checkedBy, Name: ""}
	}

	item.UpdatedAt = r.clock

	return *item, nil
}

func (r *fakeRepo) recount(listID uuid.UUID) {
	list, ok := r.lists[listID]
	if !ok {
		return
	}

	open, checked := 0, 0

	for _, item := range r.items {
		if item.ListID != listID || r.deleted[item.ID] {
			continue
		}

		if item.Checked {
			checked++
		} else {
			open++
		}
	}

	list.OpenItemCount = open
	list.CheckedItemCount = checked
}
