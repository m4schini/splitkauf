// SPDX-License-Identifier: CC0-1.0

package v1_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/m4schini/splitkauf/events"
	"github.com/m4schini/splitkauf/lists"
)

// TestCreateListPublishesListEvent asserts a successful list create emits a
// single {lists} reload hint (representative of the list-mutation handlers).
func TestCreateListPublishesListEvent(t *testing.T) {
	t.Parallel()

	var svc fakeService

	svc.createList = func(_ context.Context, name string, _ uuid.UUID) (lists.List, error) {
		return makeList(uuid.New(), name, 0, 0), nil
	}

	pub := new(capturingPublisher)
	srv := newServerWithEvents(t, &svc, pub)

	resp := postJSON(t, srv.URL+"/api/v1/lists", `{"name":"Groceries"}`)
	defer closeBody(t, resp)

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201", resp.StatusCode)
	}

	got := pub.captured()
	if len(got) != 1 {
		t.Fatalf("published %d events, want 1: %+v", len(got), got)
	}

	if got[0].Type != events.TypeLists {
		t.Errorf("event type = %q, want %q", got[0].Type, events.TypeLists)
	}

	if got[0].ListID != "" {
		t.Errorf("lists event carries listId %q, want empty", got[0].ListID)
	}
}

// TestCopyListPublishesListEvent asserts a successful copy emits a single
// {lists} reload hint, so other clients pick the new list up in their overview.
func TestCopyListPublishesListEvent(t *testing.T) {
	t.Parallel()

	var svc fakeService

	svc.copyList = func(_ context.Context, _ uuid.UUID, _ string, _ uuid.UUID) (lists.List, error) {
		return makeList(uuid.New(), "Groceries (copy)", 0, 0), nil
	}

	pub := new(capturingPublisher)
	srv := newServerWithEvents(t, &svc, pub)

	resp := postJSON(t, srv.URL+"/api/v1/lists/"+uuid.New().String()+"/copy", "")
	defer closeBody(t, resp)

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201", resp.StatusCode)
	}

	got := pub.captured()
	if len(got) != 1 {
		t.Fatalf("published %d events, want 1: %+v", len(got), got)
	}

	if got[0].Type != events.TypeLists {
		t.Errorf("event type = %q, want %q", got[0].Type, events.TypeLists)
	}

	if got[0].ListID != "" {
		t.Errorf("lists event carries listId %q, want empty", got[0].ListID)
	}
}

// TestFailedCopyPublishesNothing proves the copy's publish is strictly after
// success: a missing source emits no hint.
func TestFailedCopyPublishesNothing(t *testing.T) {
	t.Parallel()

	var svc fakeService

	svc.copyList = func(_ context.Context, _ uuid.UUID, _ string, _ uuid.UUID) (lists.List, error) {
		return lists.List{}, lists.ErrNotFound
	}

	pub := new(capturingPublisher)
	srv := newServerWithEvents(t, &svc, pub)

	resp := postJSON(t, srv.URL+"/api/v1/lists/"+uuid.New().String()+"/copy", "")
	defer closeBody(t, resp)

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}

	if got := pub.captured(); len(got) != 0 {
		t.Errorf("published %d events on error, want 0: %+v", len(got), got)
	}
}

// TestCheckItemPublishesItemEvent asserts a successful check emits a single
// {items, listId} reload hint (representative of the item-mutation handlers).
func TestCheckItemPublishesItemEvent(t *testing.T) {
	t.Parallel()

	listID, itemID := uuid.New(), uuid.New()

	var svc fakeService

	svc.checkItem = func(_ context.Context, lid, iid, _ uuid.UUID) (lists.Item, error) {
		item := makeItem(iid, lid, itemNameMilk, 0)
		item.Checked = true

		return item, nil
	}

	pub := new(capturingPublisher)
	srv := newServerWithEvents(t, &svc, pub)

	resp := postJSON(t, srv.URL+"/api/v1/lists/"+listID.String()+"/items/"+itemID.String()+"/check", "")
	defer closeBody(t, resp)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	got := pub.captured()
	if len(got) != 1 {
		t.Fatalf("published %d events, want 1: %+v", len(got), got)
	}

	if got[0].Type != events.TypeItems {
		t.Errorf("event type = %q, want %q", got[0].Type, events.TypeItems)
	}

	if got[0].ListID != listID.String() {
		t.Errorf("event listId = %q, want %q", got[0].ListID, listID.String())
	}
}

// TestRestoreItemPublishesItemEvent asserts a successful restore emits the same
// single {items, listId} reload hint the check/uncheck handlers do.
func TestRestoreItemPublishesItemEvent(t *testing.T) {
	t.Parallel()

	listID, itemID := uuid.New(), uuid.New()

	var svc fakeService

	svc.restoreItem = func(_ context.Context, lid, iid uuid.UUID) (lists.Item, error) {
		return makeItem(iid, lid, itemNameMilk, 0), nil
	}

	pub := new(capturingPublisher)
	srv := newServerWithEvents(t, &svc, pub)

	resp := postJSON(t, srv.URL+"/api/v1/lists/"+listID.String()+"/items/"+itemID.String()+"/restore", "")
	defer closeBody(t, resp)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	got := pub.captured()
	if len(got) != 1 {
		t.Fatalf("published %d events, want 1: %+v", len(got), got)
	}

	if got[0].Type != events.TypeItems {
		t.Errorf("event type = %q, want %q", got[0].Type, events.TypeItems)
	}

	if got[0].ListID != listID.String() {
		t.Errorf("event listId = %q, want %q", got[0].ListID, listID.String())
	}
}

// TestNoEventOnMutationError proves the publish is strictly after success: a
// failing mutation (not-found) emits nothing.
func TestNoEventOnMutationError(t *testing.T) {
	t.Parallel()

	listID, itemID := uuid.New(), uuid.New()

	var svc fakeService

	svc.checkItem = func(_ context.Context, _, _, _ uuid.UUID) (lists.Item, error) {
		return lists.Item{}, lists.ErrNotFound
	}

	pub := new(capturingPublisher)
	srv := newServerWithEvents(t, &svc, pub)

	resp := postJSON(t, srv.URL+"/api/v1/lists/"+listID.String()+"/items/"+itemID.String()+"/check", "")
	defer closeBody(t, resp)

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}

	if got := pub.captured(); len(got) != 0 {
		t.Errorf("published %d events on error, want 0: %+v", len(got), got)
	}
}
