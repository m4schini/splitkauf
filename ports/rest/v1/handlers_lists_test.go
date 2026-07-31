// SPDX-License-Identifier: TODO

package v1_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/m4schini/splitkauf/lists"
	"github.com/m4schini/splitkauf/ports/rest"
	v1 "github.com/m4schini/splitkauf/ports/rest/v1"
)

// fakeService is an in-memory ListService for hermetic full-stack handler
// tests. Each field, when set, overrides the corresponding method; unset
// methods return zero values. This lets a test wire only what it exercises.
type fakeService struct {
	createList  func(ctx context.Context, name string) (lists.List, error)
	listsFn     func(ctx context.Context) ([]lists.List, error)
	getList     func(ctx context.Context, id uuid.UUID) (lists.List, []lists.Item, error)
	renameList  func(ctx context.Context, id uuid.UUID, name string) (lists.List, error)
	deleteList  func(ctx context.Context, id uuid.UUID) error
	addItem     func(ctx context.Context, listID uuid.UUID, name string, quantity int, note *string) (lists.Item, error)
	updateItem  func(ctx context.Context, listID, itemID uuid.UUID, update lists.ItemUpdate) (lists.Item, error)
	deleteItem  func(ctx context.Context, listID, itemID uuid.UUID) error
	checkItem   func(ctx context.Context, listID, itemID uuid.UUID) (lists.Item, error)
	uncheckItem func(ctx context.Context, listID, itemID uuid.UUID) (lists.Item, error)
}

func (f *fakeService) CreateList(ctx context.Context, name string) (lists.List, error) {
	return f.createList(ctx, name)
}

func (f *fakeService) Lists(ctx context.Context) ([]lists.List, error) {
	return f.listsFn(ctx)
}

func (f *fakeService) GetList(ctx context.Context, id uuid.UUID) (lists.List, []lists.Item, error) {
	return f.getList(ctx, id)
}

func (f *fakeService) RenameList(ctx context.Context, id uuid.UUID, name string) (lists.List, error) {
	return f.renameList(ctx, id, name)
}

func (f *fakeService) DeleteList(ctx context.Context, id uuid.UUID) error {
	return f.deleteList(ctx, id)
}

func (f *fakeService) AddItem(ctx context.Context, listID uuid.UUID, name string, quantity int, note *string) (lists.Item, error) {
	return f.addItem(ctx, listID, name, quantity, note)
}

func (f *fakeService) UpdateItem(ctx context.Context, listID, itemID uuid.UUID, update lists.ItemUpdate) (lists.Item, error) {
	return f.updateItem(ctx, listID, itemID, update)
}

func (f *fakeService) DeleteItem(ctx context.Context, listID, itemID uuid.UUID) error {
	return f.deleteItem(ctx, listID, itemID)
}

func (f *fakeService) CheckItem(ctx context.Context, listID, itemID uuid.UUID) (lists.Item, error) {
	return f.checkItem(ctx, listID, itemID)
}

func (f *fakeService) UncheckItem(ctx context.Context, listID, itemID uuid.UUID) (lists.Item, error) {
	return f.uncheckItem(ctx, listID, itemID)
}

// newServer starts a full-stack REST server (via rest.New) backed by the given
// fake service, exercising the real router, validator, and DevAuth middleware.
func newServer(t *testing.T, svc v1.ListService) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(rest.New(&v1.V1{Service: svc}))
	t.Cleanup(srv.Close)
	return srv
}

func TestGetMe(t *testing.T) {
	srv := newServer(t, &fakeService{})

	resp, err := http.Get(srv.URL + "/api/v1/me")
	if err != nil {
		t.Fatalf("GET /me: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var got v1.User
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Name != "Dev User" {
		t.Errorf("name = %q, want %q", got.Name, "Dev User")
	}
	if got.Id == (uuid.UUID{}) {
		t.Error("dev user id is zero")
	}
}

func TestCreateListHappyPath(t *testing.T) {
	want := lists.List{ID: uuid.New(), Name: "Groceries", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	svc := &fakeService{createList: func(_ context.Context, name string) (lists.List, error) {
		if name != "Groceries" {
			t.Errorf("service got name %q", name)
		}
		return want, nil
	}}
	srv := newServer(t, svc)

	resp := postJSON(t, srv.URL+"/api/v1/lists", `{"name":"Groceries"}`)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201", resp.StatusCode)
	}
	var got v1.List
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Name != "Groceries" || got.Id != want.ID {
		t.Errorf("got %+v, want name Groceries id %v", got, want.ID)
	}
}

// TestCreateListValidationSurface is the key M1 acceptance criterion: an empty
// body is rejected by the OpenAPI request-validation middleware before the
// handler runs, yielding a 400 application/problem+json validation problem.
func TestCreateListValidationSurface(t *testing.T) {
	// The service must never be called for an invalid request.
	svc := &fakeService{createList: func(_ context.Context, _ string) (lists.List, error) {
		t.Fatal("service should not be called for invalid body")
		return lists.List{}, nil
	}}
	srv := newServer(t, svc)

	resp := postJSON(t, srv.URL+"/api/v1/lists", `{}`)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/problem+json" {
		t.Errorf("content-type = %q, want application/problem+json", ct)
	}
	var prob struct {
		Type   string `json:"type"`
		Status int    `json:"status"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&prob); err != nil {
		t.Fatalf("decode problem: %v", err)
	}
	if prob.Type != "/problems/validation" {
		t.Errorf("type = %q, want /problems/validation", prob.Type)
	}
	if prob.Status != http.StatusBadRequest {
		t.Errorf("problem status = %d, want 400", prob.Status)
	}
}

func TestListLists(t *testing.T) {
	svc := &fakeService{listsFn: func(_ context.Context) ([]lists.List, error) {
		return []lists.List{
			{ID: uuid.New(), Name: "A", OpenItemCount: 2, CheckedItemCount: 1},
			{ID: uuid.New(), Name: "B"},
		}, nil
	}}
	srv := newServer(t, svc)

	resp, err := http.Get(srv.URL + "/api/v1/lists")
	if err != nil {
		t.Fatalf("GET /lists: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var got []v1.List
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 2 || got[0].OpenItemCount != 2 || got[0].CheckedItemCount != 1 {
		t.Errorf("got %+v", got)
	}
}

func TestGetListNotFound(t *testing.T) {
	svc := &fakeService{getList: func(_ context.Context, _ uuid.UUID) (lists.List, []lists.Item, error) {
		return lists.List{}, nil, lists.ErrNotFound
	}}
	srv := newServer(t, svc)

	resp, err := http.Get(srv.URL + "/api/v1/lists/" + uuid.New().String())
	if err != nil {
		t.Fatalf("GET /lists/{id}: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/problem+json" {
		t.Errorf("content-type = %q, want application/problem+json", ct)
	}
	var prob struct {
		Type string `json:"type"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&prob); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if prob.Type != "/problems/not-found" {
		t.Errorf("type = %q, want /problems/not-found", prob.Type)
	}
}

func TestGetListHappyPath(t *testing.T) {
	listID := uuid.New()
	itemID := uuid.New()
	svc := &fakeService{getList: func(_ context.Context, id uuid.UUID) (lists.List, []lists.Item, error) {
		if id != listID {
			t.Errorf("service got id %v, want %v", id, listID)
		}
		return lists.List{ID: listID, Name: "Groceries", OpenItemCount: 1},
			[]lists.Item{{ID: itemID, ListID: listID, Name: "milk", Quantity: 1}}, nil
	}}
	srv := newServer(t, svc)

	resp, err := http.Get(srv.URL + "/api/v1/lists/" + listID.String())
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var got v1.ListWithItems
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Id != listID || len(got.Items) != 1 || got.Items[0].Name != "milk" {
		t.Errorf("got %+v", got)
	}
}

func TestAddItemHappyPath(t *testing.T) {
	listID := uuid.New()
	svc := &fakeService{addItem: func(_ context.Context, lid uuid.UUID, name string, qty int, note *string) (lists.Item, error) {
		if lid != listID || name != "milk" || qty != 2 {
			t.Errorf("service got lid=%v name=%q qty=%d", lid, name, qty)
		}
		return lists.Item{ID: uuid.New(), ListID: listID, Name: name, Quantity: qty, Note: note}, nil
	}}
	srv := newServer(t, svc)

	resp := postJSON(t, srv.URL+"/api/v1/lists/"+listID.String()+"/items", `{"name":"milk","quantity":2}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201", resp.StatusCode)
	}
	var got v1.Item
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Name != "milk" || got.Quantity != 2 {
		t.Errorf("got %+v", got)
	}
}

func TestCheckItemHappyPath(t *testing.T) {
	listID, itemID := uuid.New(), uuid.New()
	now := time.Now()
	svc := &fakeService{checkItem: func(_ context.Context, lid, iid uuid.UUID) (lists.Item, error) {
		if lid != listID || iid != itemID {
			t.Errorf("service got lid=%v iid=%v", lid, iid)
		}
		return lists.Item{ID: itemID, ListID: listID, Name: "milk", Quantity: 1, Checked: true, CheckedAt: &now}, nil
	}}
	srv := newServer(t, svc)

	resp := postJSON(t, srv.URL+"/api/v1/lists/"+listID.String()+"/items/"+itemID.String()+"/check", "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var got v1.Item
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !got.Checked || got.CheckedAt == nil {
		t.Errorf("got %+v, want checked", got)
	}
}

func TestDeleteListNoContent(t *testing.T) {
	svc := &fakeService{deleteList: func(_ context.Context, _ uuid.UUID) error { return nil }}
	srv := newServer(t, svc)

	req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/api/v1/lists/"+uuid.New().String(), nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", resp.StatusCode)
	}
}

// postJSON POSTs a JSON body and returns the response; the caller closes it.
func postJSON(t *testing.T, url, body string) *http.Response {
	t.Helper()
	resp, err := http.Post(url, "application/json", bytes.NewBufferString(body)) //nolint:gosec // url is test-controlled (httptest server)
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	return resp
}
