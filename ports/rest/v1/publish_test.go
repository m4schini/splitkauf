// SPDX-License-Identifier: TODO

package v1_test

import (
	"bytes"
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/m4schini/splitkauf/events"
	"github.com/m4schini/splitkauf/lists"
)

// patchJSON PATCHes a JSON body and returns the response; the caller closes it.
func patchJSON(t *testing.T, url, body string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPatch, url, bytes.NewBufferString(body))
	if err != nil {
		t.Fatalf("new PATCH request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PATCH %s: %v", url, err)
	}
	return resp
}

// TestCreateListPublishesListEvent asserts a successful list create emits a
// single {lists} reload hint (representative of the list-mutation handlers).
func TestCreateListPublishesListEvent(t *testing.T) {
	svc := &fakeService{createList: func(_ context.Context, name string, _ uuid.UUID) (lists.List, error) {
		return lists.List{ID: uuid.New(), Name: name}, nil
	}}
	pub := &capturingPublisher{}
	srv := newServerWithEvents(t, svc, pub)

	resp := postJSON(t, srv.URL+"/api/v1/lists", `{"name":"Groceries"}`)
	defer resp.Body.Close()
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
	svc := &fakeService{copyList: func(_ context.Context, _ uuid.UUID, _ string, _ uuid.UUID) (lists.List, error) {
		return lists.List{ID: uuid.New(), Name: "Groceries (copy)"}, nil
	}}
	pub := &capturingPublisher{}
	srv := newServerWithEvents(t, svc, pub)

	resp := postJSON(t, srv.URL+"/api/v1/lists/"+uuid.New().String()+"/copy", "")
	defer resp.Body.Close()
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
	svc := &fakeService{copyList: func(_ context.Context, _ uuid.UUID, _ string, _ uuid.UUID) (lists.List, error) {
		return lists.List{}, lists.ErrNotFound
	}}
	pub := &capturingPublisher{}
	srv := newServerWithEvents(t, svc, pub)

	resp := postJSON(t, srv.URL+"/api/v1/lists/"+uuid.New().String()+"/copy", "")
	defer resp.Body.Close()
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
	listID, itemID := uuid.New(), uuid.New()
	svc := &fakeService{checkItem: func(_ context.Context, lid, iid uuid.UUID, _ uuid.UUID) (lists.Item, error) {
		return lists.Item{ID: iid, ListID: lid, Name: "milk", Checked: true}, nil
	}}
	pub := &capturingPublisher{}
	srv := newServerWithEvents(t, svc, pub)

	resp := postJSON(t, srv.URL+"/api/v1/lists/"+listID.String()+"/items/"+itemID.String()+"/check", "")
	defer resp.Body.Close()
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
	listID, itemID := uuid.New(), uuid.New()
	svc := &fakeService{restoreItem: func(_ context.Context, lid, iid uuid.UUID) (lists.Item, error) {
		return lists.Item{ID: iid, ListID: lid, Name: "milk"}, nil
	}}
	pub := &capturingPublisher{}
	srv := newServerWithEvents(t, svc, pub)

	resp := postJSON(t, srv.URL+"/api/v1/lists/"+listID.String()+"/items/"+itemID.String()+"/restore", "")
	defer resp.Body.Close()
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
	listID, itemID := uuid.New(), uuid.New()
	svc := &fakeService{checkItem: func(_ context.Context, _, _ uuid.UUID, _ uuid.UUID) (lists.Item, error) {
		return lists.Item{}, lists.ErrNotFound
	}}
	pub := &capturingPublisher{}
	srv := newServerWithEvents(t, svc, pub)

	resp := postJSON(t, srv.URL+"/api/v1/lists/"+listID.String()+"/items/"+itemID.String()+"/check", "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}

	if got := pub.captured(); len(got) != 0 {
		t.Errorf("published %d events on error, want 0: %+v", len(got), got)
	}
}

// statefulService is a minimal in-memory ListService for convergence tests. It
// applies updates and check/uncheck as absolute writes, mirroring the real
// domain's last-write-wins + absolute-set semantics so the handler-layer tests
// can pin US-S.3 behaviour without a database.
type statefulService struct {
	mu    sync.Mutex
	items map[uuid.UUID]*lists.Item
}

func newStatefulService() *statefulService {
	return &statefulService{items: make(map[uuid.UUID]*lists.Item)}
}

func (s *statefulService) put(it lists.Item) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := it
	s.items[it.ID] = &cp
}

func (s *statefulService) get(id uuid.UUID) (lists.Item, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	it, ok := s.items[id]
	if !ok {
		return lists.Item{}, false
	}
	return *it, true
}

func (s *statefulService) UpdateItem(_ context.Context, listID, itemID uuid.UUID, update lists.ItemUpdate) (lists.Item, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	it, ok := s.items[itemID]
	if !ok || it.ListID != listID {
		return lists.Item{}, lists.ErrNotFound
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
	it.UpdatedAt = time.Now()
	return *it, nil
}

func (s *statefulService) CheckItem(_ context.Context, listID, itemID uuid.UUID, _ uuid.UUID) (lists.Item, error) {
	return s.setChecked(listID, itemID, true)
}

func (s *statefulService) UncheckItem(_ context.Context, listID, itemID uuid.UUID, _ uuid.UUID) (lists.Item, error) {
	return s.setChecked(listID, itemID, false)
}

func (s *statefulService) setChecked(listID, itemID uuid.UUID, checked bool) (lists.Item, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	it, ok := s.items[itemID]
	if !ok || it.ListID != listID {
		return lists.Item{}, lists.ErrNotFound
	}
	it.Checked = checked
	if checked {
		now := time.Now()
		it.CheckedAt = &now
	} else {
		it.CheckedAt = nil
	}
	it.UpdatedAt = time.Now()
	return *it, nil
}

// Unused-by-these-tests methods satisfy the interface.
func (s *statefulService) CreateList(context.Context, string, uuid.UUID) (lists.List, error) {
	return lists.List{}, nil
}
func (s *statefulService) Lists(context.Context) ([]lists.List, error) { return nil, nil }
func (s *statefulService) GetList(context.Context, uuid.UUID) (lists.List, []lists.Item, error) {
	return lists.List{}, nil, nil
}

func (s *statefulService) RenameList(context.Context, uuid.UUID, string) (lists.List, error) {
	return lists.List{}, nil
}
func (s *statefulService) DeleteList(context.Context, uuid.UUID) error { return nil }
func (s *statefulService) CopyList(context.Context, uuid.UUID, string, uuid.UUID) (lists.List, error) {
	return lists.List{}, nil
}

func (s *statefulService) AddItem(context.Context, uuid.UUID, string, int, string, *string, bool, uuid.UUID) (lists.Item, error) {
	return lists.Item{}, nil
}
func (s *statefulService) DeleteItem(context.Context, uuid.UUID, uuid.UUID) error { return nil }
func (s *statefulService) RestoreItem(_ context.Context, listID, itemID uuid.UUID) (lists.Item, error) {
	it, ok := s.get(itemID)
	if !ok || it.ListID != listID {
		return lists.Item{}, lists.ErrNotFound
	}
	return it, nil
}

// TestSequentialUpdatesConvergeLWW pins US-S.3's field-edit rule: two sequential
// updates to the same field converge to the LAST write, and each update emits an
// items event (so every client eventually reloads the final value). This is the
// last-write-wins model of Key Decision 4 — no new conflict machinery.
func TestSequentialUpdatesConvergeLWW(t *testing.T) {
	listID, itemID := uuid.New(), uuid.New()
	svc := newStatefulService()
	svc.put(lists.Item{ID: itemID, ListID: listID, Name: "milk", Quantity: 1})

	pub := &capturingPublisher{}
	srv := newServerWithEvents(t, svc, pub)

	base := srv.URL + "/api/v1/lists/" + listID.String() + "/items/" + itemID.String()

	// Two racing clients settle as sequential writes at the server; the second
	// wins.
	r1 := patchJSON(t, base, `{"name":"oat milk"}`)
	r1.Body.Close()
	if r1.StatusCode != http.StatusOK {
		t.Fatalf("first update status = %d, want 200", r1.StatusCode)
	}
	r2 := patchJSON(t, base, `{"name":"soy milk"}`)
	r2.Body.Close()
	if r2.StatusCode != http.StatusOK {
		t.Fatalf("second update status = %d, want 200", r2.StatusCode)
	}

	final, ok := svc.get(itemID)
	if !ok {
		t.Fatal("item vanished")
	}
	if final.Name != "soy milk" {
		t.Errorf("converged name = %q, want last write %q", final.Name, "soy milk")
	}

	got := pub.captured()
	if len(got) != 2 {
		t.Fatalf("published %d events, want 2 (one per update): %+v", len(got), got)
	}
	for i, e := range got {
		if e.Type != events.TypeItems || e.ListID != listID.String() {
			t.Errorf("event %d = %+v, want items/%s", i, e, listID)
		}
	}
}

// TestCheckThenUncheckLeavesUnchecked pins US-S.3's absolute check/uncheck rule:
// a check followed by an uncheck leaves the item unchecked with BOTH operations
// applied (neither silently dropped), and each emits its own items event.
func TestCheckThenUncheckLeavesUnchecked(t *testing.T) {
	listID, itemID := uuid.New(), uuid.New()
	svc := newStatefulService()
	svc.put(lists.Item{ID: itemID, ListID: listID, Name: "milk", Quantity: 1})

	pub := &capturingPublisher{}
	srv := newServerWithEvents(t, svc, pub)

	base := srv.URL + "/api/v1/lists/" + listID.String() + "/items/" + itemID.String()

	rc := postJSON(t, base+"/check", "")
	rc.Body.Close()
	if rc.StatusCode != http.StatusOK {
		t.Fatalf("check status = %d, want 200", rc.StatusCode)
	}
	if it, _ := svc.get(itemID); !it.Checked {
		t.Fatal("item not checked after check")
	}

	ru := postJSON(t, base+"/uncheck", "")
	ru.Body.Close()
	if ru.StatusCode != http.StatusOK {
		t.Fatalf("uncheck status = %d, want 200", ru.StatusCode)
	}

	final, _ := svc.get(itemID)
	if final.Checked {
		t.Error("item is checked after check+uncheck, want unchecked")
	}
	if final.CheckedAt != nil {
		t.Errorf("checkedAt = %v after uncheck, want nil", final.CheckedAt)
	}

	got := pub.captured()
	if len(got) != 2 {
		t.Fatalf("published %d events, want 2 (check + uncheck, neither dropped): %+v", len(got), got)
	}
	for i, e := range got {
		if e.Type != events.TypeItems || e.ListID != listID.String() {
			t.Errorf("event %d = %+v, want items/%s", i, e, listID)
		}
	}
}
